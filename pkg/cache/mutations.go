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

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

// SetCacheMutations returns a pod spec modifier that injects cache provider
// env vars, volumes, volume mounts, and init containers into the inference pod.
// Returns nil (no modifier) when the feature gate is disabled or cache is not configured.
func SetCacheMutations() generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, corev1.PodSpec] {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		if !featuregates.FeatureGates[consts.FeatureFlagDistributedCache] {
			return nil
		}

		ws := ctx.Workspace
		if ws.Cache == nil {
			return nil
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

		mutations, err := collectMutations(ctx.Ctx, ws, modelName, modelRevision)
		if err != nil {
			return fmt.Errorf("collecting cache mutations: %w", err)
		}

		applyMutations(spec, mutations)
		return nil
	}
}

// collectMutations gathers PodMutations from all configured cache providers,
// calling each provider once per concern it handles.
func collectMutations(ctx context.Context, ws *kaitov1beta1.Workspace, modelName, modelRevision string) (*PodMutations, error) {
	merged := &PodMutations{}

	// Model weights provider
	if ws.Cache.ModelWeights != nil && ws.Cache.ModelWeights.Mode != kaitov1beta1.CacheModeDisabled {
		p, err := Get(ws.Cache.ModelWeights.Provider)
		if err != nil {
			if ws.Cache.ModelWeights.Mode == kaitov1beta1.CacheModeRequired {
				return nil, fmt.Errorf("model weights cache provider %q: %w", ws.Cache.ModelWeights.Provider, err)
			}
			klog.V(2).InfoS("Model weights cache provider not available, skipping",
				"provider", ws.Cache.ModelWeights.Provider, "error", err)
		} else {
			m, err := p.PodMutations(ctx, CacheConcernModelWeights, ws, modelName, modelRevision)
			if err != nil {
				if ws.Cache.ModelWeights.Mode == kaitov1beta1.CacheModeRequired {
					return nil, fmt.Errorf("model weights cache mutations: %w", err)
				}
				klog.V(2).InfoS("Model weights cache mutations failed, skipping",
					"provider", ws.Cache.ModelWeights.Provider, "error", err)
			} else {
				mergeMutations(merged, m)
			}
		}
	}

	// KV cache provider
	if ws.Cache.KVCache != nil && ws.Cache.KVCache.Mode != kaitov1beta1.CacheModeDisabled {
		p, err := Get(ws.Cache.KVCache.Provider)
		if err != nil {
			if ws.Cache.KVCache.Mode == kaitov1beta1.CacheModeRequired {
				return nil, fmt.Errorf("KV cache provider %q: %w", ws.Cache.KVCache.Provider, err)
			}
			klog.V(2).InfoS("KV cache provider not available, skipping",
				"provider", ws.Cache.KVCache.Provider, "error", err)
		} else {
			m, err := p.PodMutations(ctx, CacheConcernKVCache, ws, modelName, modelRevision)
			if err != nil {
				if ws.Cache.KVCache.Mode == kaitov1beta1.CacheModeRequired {
					return nil, fmt.Errorf("KV cache mutations: %w", err)
				}
				klog.V(2).InfoS("KV cache mutations failed, skipping",
					"provider", ws.Cache.KVCache.Provider, "error", err)
			} else {
				mergeMutations(merged, m)
			}
		}
	}

	return merged, nil
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

	// Deduplicate env vars by name (last wins).
	existingEnvs := make(map[string]struct{}, len(dst.EnvVars))
	for _, e := range dst.EnvVars {
		existingEnvs[e.Name] = struct{}{}
	}
	for _, e := range src.EnvVars {
		if _, exists := existingEnvs[e.Name]; !exists {
			dst.EnvVars = append(dst.EnvVars, e)
			existingEnvs[e.Name] = struct{}{}
		}
	}

	dst.Volumes = append(dst.Volumes, src.Volumes...)
	dst.VolumeMounts = append(dst.VolumeMounts, src.VolumeMounts...)
	dst.InitContainers = append(dst.InitContainers, src.InitContainers...)
}

// applyMutations injects the collected mutations into the pod spec.
func applyMutations(spec *corev1.PodSpec, mutations *PodMutations) {
	if mutations == nil || len(spec.Containers) == 0 {
		return
	}

	// Inject env vars and volume mounts into the first (model) container.
	spec.Containers[0].Env = append(spec.Containers[0].Env, mutations.EnvVars...)
	spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, mutations.VolumeMounts...)

	// Inject volumes and init containers at the pod level.
	spec.Volumes = append(spec.Volumes, mutations.Volumes...)
	spec.InitContainers = append(spec.InitContainers, mutations.InitContainers...)
}

// SetCachePodTemplateLabels returns a StatefulSet modifier that adds cache-related
// labels to the pod template metadata. This enables webhook-based injection where
// the cache provider's mutating webhook detects the label and injects necessary
// volumes, mounts, and environment variables.
func SetCachePodTemplateLabels() generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, appsv1.StatefulSet] {
	return func(ctx *generator.WorkspaceGeneratorContext, ss *appsv1.StatefulSet) error {
		if !featuregates.FeatureGates[consts.FeatureFlagDistributedCache] {
			return nil
		}

		ws := ctx.Workspace
		if ws.Cache == nil {
			return nil
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

		mutations, err := collectMutations(ctx.Ctx, ws, modelName, modelRevision)
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
		}

		return nil
	}
}

// ApplyTemplateCacheMutations applies cache mutations (labels, env vars, volumes)
// to a StatefulSet generated from a custom pod template. This is the equivalent
// of SetCacheMutations + SetCachePodTemplateLabels for the template inference path.
func ApplyTemplateCacheMutations(ctx context.Context, ws *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) {
	if ws.Cache == nil {
		return
	}

	mutations, err := collectMutations(ctx, ws, "", "")
	if err != nil {
		klog.V(2).InfoS("Failed to collect cache mutations for template inference", "error", err)
		return
	}
	if mutations == nil {
		return
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
}
