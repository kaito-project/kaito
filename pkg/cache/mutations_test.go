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
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

func TestMergeMutations_DeduplicatesEnvVars(t *testing.T) {
	dst := &PodMutations{
		EnvVars: []corev1.EnvVar{
			{Name: "A", Value: "1"},
			{Name: "B", Value: "2"},
		},
	}
	src := &PodMutations{
		EnvVars: []corev1.EnvVar{
			{Name: "B", Value: "overridden"}, // duplicate, should be skipped
			{Name: "C", Value: "3"},
		},
	}

	mergeMutations(dst, src)

	if len(dst.EnvVars) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(dst.EnvVars))
	}
	// B should keep original value (first wins)
	for _, e := range dst.EnvVars {
		if e.Name == "B" && e.Value != "2" {
			t.Errorf("expected B=2 (first wins), got B=%s", e.Value)
		}
	}
}

func TestMergeMutations_NilSrc(t *testing.T) {
	dst := &PodMutations{
		EnvVars: []corev1.EnvVar{{Name: "A", Value: "1"}},
	}
	mergeMutations(dst, nil)

	if len(dst.EnvVars) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(dst.EnvVars))
	}
}

func TestApplyMutations(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "model",
				Env:  []corev1.EnvVar{{Name: "EXISTING", Value: "yes"}},
			},
		},
	}

	mutations := &PodMutations{
		EnvVars: []corev1.EnvVar{
			{Name: "CACHE_ENABLED", Value: "true"},
		},
		Volumes: []corev1.Volume{
			{Name: "cache-vol"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "cache-vol", MountPath: "/cache"},
		},
		InitContainers: []corev1.Container{
			{Name: "cache-init"},
		},
	}

	applyMutations(spec, mutations)

	if len(spec.Containers[0].Env) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(spec.Containers[0].Env))
	}
	if len(spec.Containers[0].VolumeMounts) != 1 {
		t.Errorf("expected 1 volume mount, got %d", len(spec.Containers[0].VolumeMounts))
	}
	if len(spec.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(spec.Volumes))
	}
	if len(spec.InitContainers) != 1 {
		t.Errorf("expected 1 init container, got %d", len(spec.InitContainers))
	}
}

func TestApplyMutations_EmptyContainers(t *testing.T) {
	spec := &corev1.PodSpec{}
	mutations := &PodMutations{
		EnvVars: []corev1.EnvVar{{Name: "A", Value: "1"}},
	}

	// Should not panic with empty containers
	applyMutations(spec, mutations)

	if len(spec.Containers) != 0 {
		t.Errorf("expected no containers modified")
	}
}

type availabilityTestProvider struct {
	name           string
	available      bool
	availableErr   error
	mutations      *PodMutations
	mutationsCalls int
}

func (p *availabilityTestProvider) Name() string { return p.name }

func (p *availabilityTestProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return p.available, p.availableErr
}

func (p *availabilityTestProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return p.available, "", nil
}

func (p *availabilityTestProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _, _ string) (*PodMutations, error) {
	p.mutationsCalls++
	if p.mutations != nil {
		return p.mutations, nil
	}
	return &PodMutations{}, nil
}

func (p *availabilityTestProvider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}

func TestCollectMutations_OpportunisticUnavailableProviderSkips(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &availabilityTestProvider{
		name:      "availability-test",
		available: false,
		mutations: &PodMutations{
			Labels:  map[string]string{"should-not": "appear"},
			EnvVars: []corev1.EnvVar{{Name: "SHOULD_NOT_APPEAR", Value: "true"}},
		},
	}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}

	mutations, err := collectMutations(context.Background(), nil, ws, "microsoft/phi-4", "main", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.mutationsCalls != 0 {
		t.Fatalf("expected PodMutations not to be called, got %d calls", provider.mutationsCalls)
	}
	if len(mutations.EnvVars) != 0 {
		t.Fatalf("expected no env vars, got %v", mutations.EnvVars)
	}
	if len(mutations.Labels) != 0 {
		t.Fatalf("expected no labels, got %v", mutations.Labels)
	}
}

func TestCollectMutations_RequiredUnavailableProviderReturnsError(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &availabilityTestProvider{name: "availability-test", available: false}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}

	_, err := collectMutations(context.Background(), nil, ws, "microsoft/phi-4", "main", nil)
	if err == nil {
		t.Fatal("expected error for unavailable required provider")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected availability error, got %v", err)
	}
}

func TestCollectMutations_BothConcernsMergeResults(t *testing.T) {
	isolateProviderRegistry(t)

	Register(&cacheTestProvider{
		discoveryEP: "cache-sample-discovery.cache-system.svc.cluster.local",
		kvEnabled:   true,
	})

	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "example",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "example",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}

	mutations, err := collectMutations(context.Background(), nil, ws, "microsoft/phi-4", "main", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mutations.Labels["cache.example.com/inject"] != "true" {
		t.Fatalf("expected cache injection label, got %v", mutations.Labels)
	}
	if len(mutations.EnvVars) != 3 {
		t.Fatalf("expected model weight + KV env vars (3 total), got %v", mutations.EnvVars)
	}

	envVars := map[string]string{}
	for _, envVar := range mutations.EnvVars {
		envVars[envVar.Name] = envVar.Value
	}
	if envVars["RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED"] != "true" {
		t.Fatalf("expected RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED to be set, got %q", envVars["RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED"])
	}
	if _, ok := envVars["VLLM_KV_TRANSFER_CONFIG"]; !ok {
		t.Fatalf("expected VLLM_KV_TRANSFER_CONFIG to be set, got %v", envVars)
	}
}

type concernTestProvider struct {
	name string
}

func (p *concernTestProvider) Name() string { return p.name }

func (p *concernTestProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (p *concernTestProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "ready", nil
}

func (p *concernTestProvider) PodMutations(_ context.Context, concern CacheConcern, _ *kaitov1beta1.Workspace, _, _, _ string) (*PodMutations, error) {
	switch concern {
	case CacheConcernModelWeights:
		return &PodMutations{
			Labels:  map[string]string{"model-label": "enabled"},
			EnvVars: []corev1.EnvVar{{Name: "MODEL_ENV", Value: "weights"}},
		}, nil
	case CacheConcernKVCache:
		return &PodMutations{
			EnvVars: []corev1.EnvVar{{Name: "KV_ENV", Value: "cache"}},
		}, nil
	default:
		return &PodMutations{}, nil
	}
}

func (p *concernTestProvider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}

// streamerScopedProvider is a concernTestProvider that additionally declares its
// applicability via PodApplicabilityChecker: it only engages with a workload that
// loads the model through the run:ai streamer. It exists to prove the framework
// delegates the applicability decision to the provider rather than hardcoding a
// load-format check.
type streamerScopedProvider struct {
	concernTestProvider
}

func (p *streamerScopedProvider) AppliesTo(_ CacheConcern, _ *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) bool {
	if ss == nil {
		return false
	}
	for _, c := range ss.Spec.Template.Spec.Containers {
		for _, arg := range append(append([]string{}, c.Command...), c.Args...) {
			if strings.Contains(arg, "runai_streamer") {
				return true
			}
		}
	}
	return false
}

func TestCollectMutations_WithBothConcernsMergesLabelsAndEnvVars(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &concernTestProvider{name: "concern-test"}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeRequired,
			},
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}

	mutations, err := collectMutations(context.Background(), nil, ws, "ignored", "ignored", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mutations.Labels["model-label"] != "enabled" {
		t.Fatalf("expected model weights label to be merged, got %v", mutations.Labels)
	}
	if len(mutations.EnvVars) != 2 {
		t.Fatalf("expected env vars from both concerns, got %v", mutations.EnvVars)
	}
	envVars := map[string]string{}
	for _, envVar := range mutations.EnvVars {
		envVars[envVar.Name] = envVar.Value
	}
	if envVars["MODEL_ENV"] != "weights" {
		t.Fatalf("MODEL_ENV: got %q", envVars["MODEL_ENV"])
	}
	if envVars["KV_ENV"] != "cache" {
		t.Fatalf("KV_ENV: got %q", envVars["KV_ENV"])
	}
}

func TestSetCachePodTemplateLabels_ModifierAppliesLabels(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	provider := &concernTestProvider{name: "concern-test"}
	Register(provider)

	ctx := &generator.WorkspaceGeneratorContext{
		Ctx: context.Background(),
		Workspace: &kaitov1beta1.Workspace{
			Cache: &kaitov1beta1.CacheSpec{
				ModelCache: &kaitov1beta1.ModelCacheSpec{
					Provider: kaitov1beta1.CacheProvider(provider.name),
					Mode:     kaitov1beta1.CacheModeRequired,
				},
			},
		},
	}
	ss := &appsv1.StatefulSet{}

	if err := SetCachePodTemplateLabels()(ctx, ss); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ss.Spec.Template.Labels["model-label"] != "enabled" {
		t.Fatalf("expected pod template label to be applied, got %v", ss.Spec.Template.Labels)
	}
}

func TestApplyTemplateCacheMutations_AppliesLabelsAndEnvVars(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &concernTestProvider{name: "concern-test"}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeRequired,
			},
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "model",
						Args: []string{"--load-format=runai_streamer", "--model=az://acct/container/model"},
						Env:  []corev1.EnvVar{{Name: "EXISTING", Value: "keep"}},
					}},
				},
			},
		},
	}

	ApplyTemplateCacheMutations(context.Background(), ws, nil, ss)

	if ss.Spec.Template.Labels["model-label"] != "enabled" {
		t.Fatalf("expected label to be applied, got %v", ss.Spec.Template.Labels)
	}
	envVars := map[string]string{}
	for _, envVar := range ss.Spec.Template.Spec.Containers[0].Env {
		envVars[envVar.Name] = envVar.Value
	}
	if envVars["EXISTING"] != "keep" {
		t.Fatal("existing env var was not preserved")
	}
	if envVars["MODEL_ENV"] != "weights" {
		t.Fatalf("MODEL_ENV: got %q", envVars["MODEL_ENV"])
	}
	if envVars["KV_ENV"] != "cache" {
		t.Fatalf("KV_ENV: got %q", envVars["KV_ENV"])
	}
}

// newTemplateSS builds a StatefulSet whose single container optionally loads the
// model via the run:ai streamer, for exercising the cache-applicability gate.
func newTemplateSS(usesRunaiStreamer bool) *appsv1.StatefulSet {
	c := corev1.Container{Name: "model"}
	if usesRunaiStreamer {
		c.Args = []string{"--load-format=runai_streamer", "--model=az://acct/container/model"}
	} else {
		c.Args = []string{"--load_format=auto", "--model=microsoft/phi-4"}
	}
	return &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{c}},
			},
		},
	}
}

// TestApplyTemplateCacheMutations_SkipsWhenProviderNotApplicable verifies that a
// Preferred-mode cache is silently skipped (no label/injection) when the provider
// reports it is not applicable to the workload (here: a template that does not
// load the model via runai_streamer), avoiding a no-op.
func TestApplyTemplateCacheMutations_SkipsWhenProviderNotApplicable(t *testing.T) {
	isolateProviderRegistry(t)
	provider := &streamerScopedProvider{concernTestProvider{name: "concern-test"}}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}
	ss := newTemplateSS(false)

	if err := ApplyTemplateCacheMutations(context.Background(), ws, nil, ss); err != nil {
		t.Fatalf("expected no error in Preferred mode, got %v", err)
	}
	if len(ss.Spec.Template.Labels) != 0 {
		t.Fatalf("expected no cache label when provider is not applicable, got %v", ss.Spec.Template.Labels)
	}
}

// TestApplyTemplateCacheMutations_SkipsInRequiredMode verifies that a
// Required-mode cache is also skipped without error (not blocked) when the
// provider declares it is not applicable to the workload, since injecting could
// never engage the cache.
func TestApplyTemplateCacheMutations_SkipsInRequiredMode(t *testing.T) {
	isolateProviderRegistry(t)
	provider := &streamerScopedProvider{concernTestProvider{name: "concern-test"}}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	ss := newTemplateSS(false)

	if err := ApplyTemplateCacheMutations(context.Background(), ws, nil, ss); err != nil {
		t.Fatalf("expected no error in Required mode when provider is not applicable, got %v", err)
	}
	if len(ss.Spec.Template.Labels) != 0 {
		t.Fatalf("expected no cache label when provider is not applicable, got %v", ss.Spec.Template.Labels)
	}
}

// TestApplyTemplateCacheMutations_AppliesWhenProviderApplicable is the positive
// counterpart: the same applicability-scoped provider DOES inject when the
// template loads the model via the run:ai streamer.
func TestApplyTemplateCacheMutations_AppliesWhenProviderApplicable(t *testing.T) {
	isolateProviderRegistry(t)
	provider := &streamerScopedProvider{concernTestProvider{name: "concern-test"}}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: kaitov1beta1.CacheProvider(provider.name),
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}
	ss := newTemplateSS(true)

	if err := ApplyTemplateCacheMutations(context.Background(), ws, nil, ss); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ss.Spec.Template.Labels["model-label"] != "enabled" {
		t.Fatalf("expected cache label when provider is applicable, got %v", ss.Spec.Template.Labels)
	}
}

func TestMountCacheConfigMaps_ModelCacheConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create a user ConfigMap
	userCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-model-config", Namespace: "default"},
		Data:       map[string]string{"key1": "userval1", "key2": "userval2"},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(userCM).Build()

	// Register a mock provider with DefaultConfig
	Register(&mockDefaultConfigProvider{name: "example", defaults: map[string]string{
		"key1":     "default1",
		"key3":     "default3",
		"endpoint": "discovery.svc:9065",
	}})
	defer func() {
		mu.Lock()
		delete(providers, "example")
		mu.Unlock()
	}()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ws", Namespace: "default"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "example",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
				Config:   "my-model-config",
			},
		},
	}

	mutations := &PodMutations{}
	mountCacheConfigMaps(context.Background(), kubeClient, ws, mutations)

	// Verify a volume was added
	if len(mutations.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(mutations.Volumes))
	}
	if mutations.Volumes[0].Name != "cache-model-config" {
		t.Errorf("expected volume name 'cache-model-config', got %q", mutations.Volumes[0].Name)
	}

	// Verify a volume mount was added
	if len(mutations.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volumeMount, got %d", len(mutations.VolumeMounts))
	}
	if mutations.VolumeMounts[0].MountPath != "/etc/kaito/cache/model" {
		t.Errorf("expected mountPath '/etc/kaito/cache/model', got %q", mutations.VolumeMounts[0].MountPath)
	}

	// Verify the runtime ConfigMap was created with merged data
	runtimeCM := &corev1.ConfigMap{}
	err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "cache-model-test-ws"}, runtimeCM)
	if err != nil {
		t.Fatalf("runtime ConfigMap not created: %v", err)
	}
	// User value overrides default
	if runtimeCM.Data["key1"] != "userval1" {
		t.Errorf("expected key1='userval1' (user override), got %q", runtimeCM.Data["key1"])
	}
	// User-only key preserved
	if runtimeCM.Data["key2"] != "userval2" {
		t.Errorf("expected key2='userval2', got %q", runtimeCM.Data["key2"])
	}
	// Default-only key preserved
	if runtimeCM.Data["key3"] != "default3" {
		t.Errorf("expected key3='default3' (from defaults), got %q", runtimeCM.Data["key3"])
	}
}

func TestMountCacheConfigMaps_NilClient(t *testing.T) {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ws", Namespace: "default"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "example",
				Config:   "my-config",
			},
		},
	}
	mutations := &PodMutations{}
	mountCacheConfigMaps(context.Background(), nil, ws, mutations)
	if len(mutations.Volumes) != 0 {
		t.Error("expected no volumes when client is nil")
	}
}

func TestMountCacheConfigMaps_NoConfigSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ws", Namespace: "default"},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "example",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
				// No Config field set
			},
		},
	}
	mutations := &PodMutations{}
	mountCacheConfigMaps(context.Background(), kubeClient, ws, mutations)
	if len(mutations.Volumes) != 0 {
		t.Error("expected no volumes when Config is empty")
	}
}

// mockDefaultConfigProvider implements Provider + DefaultConfigProvider for testing
type mockDefaultConfigProvider struct {
	name     string
	defaults map[string]string
}

func (m *mockDefaultConfigProvider) Name() string { return m.name }
func (m *mockDefaultConfigProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (m *mockDefaultConfigProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "ready", nil
}
func (m *mockDefaultConfigProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _, _ string) (*PodMutations, error) {
	return &PodMutations{}, nil
}
func (m *mockDefaultConfigProvider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}
func (m *mockDefaultConfigProvider) DefaultConfig(concern string) map[string]string {
	return m.defaults
}

// TestBuildRuntimeConfigMap_SkipsUnchanged verifies that buildRuntimeConfigMap
// does NOT update the runtime ConfigMap when the merged data is unchanged,
// avoiding unnecessary API writes and watch-triggered reconciles.
func TestBuildRuntimeConfigMap_SkipsUnchanged(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	userCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "user-cfg", Namespace: "default"},
		Data:       map[string]string{"discoveryEndpoint": "ep.example"},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(userCM).Build()

	ws := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "ws1", Namespace: "default"}}

	name := buildRuntimeConfigMap(context.Background(), kubeClient, ws,
		"user-cfg", kaitov1beta1.CacheProvider("example"), "model")
	if name == "" {
		t.Fatal("expected a runtime ConfigMap name")
	}

	key := client.ObjectKey{Namespace: "default", Name: name}
	created := &corev1.ConfigMap{}
	if err := kubeClient.Get(context.Background(), key, created); err != nil {
		t.Fatalf("runtime ConfigMap not created: %v", err)
	}
	rvCreate := created.ResourceVersion

	// Second identical call must NOT write (data unchanged) → resourceVersion stays the same.
	if got := buildRuntimeConfigMap(context.Background(), kubeClient, ws,
		"user-cfg", kaitov1beta1.CacheProvider("example"), "model"); got != name {
		t.Fatalf("expected stable runtime CM name, got %q want %q", got, name)
	}
	updated := &corev1.ConfigMap{}
	if err := kubeClient.Get(context.Background(), key, updated); err != nil {
		t.Fatalf("runtime ConfigMap missing after update: %v", err)
	}
	if updated.ResourceVersion != rvCreate {
		t.Errorf("expected no update (same resourceVersion), got %q want %q", updated.ResourceVersion, rvCreate)
	}
}
