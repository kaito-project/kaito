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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

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

// SetCacheMutations returns a pod spec modifier that injects cache provider
// env vars, volumes, volume mounts, and init containers into the inference pod.
// Returns nil (no modifier) when cache is not configured. Callers are expected
// to gate on the distributed-cache feature flag before wiring this modifier in.
func SetCacheMutations() generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, corev1.PodSpec] {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		ws := ctx.Workspace
		if !HasAttachment(ws) {
			return nil
		}
		attachment, err := ResolveEffectiveAttachment(ctx.Ctx, ctx.KubeClient, ws)
		if err != nil {
			return fmt.Errorf("resolving cache attachment: %w", err)
		}

		// Extract model name and revision from the model's metadata.
		var modelName, modelRevision string
		if ctx.Model != nil {
			params := ctx.Model.GetInferenceParameters()
			if params != nil {
				modelName = params.Name
				if params.Version != "" {
					_, revision, err := utils.ParseHuggingFaceModelVersion(params.Version)
					if err == nil {
						modelRevision = revision
					}
				}
				// If model name from params is empty, try preset name.
				if modelName == "" && ws.Inference != nil && ws.Inference.Preset != nil {
					modelName = string(ws.Inference.Preset.Name)
				}
			}
		}

		mutations, err := collectMutations(ctx.Ctx, ws, attachment, modelName, modelRevision, workloadFromPodSpec(spec))
		if err != nil {
			return fmt.Errorf("collecting cache mutations: %w", err)
		}

		mountAttachmentParameters(ctx.Ctx, ctx.KubeClient, ws, attachment, mutations)

		klog.InfoS("Cache mutations collected",
			"workspace", ws.Name,
			"labels", mutations.Labels,
			"envVars", len(mutations.EnvVars),
			"volumes", len(mutations.Volumes),
			"volumeMounts", len(mutations.VolumeMounts),
			"initContainers", len(mutations.InitContainers))
		for _, env := range mutations.EnvVars {
			klog.V(4).InfoS("  cache env var", "name", env.Name, "value", env.Value)
		}

		applyMutations(spec, mutations)
		return nil
	}
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
	dst.InitContainers = append(dst.InitContainers, src.InitContainers...)
}

// applyMutations injects the collected mutations into the pod spec.
// It deduplicates by name to avoid invalid PodSpecs with duplicate entries.
func applyMutations(spec *corev1.PodSpec, mutations *PodMutations) {
	if mutations == nil || len(spec.Containers) == 0 {
		return
	}

	container := &spec.Containers[0]

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

	// Deduplicate init containers by name (first wins).
	existingInits := make(map[string]struct{}, len(spec.InitContainers))
	for _, c := range spec.InitContainers {
		existingInits[c.Name] = struct{}{}
	}
	for _, c := range mutations.InitContainers {
		if _, exists := existingInits[c.Name]; !exists {
			spec.InitContainers = append(spec.InitContainers, c)
			existingInits[c.Name] = struct{}{}
		}
	}
}

// mountAttachmentParameters merges attachment parameters with provider defaults,
// creates runtime ConfigMaps, and mounts them for providers that consume files.
func mountAttachmentParameters(ctx context.Context, kubeClient client.Client, ws *kaitov1beta1.Workspace, attachment *Attachment, mutations *PodMutations) {
	if kubeClient == nil {
		return
	}

	if config := attachment.ModelWeights; config != nil && config.Mode != ModeDisabled {
		runtimeCM := buildRuntimeConfigMap(ctx, kubeClient, ws,
			config.Parameters, config.Provider, "model")
		if runtimeCM != "" {
			volName := "cache-model-config"
			mutations.Volumes = append(mutations.Volumes, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: runtimeCM},
					},
				},
			})
			mutations.VolumeMounts = append(mutations.VolumeMounts, corev1.VolumeMount{
				Name:      volName,
				MountPath: "/etc/kaito/cache/model",
				ReadOnly:  true,
			})
		}
	}

	if config := attachment.KVCache; config != nil && config.Mode != ModeDisabled {
		runtimeCM := buildRuntimeConfigMap(ctx, kubeClient, ws,
			config.Parameters, config.Provider, "kv")
		if runtimeCM != "" {
			volName := "cache-kv-config"
			mutations.Volumes = append(mutations.Volumes, corev1.Volume{
				Name: volName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: runtimeCM},
					},
				},
			})
			mutations.VolumeMounts = append(mutations.VolumeMounts, corev1.VolumeMount{
				Name:      volName,
				MountPath: "/etc/kaito/cache/kv",
				ReadOnly:  true,
			})
		}
	}
}

// buildRuntimeConfigMap merges provider Helm defaults with attachment parameters,
// creates/updates a runtime ConfigMap named "cache-<concern>-<workspace>", and returns
// the runtime ConfigMap name (or empty string on failure).
func buildRuntimeConfigMap(ctx context.Context, kubeClient client.Client,
	ws *kaitov1beta1.Workspace, parameters map[string]string, providerName ProviderName, concern string) string {

	// Start with provider Helm defaults (from registered provider config).
	merged := make(map[string]string)
	p, err := Get(providerName)
	if err == nil {
		if dc, ok := p.(DefaultConfigProvider); ok {
			for k, v := range dc.DefaultConfig(concern) {
				merged[k] = v
			}
		}
	}

	for k, v := range parameters {
		merged[k] = v
	}

	if len(merged) == 0 {
		return ""
	}

	// Create/update the runtime ConfigMap.
	runtimeName := fmt.Sprintf("cache-%s-%s", concern, ws.Name)
	runtimeCM := &corev1.ConfigMap{}
	runtimeCM.Name = runtimeName
	runtimeCM.Namespace = ws.Namespace
	runtimeCM.Labels = map[string]string{
		"kaito.sh/cache-config": "true",
		"kaito.sh/workspace":    ws.Name,
	}
	runtimeCM.Data = merged

	existing := &corev1.ConfigMap{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: ws.Namespace, Name: runtimeName}, existing); err != nil {
		// Create
		if err := kubeClient.Create(ctx, runtimeCM); err != nil {
			klog.V(2).InfoS("Failed to create runtime cache ConfigMap", "name", runtimeName, "error", err)
			return ""
		}
	} else {
		// Update only if data changed.
		if !mapsEqual(existing.Data, merged) {
			existing.Data = merged
			if err := kubeClient.Update(ctx, existing); err != nil {
				klog.V(2).InfoS("Failed to update runtime cache ConfigMap", "name", runtimeName, "error", err)
				return ""
			}
		}
	}

	return runtimeName
}

// SetCachePodTemplateLabels returns a StatefulSet modifier that adds cache-related
// labels to the pod template metadata. This enables webhook-based injection where
// the cache provider's mutating webhook detects the label and injects necessary
// volumes, mounts, and environment variables.
func SetCachePodTemplateLabels() generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, appsv1.StatefulSet] {
	return func(ctx *generator.WorkspaceGeneratorContext, ss *appsv1.StatefulSet) error {
		ws := ctx.Workspace
		if !HasAttachment(ws) {
			return nil
		}
		attachment, err := ResolveEffectiveAttachment(ctx.Ctx, ctx.KubeClient, ws)
		if err != nil {
			return fmt.Errorf("resolving cache attachment: %w", err)
		}

		var modelName, modelRevision string
		if ctx.Model != nil {
			params := ctx.Model.GetInferenceParameters()
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

		mutations, err := collectMutations(ctx.Ctx, ws, attachment, modelName, modelRevision, ss)
		if err != nil {
			return fmt.Errorf("collecting cache label mutations: %w", err)
		}

		if mutations != nil && len(mutations.Labels) > 0 {
			if ss.Spec.Template.Labels == nil {
				ss.Spec.Template.Labels = make(map[string]string)
			}
			for k, v := range mutations.Labels {
				ss.Spec.Template.Labels[k] = v
			}
			klog.InfoS("Cache pod template labels applied",
				"workspace", ws.Name,
				"labels", mutations.Labels)
		}

		return nil
	}
}

// ApplyTemplateCacheMutations applies cache mutations (labels, env vars, volumes)
// to a StatefulSet generated from a custom pod template. This is the equivalent
// of SetCacheMutations + SetCachePodTemplateLabels for the template inference path.
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

	mountAttachmentParameters(ctx, kubeClient, ws, attachment, mutations)

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

// workloadFromPodSpec wraps an in-progress PodSpec in a minimal StatefulSet so
// providers can evaluate applicability against the actual workload (containers,
// args, volumes) from the PodSpec-level mutation path, which does not otherwise
// have the enclosing StatefulSet available.
func workloadFromPodSpec(spec *corev1.PodSpec) *appsv1.StatefulSet {
	if spec == nil {
		return nil
	}
	return &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{Spec: *spec},
		},
	}
}

// mapsEqual returns true if two string maps have identical keys and values.
func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
