// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
)

const (
	defaultCacheContainerImages  = "kaito-base"
	cacheContainerImageRegexEnv  = "CACHE_CONTAINER_IMAGE_REGEX"
	MutationHashAnnotation       = "kaito.sh/cache-mutation-hash"
	cacheModelNameAnnotation     = "kaito.sh/cache-model-name"
	cacheModelRevisionAnnotation = "kaito.sh/cache-model-revision"
)

type mutationHashInput struct {
	Attachment *Attachment   `json:"attachment"`
	Mutations  *PodMutations `json:"mutations"`
}

func setMutationHash(ss *appsv1.StatefulSet, attachment *Attachment, mutations *PodMutations) error {
	data, err := json.Marshal(mutationHashInput{
		Attachment: attachment,
		Mutations:  mutations,
	})
	if err != nil {
		return fmt.Errorf("marshalling cache mutation hash input: %w", err)
	}

	sum := sha256.Sum256(data)
	if ss.Spec.Template.Annotations == nil {
		ss.Spec.Template.Annotations = make(map[string]string)
	}
	ss.Spec.Template.Annotations[MutationHashAnnotation] = hex.EncodeToString(sum[:])
	return nil
}

// MutationHashChanged reports whether the desired cache configuration differs
// from the configuration rendered on the existing workload.
func MutationHashChanged(existing, desired *appsv1.StatefulSet) bool {
	return existing.Spec.Template.Annotations[MutationHashAnnotation] !=
		desired.Spec.Template.Annotations[MutationHashAnnotation]
}

func setCacheModelIdentity(ss *appsv1.StatefulSet, modelName, modelRevision string) {
	if ss.Spec.Template.Annotations == nil {
		ss.Spec.Template.Annotations = make(map[string]string)
	}

	if modelName != "" {
		ss.Spec.Template.Annotations[cacheModelNameAnnotation] = modelName
	} else {
		delete(ss.Spec.Template.Annotations, cacheModelNameAnnotation)
	}

	if modelRevision != "" {
		ss.Spec.Template.Annotations[cacheModelRevisionAnnotation] = modelRevision
	} else {
		delete(ss.Spec.Template.Annotations, cacheModelRevisionAnnotation)
	}
}

func isCacheContainer(image string) bool {
	if pattern := os.Getenv(cacheContainerImageRegexEnv); pattern != "" {
		matched, err := regexp.MatchString(pattern, image)
		if err != nil {
			klog.ErrorS(err, "Invalid cache container image regex", "pattern", pattern)
			return false
		}
		return matched
	}

	for _, value := range strings.Split(defaultCacheContainerImages, ",") {
		if strings.Contains(image, strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

// providerAppliesTo consults a provider's optional PodApplicabilityChecker to
// decide whether its mutations should be injected into the given workload.
// Providers that don't implement the interface — or calls made without a
// workload to evaluate (ss == nil) — are treated as applicable.
func providerAppliesTo(p Provider, concern CacheConcern, ws *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) bool {
	checker, ok := p.(PodApplicabilityChecker)
	if !ok || ss == nil {
		return true
	}
	return checker.AppliesTo(concern, ws, ss)
}

// ApplyPresetCacheMutations evaluates provider applicability against a rendered
// preset workload and applies all cache mutations. Callers must invoke this only
// when the distributed-cache feature gate is enabled.
func ApplyPresetCacheMutations(ctx context.Context, ws *kaitov1beta1.Workspace, kubeClient client.Client,
	model pkgmodel.Model, ss *appsv1.StatefulSet) error {
	if !HasAttachment(ws) {
		return nil
	}
	attachment, err := ResolveEffectiveAttachment(ctx, kubeClient, ws)
	if err != nil {
		return fmt.Errorf("resolving cache attachment: %w", err)
	}
	if attachment == nil {
		return nil
	}

	var modelName, modelRevision string
	if model != nil {
		params := model.GetInferenceParameters()
		if params != nil {
			modelName = params.Name
			if params.Version != "" {
				_, revision, err := utils.ParseHuggingFaceModelVersion(params.Version)
				if err == nil {
					modelRevision = revision
				}
			}
			if modelName == "" && ws.Inference != nil && ws.Inference.Preset != nil {
				modelName = string(ws.Inference.Preset.Name)
			}
		}
	}

	mutations, err := collectMutations(ctx, ws, attachment, modelName, modelRevision, ss)
	if err != nil {
		return fmt.Errorf("collecting cache mutations: %w", err)
	}

	setCacheModelIdentity(ss, modelName, modelRevision)

	klog.InfoS("Cache mutations collected",
		"workspace", ws.Name,
		"labels", mutations.Labels,
		"envVars", len(mutations.EnvVars),
		"volumes", len(mutations.Volumes),
		"volumeMounts", len(mutations.VolumeMounts))
	for _, env := range mutations.EnvVars {
		klog.V(4).InfoS("  cache env var", "name", env.Name, "value", env.Value)
	}

	if len(mutations.Labels) > 0 {
		ss.Spec.Template.Labels = maps.Clone(ss.Spec.Template.Labels)
		if ss.Spec.Template.Labels == nil {
			ss.Spec.Template.Labels = make(map[string]string, len(mutations.Labels))
		}
		for k, v := range mutations.Labels {
			ss.Spec.Template.Labels[k] = v
		}
	}

	applyMutations(&ss.Spec.Template.Spec, mutations)
	return setMutationHash(ss, attachment, mutations)
}

// collectMutations gathers PodMutations from all configured cache providers,
// calling each provider once per concern it handles. ss is the workload being
// mutated; it is passed to each provider's optional applicability check so a
// provider can decline to engage with a workload it does not support.
func collectMutations(ctx context.Context, ws *kaitov1beta1.Workspace, attachment *Attachment, modelName, modelRevision string, ss *appsv1.StatefulSet) (*PodMutations, error) {
	merged := &PodMutations{}
	if attachment == nil {
		return merged, nil
	}

	if config := attachment.ModelWeights; config != nil && config.Mode != ModeDisabled {
		mutations, err := collectConcernMutations(ctx, ws, config, CacheConcernModelWeights,
			modelName, modelRevision, ss, true)
		if err != nil {
			return nil, err
		}
		mergeMutations(merged, mutations)
	}

	if config := attachment.KVCache; config != nil && config.Mode != ModeDisabled {
		mutations, err := collectConcernMutations(ctx, ws, config, CacheConcernKVCache,
			modelName, modelRevision, ss, false)
		if err != nil {
			return nil, err
		}
		mergeMutations(merged, mutations)
	}

	return merged, nil
}

func collectConcernMutations(ctx context.Context, ws *kaitov1beta1.Workspace, config *ConcernConfig,
	concern CacheConcern, modelName, modelRevision string, ss *appsv1.StatefulSet, checkApplicability bool) (*PodMutations, error) {
	label := "KV cache"
	if concern == CacheConcernModelWeights {
		label = "model weights cache"
	}

	provider, err := Get(config.Provider)
	if err != nil {
		if config.Mode == ModeRequired {
			return nil, fmt.Errorf("%s provider %q: %w", label, config.Provider, err)
		}
		klog.V(2).InfoS("Cache provider not registered, skipping",
			"concern", concern, "provider", config.Provider, "error", err)
		return nil, nil
	}
	if checkApplicability && !providerAppliesTo(provider, concern, ws, ss) {
		klog.V(2).InfoS("Cache provider not applicable to this workload, skipping",
			"concern", concern, "provider", config.Provider, "workspace", ws.Name)
		return nil, nil
	}
	if err := validateParameters(provider, concern, config.Parameters); err != nil {
		if config.Mode == ModeRequired {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		klog.V(2).InfoS("Cache parameters invalid, skipping",
			"concern", concern, "provider", config.Provider, "error", err)
		return nil, nil
	}

	cacheName := config.Parameters["cacheName"]
	available, availabilityErr := provider.IsAvailable(ctx, cacheName)
	if availabilityErr != nil || !available {
		if config.Mode == ModeRequired {
			return nil, fmt.Errorf("%s provider %q not available", label, config.Provider)
		}
		klog.V(2).InfoS("Cache provider not available, skipping mutations",
			"concern", concern, "provider", config.Provider,
			"available", available, "error", availabilityErr)
		return nil, nil
	}

	mutations, err := provider.PodMutations(ctx, concern, ws, modelName, modelRevision, config.Parameters)
	if err != nil {
		if config.Mode == ModeRequired {
			return nil, fmt.Errorf("%s mutations: %w", label, err)
		}
		klog.V(2).InfoS("Cache mutations failed, skipping",
			"concern", concern, "provider", config.Provider, "error", err)
		return nil, nil
	}
	return mutations, nil
}

// mergeMutations appends src mutations into dst, deduplicating env vars by name
// and merging labels.
func mergeMutations(dst, src *PodMutations) {
	if src == nil {
		return
	}

	// Merge labels.
	if len(src.Labels) > 0 {
		if dst.Labels == nil {
			dst.Labels = make(map[string]string, len(src.Labels))
		}
		for k, v := range src.Labels {
			dst.Labels[k] = v
		}
	}

	// Deduplicate env vars by name: first wins (dst takes priority). Env vars
	// already present in dst are not overwritten or merged.
	existingEnvs := make(map[string]int, len(dst.EnvVars))
	for i, e := range dst.EnvVars {
		existingEnvs[e.Name] = i
	}
	for _, e := range src.EnvVars {
		if _, exists := existingEnvs[e.Name]; !exists {
			dst.EnvVars = append(dst.EnvVars, e)
			existingEnvs[e.Name] = len(dst.EnvVars) - 1
		}
	}

	dst.Volumes = append(dst.Volumes, src.Volumes...)
	dst.VolumeMounts = append(dst.VolumeMounts, src.VolumeMounts...)
}

// applyMutations injects the collected mutations into the pod spec.
// It deduplicates by name to avoid invalid PodSpecs with duplicate entries.
func applyMutations(spec *corev1.PodSpec, mutations *PodMutations) {
	if mutations == nil || len(spec.Containers) == 0 {
		return
	}

	matched := false
	for i := range spec.Containers {
		container := &spec.Containers[i]
		if !isCacheContainer(container.Image) {
			continue
		}
		matched = true

		// Deduplicate env vars. First wins — existing user values take priority.
		existingEnvs := make(map[string]int, len(container.Env))
		for i, e := range container.Env {
			existingEnvs[e.Name] = i
		}
		for _, e := range mutations.EnvVars {
			if _, exists := existingEnvs[e.Name]; !exists {
				container.Env = append(container.Env, e)
				existingEnvs[e.Name] = len(container.Env) - 1
			}
		}

		// Deduplicate volume mounts by name (first wins).
		existingMounts := make(map[string]struct{}, len(container.VolumeMounts))
		for _, m := range container.VolumeMounts {
			existingMounts[m.Name] = struct{}{}
		}
		for _, m := range mutations.VolumeMounts {
			if _, exists := existingMounts[m.Name]; !exists {
				container.VolumeMounts = append(container.VolumeMounts, m)
				existingMounts[m.Name] = struct{}{}
			}
		}
	}

	if !matched {
		return
	}

	// Deduplicate volumes by name (first wins).
	existingVols := make(map[string]struct{}, len(spec.Volumes))
	for _, v := range spec.Volumes {
		existingVols[v.Name] = struct{}{}
	}
	for _, v := range mutations.Volumes {
		if _, exists := existingVols[v.Name]; !exists {
			spec.Volumes = append(spec.Volumes, v)
			existingVols[v.Name] = struct{}{}
		}
	}

}

// ApplyTemplateCacheMutations applies cache mutations (labels, env vars, volumes)
// to a StatefulSet generated from a custom pod template. This is the equivalent
// of SetCacheMutations for the template inference path.
// Returns an error when cache mode is Required and mutations cannot be collected.
//
// Whether a configured provider actually engages with this workload is decided by
// the provider itself (via the optional PodApplicabilityChecker), so a provider
// that cannot serve a given pod template is skipped without a framework-level gate.
func ApplyTemplateCacheMutations(ctx context.Context, ws *kaitov1beta1.Workspace, kubeClient client.Client, ss *appsv1.StatefulSet) error {
	if !HasAttachment(ws) {
		return nil
	}
	attachment, err := ResolveEffectiveAttachment(ctx, kubeClient, ws)
	if err != nil {
		return fmt.Errorf("resolving cache attachment: %w", err)
	}
	if attachment == nil {
		return nil
	}

	mutations, err := collectMutations(ctx, ws, attachment, "", "", ss)
	if err != nil {
		klog.V(2).InfoS("Failed to collect cache mutations for template inference", "error", err)
		if attachmentRequires(attachment) {
			return fmt.Errorf("cache mutations failed in Required mode: %w", err)
		}
		return nil
	}
	if mutations == nil {
		return nil
	}

	klog.InfoS("Cache mutations applied to template inference",
		"workspace", ws.Name,
		"labels", mutations.Labels,
		"envVars", len(mutations.EnvVars),
		"volumes", len(mutations.Volumes),
		"volumeMounts", len(mutations.VolumeMounts))
	for _, env := range mutations.EnvVars {
		klog.InfoS("  cache mutation env", "workspace", ws.Name, "name", env.Name, "value", env.Value)
	}

	// Apply labels to pod template for webhook-based injection.
	if len(mutations.Labels) > 0 {
		if ss.Spec.Template.Labels == nil {
			ss.Spec.Template.Labels = make(map[string]string)
		}
		for k, v := range mutations.Labels {
			ss.Spec.Template.Labels[k] = v
		}
	}

	// Apply env vars, volumes, and mounts to the pod spec.
	applyMutations(&ss.Spec.Template.Spec, mutations)
	return nil
}

func attachmentRequires(attachment *Attachment) bool {
	return attachment != nil &&
		((attachment.ModelWeights != nil && attachment.ModelWeights.Mode == ModeRequired) ||
			(attachment.KVCache != nil && attachment.KVCache.Mode == ModeRequired))
}
