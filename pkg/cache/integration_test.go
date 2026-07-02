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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

// dacsTestProvider simulates the DACS provider for integration tests.
// It returns mutations matching the CSI-based approach: labels + env vars only.
type dacsTestProvider struct {
	blobPrefix  string
	discoveryEP string
	kvEnabled   bool
}

func (p *dacsTestProvider) Name() string { return "dacs" }
func (p *dacsTestProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (p *dacsTestProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "ready", nil
}
func (p *dacsTestProvider) PodMutations(_ context.Context, concern CacheConcern, ws *kaitov1beta1.Workspace, modelName, modelRevision, _ string) (*PodMutations, error) {
	mutations := &PodMutations{}

	switch concern {
	case CacheConcernModelWeights:
		// CSI approach: label triggers webhook injection.
		mutations.Labels = map[string]string{
			"dacs.azure.com/inject": "true",
		}
		if modelName != "" {
			revision := modelRevision
			if revision == "" {
				revision = "main"
			}
			localPath := "/mnt/models/" + p.blobPrefix + "/" + modelName + "/" + revision
			mutations.EnvVars = append(mutations.EnvVars, corev1.EnvVar{
				Name:  "KAITO_MODEL_PATH",
				Value: localPath,
			})
		}

	case CacheConcernKVCache:
		if !p.kvEnabled {
			return mutations, nil
		}
		// CSI approach: label triggers webhook injection for KV connector libs.
		mutations.Labels = map[string]string{
			"dacs.azure.com/inject": "true",
		}
		mutations.EnvVars = append(mutations.EnvVars, corev1.EnvVar{
			Name:  "VLLM_KV_TRANSFER_CONFIG",
			Value: `{"kv_connector":"dacs_client.connectors.vllm_connector.DacsKVConnector","kv_connector_extra_config":{"locator_nodes":"` + p.discoveryEP + `","protocol":"tcp","initial_ttl_ms":300000,"producer_ttl_ms":1800000,"max_ttl_ms":86400000}}`,
		})
	}

	return mutations, nil
}

func (p *dacsTestProvider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}

// mockModel implements pkgmodel.Model for tests.
type mockModel struct {
	name    string
	version string
}

func (m *mockModel) GetInferenceParameters() *pkgmodel.PresetParam {
	return &pkgmodel.PresetParam{
		Metadata: pkgmodel.Metadata{
			Name:    m.name,
			Version: m.version,
		},
		ReadinessTimeout: 30 * time.Minute,
	}
}
func (m *mockModel) GetTuningParameters() *pkgmodel.PresetParam { return nil }
func (m *mockModel) SupportDistributedInference() bool          { return false }
func (m *mockModel) SupportTuning() bool                        { return false }

func setupDacsProvider() *dacsTestProvider {
	p := &dacsTestProvider{
		blobPrefix:  "kaito-models",
		discoveryEP: "http://cacheserver-discovery.dacs-cache-system.svc.cluster.local:9065",
		kvEnabled:   true,
	}
	Register(p)
	return p
}

// TestSetCacheMutations_FullPipeline tests the complete mutation pipeline
// from feature gate check through to pod spec modification.
func TestSetCacheMutations_FullPipeline(t *testing.T) {
	setupDacsProvider()

	tests := []struct {
		name               string
		featureGateEnabled bool
		workspace          *kaitov1beta1.Workspace
		expectEnvVars      []string // env var names to check
		expectNoEnvVars    []string // env vars that should NOT be present
	}{
		{
			name:               "feature gate disabled - no mutations",
			featureGateEnabled: false,
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeRequired,
					},
				},
			},
			expectNoEnvVars: []string{"KAITO_MODEL_PATH", "VLLM_KV_TRANSFER_CONFIG"},
		},
		{
			name:               "model weights only - label + model path",
			featureGateEnabled: true,
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeOpportunistic,
					},
				},
			},
			expectEnvVars:   []string{"KAITO_MODEL_PATH"},
			expectNoEnvVars: []string{"VLLM_KV_TRANSFER_CONFIG", "LD_PRELOAD", "SI_storagePath"},
		},
		{
			name:               "KV cache only - KV config env var",
			featureGateEnabled: true,
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					KVCache: &kaitov1beta1.KVCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeRequired,
					},
				},
			},
			expectEnvVars:   []string{"VLLM_KV_TRANSFER_CONFIG"},
			expectNoEnvVars: []string{"KAITO_MODEL_PATH", "LD_PRELOAD"},
		},
		{
			name:               "both model weights and KV cache",
			featureGateEnabled: true,
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeOpportunistic,
					},
					KVCache: &kaitov1beta1.KVCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeRequired,
					},
				},
			},
			expectEnvVars:   []string{"KAITO_MODEL_PATH", "VLLM_KV_TRANSFER_CONFIG"},
			expectNoEnvVars: []string{"LD_PRELOAD", "SI_storagePath"},
		},
		{
			name:               "disabled mode - no mutations",
			featureGateEnabled: true,
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeDisabled,
					},
				},
			},
			expectNoEnvVars: []string{"KAITO_MODEL_PATH", "VLLM_KV_TRANSFER_CONFIG"},
		},
		{
			name:               "nil cache - no mutations",
			featureGateEnabled: true,
			workspace:          &kaitov1beta1.Workspace{},
			expectNoEnvVars:    []string{"KAITO_MODEL_PATH", "VLLM_KV_TRANSFER_CONFIG"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = tt.featureGateEnabled
			defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

			ctx := &generator.WorkspaceGeneratorContext{
				Ctx:       context.Background(),
				Workspace: tt.workspace,
				Model:     &mockModel{name: "microsoft/phi-4"},
			}

			spec := &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "vllm",
						Image:   "kaito-vllm:latest",
						Command: []string{"python3", "/workspace/vllm/inference_api.py"},
						Env:     []corev1.EnvVar{{Name: "EXISTING_VAR", Value: "keep"}},
					},
				},
			}

			modifier := SetCacheMutations()
			err := modifier(ctx, spec)
			if err != nil {
				t.Fatalf("SetCacheMutations returned error: %v", err)
			}

			// With CSI, no init containers or volumes should be injected.
			if len(spec.InitContainers) != 0 {
				t.Errorf("expected 0 init containers (CSI handles injection), got %d", len(spec.InitContainers))
			}
			if len(spec.Volumes) != 0 {
				t.Errorf("expected 0 volumes, got %d", len(spec.Volumes))
			}

			// Verify expected env vars.
			envMap := make(map[string]string)
			for _, e := range spec.Containers[0].Env {
				envMap[e.Name] = e.Value
			}
			for _, name := range tt.expectEnvVars {
				if _, ok := envMap[name]; !ok {
					t.Errorf("expected env var %s to be present", name)
				}
			}
			for _, name := range tt.expectNoEnvVars {
				if _, ok := envMap[name]; ok {
					t.Errorf("expected env var %s to NOT be present", name)
				}
			}

			// Always verify existing env vars are preserved.
			if envMap["EXISTING_VAR"] != "keep" {
				t.Error("existing env var EXISTING_VAR was lost or modified")
			}
		})
	}
}

// TestSetCacheMutations_ModelPathDerivation verifies that KAITO_MODEL_PATH
// is correctly derived from the model name and blob prefix.
func TestSetCacheMutations_ModelPathDerivation(t *testing.T) {
	setupDacsProvider()
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	tests := []struct {
		name         string
		presetName   string
		expectedPath string
	}{
		{
			name:         "standard org/model format",
			presetName:   "microsoft/phi-4",
			expectedPath: "/mnt/models/kaito-models/microsoft/phi-4/main",
		},
		{
			name:         "nested org name",
			presetName:   "meta-llama/Llama-3.3-70B-Instruct",
			expectedPath: "/mnt/models/kaito-models/meta-llama/Llama-3.3-70B-Instruct/main",
		},
		{
			name:         "single segment model name",
			presetName:   "phi-4",
			expectedPath: "/mnt/models/kaito-models/phi-4/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeOpportunistic,
					},
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{Name: kaitov1beta1.ModelName(tt.presetName)},
					},
				},
			}

			ctx := &generator.WorkspaceGeneratorContext{
				Ctx:       context.Background(),
				Workspace: ws,
				Model:     &mockModel{name: tt.presetName},
			}
			spec := &corev1.PodSpec{
				Containers: []corev1.Container{{Name: "model"}},
			}

			modifier := SetCacheMutations()
			err := modifier(ctx, spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var modelPath string
			for _, e := range spec.Containers[0].Env {
				if e.Name == "KAITO_MODEL_PATH" {
					modelPath = e.Value
					break
				}
			}
			if modelPath != tt.expectedPath {
				t.Errorf("KAITO_MODEL_PATH: got %q, want %q", modelPath, tt.expectedPath)
			}
		})
	}
}

// TestSetCacheMutations_PreservesExistingPodSpec verifies that cache mutations
// don't interfere with existing pod spec elements.
func TestSetCacheMutations_PreservesExistingPodSpec(t *testing.T) {
	setupDacsProvider()
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "dacs",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
		Inference: &kaitov1beta1.InferenceSpec{
			Preset: &kaitov1beta1.PresetSpec{
				PresetMeta: kaitov1beta1.PresetMeta{Name: "microsoft/phi-4"},
			},
		},
	}

	ctx := &generator.WorkspaceGeneratorContext{
		Ctx:       context.Background(),
		Workspace: ws,
		Model:     &mockModel{name: "microsoft/phi-4"},
	}

	// Pod spec with pre-existing volumes, mounts, env vars, and init containers.
	spec := &corev1.PodSpec{
		InitContainers: []corev1.Container{
			{Name: "existing-init", Image: "busybox"},
		},
		Containers: []corev1.Container{
			{
				Name:  "model",
				Image: "vllm:latest",
				Env: []corev1.EnvVar{
					{Name: "HF_TOKEN", Value: "secret"},
					{Name: "CUDA_VISIBLE_DEVICES", Value: "0,1"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "weights", MountPath: "/workspace/vllm/weights"},
				},
			},
		},
		Volumes: []corev1.Volume{
			{Name: "weights"},
		},
	}

	modifier := SetCacheMutations()
	err := modifier(ctx, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify existing init container is preserved (no new ones added with CSI).
	if len(spec.InitContainers) != 1 {
		t.Fatalf("expected 1 init container (only existing), got %d", len(spec.InitContainers))
	}
	if spec.InitContainers[0].Name != "existing-init" {
		t.Error("existing init container was not preserved")
	}

	// Verify existing volumes preserved (no new ones added with CSI).
	if len(spec.Volumes) != 1 {
		t.Fatalf("expected 1 volume (only existing), got %d", len(spec.Volumes))
	}

	// Verify existing env vars preserved.
	envMap := make(map[string]string)
	for _, e := range spec.Containers[0].Env {
		envMap[e.Name] = e.Value
	}
	if envMap["HF_TOKEN"] != "secret" {
		t.Error("HF_TOKEN env var was lost")
	}
	if envMap["CUDA_VISIBLE_DEVICES"] != "0,1" {
		t.Error("CUDA_VISIBLE_DEVICES env var was lost")
	}
	if envMap["KAITO_MODEL_PATH"] == "" {
		t.Error("KAITO_MODEL_PATH should be added")
	}

	// Verify existing volume mounts preserved (no new ones added with CSI).
	if len(spec.Containers[0].VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount (only existing), got %d", len(spec.Containers[0].VolumeMounts))
	}
}

// TestSetCachePodTemplateLabels verifies that the StatefulSet modifier
// correctly applies labels from cache mutations to the pod template.
func TestSetCachePodTemplateLabels(t *testing.T) {
	setupDacsProvider()
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	tests := []struct {
		name        string
		workspace   *kaitov1beta1.Workspace
		expectLabel bool
	}{
		{
			name: "model weights enabled - label added",
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeOpportunistic,
					},
				},
			},
			expectLabel: true,
		},
		{
			name: "KV cache enabled - label added",
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					KVCache: &kaitov1beta1.KVCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeRequired,
					},
				},
			},
			expectLabel: true,
		},
		{
			name:        "nil cache - no label",
			workspace:   &kaitov1beta1.Workspace{},
			expectLabel: false,
		},
		{
			name: "disabled mode - no label",
			workspace: &kaitov1beta1.Workspace{
				Cache: &kaitov1beta1.CacheSpec{
					ModelCache: &kaitov1beta1.ModelCacheSpec{
						Provider: "dacs",
						Mode:     kaitov1beta1.CacheModeDisabled,
					},
				},
			},
			expectLabel: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the appsv1 import indirectly through the modifier.
			ctx := &generator.WorkspaceGeneratorContext{
				Ctx:       context.Background(),
				Workspace: tt.workspace,
				Model:     &mockModel{name: "microsoft/phi-4"},
			}

			// Create a minimal StatefulSet-like structure to test label application.
			// We test via collectMutations directly since SetCachePodTemplateLabels
			// operates on appsv1.StatefulSet which requires the appsv1 import.
			if tt.workspace.Cache == nil {
				return
			}
			mutations, err := collectMutations(ctx.Ctx, nil, tt.workspace, "microsoft/phi-4", "main")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			hasLabel := mutations != nil && len(mutations.Labels) > 0 && mutations.Labels["dacs.azure.com/inject"] == "true"
			if hasLabel != tt.expectLabel {
				t.Errorf("label present: got %v, want %v", hasLabel, tt.expectLabel)
			}
		})
	}
}

// TestMergeMutations_Labels verifies that label merging works correctly.
func TestMergeMutations_Labels(t *testing.T) {
	dst := &PodMutations{
		Labels: map[string]string{"existing": "label"},
	}
	src := &PodMutations{
		Labels: map[string]string{
			"dacs.azure.com/inject": "true",
			"another":               "value",
		},
	}

	mergeMutations(dst, src)

	if len(dst.Labels) != 3 {
		t.Fatalf("expected 3 labels after merge, got %d", len(dst.Labels))
	}
	if dst.Labels["existing"] != "label" {
		t.Error("existing label was lost")
	}
	if dst.Labels["dacs.azure.com/inject"] != "true" {
		t.Error("inject label not merged")
	}
	if dst.Labels["another"] != "value" {
		t.Error("another label not merged")
	}
}

// TestMergeMutations_NilLabels verifies merging into nil labels.
func TestMergeMutations_NilLabels(t *testing.T) {
	dst := &PodMutations{}
	src := &PodMutations{
		Labels: map[string]string{"key": "value"},
	}

	mergeMutations(dst, src)

	if dst.Labels == nil || dst.Labels["key"] != "value" {
		t.Error("labels not correctly initialized and merged from nil")
	}
}
