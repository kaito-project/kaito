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

// Onboarding a new cache provider into this suite
// ------------------------------------------------
// These integration tests are provider-agnostic: every test iterates
// cacheProviderFixtures() and asserts purely against each fixture's registered
// Expectations, never against hardcoded labels or env var names. A new provider
// is exercised end-to-end here without touching any test function — you only
// supply two things:
//
//  1. A mock cache.Provider implementing the core interface (Name, IsAvailable,
//     IsReady, PodMutations, Cleanup) that faithfully reproduces the provider's
//     real pod-mutation surface: the injection label(s) and env vars it adds for
//     each CacheConcern (ModelWeights and/or KVCache). The mock is the contract
//     stand-in, so keep its output in lockstep with the production provider.
//
//  2. One cacheProviderFixture appended to cacheProviderFixtures() that pairs the
//     mock with an Expectations value declaring, per concern, the RequiredLabels
//     and RequiredEnvVars the provider emits. This is the single source of truth
//     the tests read via Expectations.ForConcern.
//
// From there the shared scenarios (model-weights only, KV only, both, disabled
// mode, nil cache) run automatically for the new provider: envExpectations
// derives which env vars must be present versus absent, and requiredLabels drives
// the pod-template label assertions. If a provider legitimately injects nothing
// for a concern, model that with Supported: false / ExpectEmpty on that concern's
// MutationExpectation rather than editing the tests.

// dacsTestProvider simulates the DACS provider for integration tests.
// It returns mutations matching the CSI-based approach: labels + env vars only.
type dacsTestProvider struct {
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
		// Label triggers webhook injection + RunAI streamer env vars.
		mutations.Labels = map[string]string{
			"dacs.azure.com/inject": "true",
		}
		mutations.EnvVars = append(mutations.EnvVars,
			corev1.EnvVar{Name: "RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "RUNAI_STREAMER_CACHE_ENABLED", Value: "true"},
		)

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

// cacheProviderFixture couples a mock cache Provider with the observable
// mutation surface the integration tests assert against. The Expectations carry
// the provider-specific labels and env vars per concern, so the tests never
// hardcode them. To exercise a new provider, append one fixture to
// cacheProviderFixtures and supply a mock Provider implementation — every test
// below iterates the fixtures dynamically, so no per-test edits are required.
type cacheProviderFixture struct {
	provider     Provider
	expectations Expectations
}

// cacheProviderFixtures is the single source of truth for the providers the
// integration suite exercises. Add a fixture here to cover a new provider.
func cacheProviderFixtures() []cacheProviderFixture {
	return []cacheProviderFixture{
		dacsFixture(),
	}
}

// dacsFixture builds the DACS mock provider together with its expected
// mutations per cache concern.
func dacsFixture() cacheProviderFixture {
	p := &dacsTestProvider{
		discoveryEP: "http://cacheserver-discovery.dacs-cache-system.svc.cluster.local:9065",
		kvEnabled:   true,
	}
	injectLabel := map[string]string{"dacs.azure.com/inject": "true"}
	return cacheProviderFixture{
		provider: p,
		expectations: Expectations{
			Provider: kaitov1beta1.CacheProvider(p.Name()),
			ModelWeights: MutationExpectation{
				Supported:      true,
				RequiredLabels: injectLabel,
				RequiredEnvVars: []string{
					"RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED",
					"RUNAI_STREAMER_CACHE_ENABLED",
				},
			},
			KVCache: MutationExpectation{
				Supported:       true,
				RequiredLabels:  injectLabel,
				RequiredEnvVars: []string{"VLLM_KV_TRANSFER_CONFIG"},
			},
		},
	}
}

// name returns the provider identifier used in Workspace specs.
func (f cacheProviderFixture) name() kaitov1beta1.CacheProvider {
	return f.expectations.Provider
}

// register adds the mock provider to the cache registry.
func (f cacheProviderFixture) register() {
	Register(f.provider)
}

// requiredLabels returns the labels the provider is expected to set for a
// concern (empty when the concern is unsupported).
func (f cacheProviderFixture) requiredLabels(concern CacheConcern) map[string]string {
	return f.expectations.ForConcern(concern).RequiredLabels
}

// envExpectations derives, from the fixture's registered Expectations, which env
// vars must be present and which must be absent for a workspace that enables the
// given concerns. This keeps the tests provider-agnostic: a new provider only
// declares its env vars in its fixture and these assertions follow automatically.
func (f cacheProviderFixture) envExpectations(modelWeights, kvCache bool) (present, absent []string) {
	mw := f.expectations.ForConcern(CacheConcernModelWeights).RequiredEnvVars
	kv := f.expectations.ForConcern(CacheConcernKVCache).RequiredEnvVars
	if modelWeights {
		present = append(present, mw...)
	} else {
		absent = append(absent, mw...)
	}
	if kvCache {
		present = append(present, kv...)
	} else {
		absent = append(absent, kv...)
	}
	return present, absent
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

// modelWeightsWorkspace builds a Workspace whose only cache concern is model weights.
func modelWeightsWorkspace(provider kaitov1beta1.CacheProvider, mode kaitov1beta1.CacheMode) *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{Provider: provider, Mode: mode},
		},
	}
}

// kvCacheWorkspace builds a Workspace whose only cache concern is KV cache.
func kvCacheWorkspace(provider kaitov1beta1.CacheProvider, mode kaitov1beta1.CacheMode) *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			KVCache: &kaitov1beta1.KVCacheSpec{Provider: provider, Mode: mode},
		},
	}
}

// bothConcernsWorkspace builds a Workspace that enables both cache concerns.
func bothConcernsWorkspace(provider kaitov1beta1.CacheProvider, mwMode, kvMode kaitov1beta1.CacheMode) *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{Provider: provider, Mode: mwMode},
			KVCache:    &kaitov1beta1.KVCacheSpec{Provider: provider, Mode: kvMode},
		},
	}
}

// TestSetCacheMutations_FullPipeline tests the complete mutation pipeline
// from feature gate check through to pod spec modification, for every
// registered provider fixture.
func TestSetCacheMutations_FullPipeline(t *testing.T) {
	for _, f := range cacheProviderFixtures() {
		f.register()

		t.Run(string(f.name()), func(t *testing.T) {
			tests := []struct {
				name         string
				workspace    *kaitov1beta1.Workspace
				modelWeights bool
				kvCache      bool
			}{
				{
					name:         "model weights only - label + streamer env vars",
					workspace:    modelWeightsWorkspace(f.name(), kaitov1beta1.CacheModeOpportunistic),
					modelWeights: true,
				},
				{
					name:      "KV cache only - KV config env var",
					workspace: kvCacheWorkspace(f.name(), kaitov1beta1.CacheModeRequired),
					kvCache:   true,
				},
				{
					name:         "both model weights and KV cache",
					workspace:    bothConcernsWorkspace(f.name(), kaitov1beta1.CacheModeOpportunistic, kaitov1beta1.CacheModeRequired),
					modelWeights: true,
					kvCache:      true,
				},
				{
					name:      "disabled mode - no mutations",
					workspace: modelWeightsWorkspace(f.name(), kaitov1beta1.CacheModeDisabled),
				},
				{
					name:      "nil cache - no mutations",
					workspace: &kaitov1beta1.Workspace{},
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
					defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

					expectEnvVars, expectNoEnvVars := f.envExpectations(tt.modelWeights, tt.kvCache)

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
					for _, name := range expectEnvVars {
						if _, ok := envMap[name]; !ok {
							t.Errorf("expected env var %s to be present", name)
						}
					}
					for _, name := range expectNoEnvVars {
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
		})
	}
}

// TestSetCacheMutations_ModelWeightsEnvVars verifies that the provider's model
// weight env vars are injected for model weight caching, for every fixture.
func TestSetCacheMutations_ModelWeightsEnvVars(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	for _, f := range cacheProviderFixtures() {
		f.register()

		t.Run(string(f.name()), func(t *testing.T) {
			ws := modelWeightsWorkspace(f.name(), kaitov1beta1.CacheModeOpportunistic)
			ws.Inference = &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{Name: "microsoft/phi-4"},
				},
			}

			ctx := &generator.WorkspaceGeneratorContext{
				Ctx:       context.Background(),
				Workspace: ws,
				Model:     &mockModel{name: "microsoft/phi-4"},
			}
			spec := &corev1.PodSpec{
				Containers: []corev1.Container{{Name: "model"}},
			}

			modifier := SetCacheMutations()
			if err := modifier(ctx, spec); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			envMap := map[string]string{}
			for _, e := range spec.Containers[0].Env {
				envMap[e.Name] = e.Value
			}
			for _, name := range f.expectations.ForConcern(CacheConcernModelWeights).RequiredEnvVars {
				if _, ok := envMap[name]; !ok {
					t.Errorf("expected model weights env var %s to be present", name)
				}
			}
		})
	}
}

// TestSetCacheMutations_PreservesExistingPodSpec verifies that cache mutations
// don't interfere with existing pod spec elements, for every fixture.
func TestSetCacheMutations_PreservesExistingPodSpec(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	for _, f := range cacheProviderFixtures() {
		f.register()

		t.Run(string(f.name()), func(t *testing.T) {
			ws := modelWeightsWorkspace(f.name(), kaitov1beta1.CacheModeRequired)
			ws.Inference = &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{Name: "microsoft/phi-4"},
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
			if err := modifier(ctx, spec); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify existing init container is preserved.
			if len(spec.InitContainers) != 1 {
				t.Fatalf("expected 1 init container (only existing), got %d", len(spec.InitContainers))
			}
			if spec.InitContainers[0].Name != "existing-init" {
				t.Error("existing init container was not preserved")
			}

			// Verify existing volume preserved (fake provider doesn't add ImageVolume).
			if len(spec.Volumes) != 1 {
				t.Fatalf("expected 1 volume (only existing), got %d", len(spec.Volumes))
			}

			// Verify existing env vars preserved and provider env vars added.
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
			for _, name := range f.expectations.ForConcern(CacheConcernModelWeights).RequiredEnvVars {
				if _, ok := envMap[name]; !ok {
					t.Errorf("expected model weights env var %s to be added", name)
				}
			}

			// Verify existing volume mounts preserved (fake provider doesn't add mounts).
			if len(spec.Containers[0].VolumeMounts) != 1 {
				t.Fatalf("expected 1 volume mount (only existing), got %d", len(spec.Containers[0].VolumeMounts))
			}
		})
	}
}

// TestSetCachePodTemplateLabels verifies that cache mutations produce the
// provider's injection labels for the relevant concern, for every fixture.
func TestSetCachePodTemplateLabels(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	for _, f := range cacheProviderFixtures() {
		f.register()

		t.Run(string(f.name()), func(t *testing.T) {
			tests := []struct {
				name        string
				workspace   *kaitov1beta1.Workspace
				concern     CacheConcern
				expectLabel bool
			}{
				{
					name:        "model weights enabled - label added",
					workspace:   modelWeightsWorkspace(f.name(), kaitov1beta1.CacheModeOpportunistic),
					concern:     CacheConcernModelWeights,
					expectLabel: true,
				},
				{
					name:        "KV cache enabled - label added",
					workspace:   kvCacheWorkspace(f.name(), kaitov1beta1.CacheModeRequired),
					concern:     CacheConcernKVCache,
					expectLabel: true,
				},
				{
					name:        "nil cache - no label",
					workspace:   &kaitov1beta1.Workspace{},
					expectLabel: false,
				},
				{
					name:        "disabled mode - no label",
					workspace:   modelWeightsWorkspace(f.name(), kaitov1beta1.CacheModeDisabled),
					concern:     CacheConcernModelWeights,
					expectLabel: false,
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					ctx := &generator.WorkspaceGeneratorContext{
						Ctx:       context.Background(),
						Workspace: tt.workspace,
						Model:     &mockModel{name: "microsoft/phi-4"},
					}

					if tt.workspace.Cache == nil {
						return
					}
					mutations, err := collectMutations(ctx.Ctx, nil, tt.workspace, "microsoft/phi-4", "main", nil)
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}

					hasLabel := mutations != nil && hasAllLabels(mutations.Labels, f.requiredLabels(tt.concern))
					if hasLabel != tt.expectLabel {
						t.Errorf("label present: got %v, want %v", hasLabel, tt.expectLabel)
					}
				})
			}
		})
	}
}

// hasAllLabels reports whether every key/value pair in want is present in got.
// It returns false for an empty want so callers can treat "no expected labels"
// as "no label present".
func hasAllLabels(got, want map[string]string) bool {
	if len(want) == 0 {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
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
