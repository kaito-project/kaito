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

		mutations, err := collectMutations(ctx.Ctx, ctx.KubeClient, ws, modelName, modelRevision)
		if err != nil {
			return fmt.Errorf("collecting cache mutations: %w", err)
		}

		// Mount user-provided Config ConfigMaps as volumes.
		mountCacheConfigMaps(ctx.Ctx, ctx.KubeClient, ws, mutations)

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
// calling each provider once per concern it handles.
func collectMutations(ctx context.Context, kubeClient client.Client, ws *kaitov1beta1.Workspace, modelName, modelRevision string) (*PodMutations, error) {
	merged := &PodMutations{}

	// Model weights provider
	if ws.Cache.ModelCache != nil && ws.Cache.ModelCache.Mode != kaitov1beta1.CacheModeDisabled {
		p, err := Get(ws.Cache.ModelCache.Provider)
		if err != nil {
			if ws.Cache.ModelCache.Mode == kaitov1beta1.CacheModeRequired {
				return nil, fmt.Errorf("model weights cache provider %q: %w", ws.Cache.ModelCache.Provider, err)
			}
			klog.V(2).InfoS("Model weights cache provider not registered, skipping",
				"provider", ws.Cache.ModelCache.Provider, "error", err)
		} else {
			cacheName := extractCacheName(ctx, kubeClient, ws.Namespace, ws.Cache.ModelCache.Config)
			// Check infrastructure availability before injecting mutations.
			available, availErr := p.IsAvailable(ctx, cacheName)
			if availErr != nil || !available {
				if ws.Cache.ModelCache.Mode == kaitov1beta1.CacheModeRequired {
					return nil, fmt.Errorf("model weights cache provider %q not available", ws.Cache.ModelCache.Provider)
				}
				klog.V(2).InfoS("Model weights cache provider not available, skipping mutations",
					"provider", ws.Cache.ModelCache.Provider, "available", available, "error", availErr)
			} else {
				m, err := p.PodMutations(ctx, CacheConcernModelWeights, ws, modelName, modelRevision, cacheName)
				if err != nil {
					if ws.Cache.ModelCache.Mode == kaitov1beta1.CacheModeRequired {
						return nil, fmt.Errorf("model weights cache mutations: %w", err)
					}
					klog.V(2).InfoS("Model weights cache mutations failed, skipping",
						"provider", ws.Cache.ModelCache.Provider, "error", err)
				} else {
					mergeMutations(merged, m)
				}
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
			klog.V(2).InfoS("KV cache provider not registered, skipping",
				"provider", ws.Cache.KVCache.Provider, "error", err)
		} else {
			cacheName := extractCacheName(ctx, kubeClient, ws.Namespace, ws.Cache.KVCache.Config)
			// Check infrastructure availability before injecting mutations.
			available, availErr := p.IsAvailable(ctx, cacheName)
			if availErr != nil || !available {
				if ws.Cache.KVCache.Mode == kaitov1beta1.CacheModeRequired {
					return nil, fmt.Errorf("KV cache provider %q not available", ws.Cache.KVCache.Provider)
				}
				klog.V(2).InfoS("KV cache provider not available, skipping mutations",
					"provider", ws.Cache.KVCache.Provider, "available", available, "error", availErr)
			} else {
				m, err := p.PodMutations(ctx, CacheConcernKVCache, ws, modelName, modelRevision, cacheName)
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

// mountCacheConfigMaps merges user-provided Config ConfigMaps with provider Helm
// defaults, creates a runtime ConfigMap with the merged result, and mounts it into
// the pod so providers can consume the full configuration.
func mountCacheConfigMaps(ctx context.Context, kubeClient client.Client, ws *kaitov1beta1.Workspace, mutations *PodMutations) {
	if kubeClient == nil {
		return
	}

	// Model cache config
	if ws.Cache.ModelCache != nil && ws.Cache.ModelCache.Config != "" {
		runtimeCM := buildRuntimeConfigMap(ctx, kubeClient, ws,
			ws.Cache.ModelCache.Config, ws.Cache.ModelCache.Provider, "model")
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

	// KV cache config
	if ws.Cache.KVCache != nil && ws.Cache.KVCache.Config != "" {
		runtimeCM := buildRuntimeConfigMap(ctx, kubeClient, ws,
			ws.Cache.KVCache.Config, ws.Cache.KVCache.Provider, "kv")
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

// buildRuntimeConfigMap merges provider Helm defaults with user-provided ConfigMap data,
// creates/updates a runtime ConfigMap named "cache-<concern>-<workspace>", and returns
// the runtime ConfigMap name (or empty string on failure).
func buildRuntimeConfigMap(ctx context.Context, kubeClient client.Client,
	ws *kaitov1beta1.Workspace, userCMName string, providerName kaitov1beta1.CacheProvider, concern string) string {

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

	// Overlay user-provided ConfigMap data (user overrides defaults).
	userCM := &corev1.ConfigMap{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: ws.Namespace, Name: userCMName}, userCM); err != nil {
		klog.V(2).InfoS("Failed to fetch user cache config ConfigMap, using defaults only",
			"configMap", userCMName, "namespace", ws.Namespace, "error", err)
	} else {
		for k, v := range userCM.Data {
			merged[k] = v
		}
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
		// Update
		existing.Data = merged
		if err := kubeClient.Update(ctx, existing); err != nil {
			klog.V(2).InfoS("Failed to update runtime cache ConfigMap", "name", runtimeName, "error", err)
			return ""
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

		mutations, err := collectMutations(ctx.Ctx, ctx.KubeClient, ws, modelName, modelRevision)
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
func ApplyTemplateCacheMutations(ctx context.Context, ws *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) {
	if ws.Cache == nil {
		return
	}

	mutations, err := collectMutations(ctx, nil, ws, "", "")
	if err != nil {
		klog.V(2).InfoS("Failed to collect cache mutations for template inference", "error", err)
		return
	}
	if mutations == nil {
		return
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
}

// extractCacheName reads the user-provided Config ConfigMap and returns the
// "cacheName" key value. Returns empty string if no ConfigMap is specified or
// the key is absent, meaning the provider should use its default cache.
func extractCacheName(ctx context.Context, kubeClient client.Client, namespace, configMapName string) string {
	if kubeClient == nil || configMapName == "" {
		return ""
	}
	cm := &corev1.ConfigMap{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: configMapName}, cm); err != nil {
		klog.V(4).InfoS("Could not read user cache ConfigMap for cacheName",
			"configMap", configMapName, "namespace", namespace, "error", err)
		return ""
	}
	return cm.Data["cacheName"]
}
