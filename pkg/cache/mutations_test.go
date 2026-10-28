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

func TestMutationHashChanged(t *testing.T) {
	attachment := &Attachment{
		ModelWeights: &ConcernConfig{
			Provider:   "test",
			Mode:       ModeOpportunistic,
			Parameters: map[string]string{"cacheName": "cache-a"},
		},
	}
	mutations := &PodMutations{
		EnvVars: []corev1.EnvVar{{Name: "CACHE_ENABLED", Value: "true"}},
	}

	existing := &appsv1.StatefulSet{}
	desired := &appsv1.StatefulSet{}
	if err := setMutationHash(existing, attachment, mutations); err != nil {
		t.Fatalf("setMutationHash(existing) returned error: %v", err)
	}
	if err := setMutationHash(desired, attachment, mutations); err != nil {
		t.Fatalf("setMutationHash(desired) returned error: %v", err)
	}
	if MutationHashChanged(existing, desired) {
		t.Fatal("identical cache configuration reported as changed")
	}

	changedAttachment := &Attachment{
		ModelWeights: &ConcernConfig{
			Provider:   "test",
			Mode:       ModeOpportunistic,
			Parameters: map[string]string{"cacheName": "cache-b"},
		},
	}
	if err := setMutationHash(desired, changedAttachment, mutations); err != nil {
		t.Fatalf("setMutationHash(changed desired) returned error: %v", err)
	}
	if !MutationHashChanged(existing, desired) {
		t.Fatal("changed cache attachment was not detected")
	}

	delete(desired.Spec.Template.Annotations, MutationHashAnnotation)
	if !MutationHashChanged(existing, desired) {
		t.Fatal("removed cache attachment was not detected")
	}
}

func TestApplyMutations(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "model",
				Image: "registry.example/kaito-base:v1",
				Env:   []corev1.EnvVar{{Name: "EXISTING", Value: "yes"}},
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
}

func TestApplyMutations_DeduplicatesVolumeMountsByPath(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:  "model",
			Image: "registry.example/kaito-base:v1",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "shared", MountPath: "/model"},
				{Name: "user-cache", MountPath: "/cache"},
			},
		}},
	}
	mutations := &PodMutations{
		VolumeMounts: []corev1.VolumeMount{
			{Name: "shared", MountPath: "/cache-client"},
			{Name: "provider-cache", MountPath: "/cache"},
		},
	}

	applyMutations(spec, mutations)

	mounts := spec.Containers[0].VolumeMounts
	if len(mounts) != 3 {
		t.Fatalf("expected same volume at a distinct path to be retained and duplicate path skipped, got %v", mounts)
	}
	if mounts[2].Name != "shared" || mounts[2].MountPath != "/cache-client" {
		t.Fatalf("unexpected appended mount: %#v", mounts[2])
	}
}

func TestApplyMutations_TargetsMatchingContainers(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{Name: "sidecar", Image: "registry.example/sidecar:v1"},
			{Name: "model-a", Image: "registry.example/kaito-base:v1"},
			{Name: "model-b", Image: "registry.example/kaito-base:v2"},
		},
	}
	mutations := &PodMutations{
		EnvVars:      []corev1.EnvVar{{Name: "CACHE_ENABLED", Value: "true"}},
		VolumeMounts: []corev1.VolumeMount{{Name: "cache", MountPath: "/cache"}},
	}

	applyMutations(spec, mutations)

	if len(spec.Containers[0].Env) != 0 || len(spec.Containers[0].VolumeMounts) != 0 {
		t.Error("expected sidecar to remain unchanged")
	}
	for i := 1; i < len(spec.Containers); i++ {
		if len(spec.Containers[i].Env) != 1 || len(spec.Containers[i].VolumeMounts) != 1 {
			t.Errorf("expected cache mutations on container %q", spec.Containers[i].Name)
		}
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

func TestMutationEntryPoints_InvalidOpportunisticAttachmentSkip(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workspace",
			Namespace: "inference",
			Annotations: map[string]string{
				ConfigAnnotation: "cache-attachment",
			},
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cache-attachment", Namespace: "inference"},
		Data: map[string]string{
			ConfigKey: "modelWeights:\n  provider: dacs\n  mode: Opportunistic\n  unknown: value\n",
		},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(configMap).Build()
	generatorContext := &generator.WorkspaceGeneratorContext{
		Ctx:        context.Background(),
		Workspace:  ws,
		KubeClient: kubeClient,
	}

	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "model", Image: "kaito-base:latest"}},
				},
			},
		},
	}
	if err := ApplyPresetCacheMutations(generatorContext.Ctx, generatorContext.Workspace,
		generatorContext.KubeClient, generatorContext.Model, ss); err != nil {
		t.Fatalf("ApplyPresetCacheMutations() error = %v", err)
	}
	if len(ss.Spec.Template.Spec.Containers[0].Env) != 0 || len(ss.Spec.Template.Spec.Volumes) != 0 {
		t.Fatal("invalid Opportunistic attachment must not mutate the pod spec")
	}

	templateSS := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: *ss.Spec.Template.DeepCopy(),
		},
	}
	if err := ApplyTemplateCacheMutations(context.Background(), ws, kubeClient, templateSS); err != nil {
		t.Fatalf("ApplyTemplateCacheMutations() error = %v", err)
	}
	if len(templateSS.Spec.Template.Labels) != 0 ||
		len(templateSS.Spec.Template.Spec.Containers[0].Env) != 0 ||
		len(templateSS.Spec.Template.Spec.Volumes) != 0 {
		t.Fatal("invalid Opportunistic attachment must not mutate the StatefulSet")
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

func (p *availabilityTestProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _ string, _ map[string]string) (*PodMutations, error) {
	p.mutationsCalls++
	if p.mutations != nil {
		return p.mutations, nil
	}
	return &PodMutations{}, nil
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

	ws := &kaitov1beta1.Workspace{}
	attachment := &Attachment{
		ModelWeights: &ConcernConfig{
			Provider: ProviderName(provider.name),
			Mode:     ModeOpportunistic,
		},
	}

	mutations, err := collectMutations(context.Background(), ws, attachment, "microsoft/phi-4", "main", nil)
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

	ws := &kaitov1beta1.Workspace{}
	attachment := &Attachment{
		ModelWeights: &ConcernConfig{
			Provider: ProviderName(provider.name),
			Mode:     ModeRequired,
		},
	}

	_, err := collectMutations(context.Background(), ws, attachment, "microsoft/phi-4", "main", nil)
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

	ws := &kaitov1beta1.Workspace{}
	attachment := &Attachment{
		ModelWeights: &ConcernConfig{
			Provider: "example",
			Mode:     ModeOpportunistic,
		},
		KVCache: &ConcernConfig{
			Provider: "example",
			Mode:     ModeRequired,
		},
	}

	mutations, err := collectMutations(context.Background(), ws, attachment, "microsoft/phi-4", "main", nil)
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

func (p *concernTestProvider) PodMutations(_ context.Context, concern CacheConcern, _ *kaitov1beta1.Workspace, _, _ string, _ map[string]string) (*PodMutations, error) {
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

type labelScopedProvider struct {
	concernTestProvider
	applicabilityCalls int
	mutationCalls      int
}

func (p *labelScopedProvider) AppliesTo(_ CacheConcern, _ *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) bool {
	p.applicabilityCalls++
	return ss != nil && ss.Spec.Template.Labels["workload-label"] == "enabled"
}

func (p *labelScopedProvider) PodMutations(ctx context.Context, concern CacheConcern, ws *kaitov1beta1.Workspace, modelName, modelRevision string, parameters map[string]string) (*PodMutations, error) {
	p.mutationCalls++
	return p.concernTestProvider.PodMutations(ctx, concern, ws, modelName, modelRevision, parameters)
}

type imageScopedProvider struct {
	concernTestProvider
	expectedImage string
	observedImage string
}

func (p *imageScopedProvider) AppliesTo(_ CacheConcern, _ *kaitov1beta1.Workspace, ss *appsv1.StatefulSet) bool {
	if ss == nil || len(ss.Spec.Template.Spec.Containers) == 0 {
		return false
	}
	p.observedImage = ss.Spec.Template.Spec.Containers[0].Image
	return p.observedImage == p.expectedImage
}

func TestCollectMutations_WithBothConcernsMergesLabelsAndEnvVars(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &concernTestProvider{name: "concern-test"}
	Register(provider)

	ws := &kaitov1beta1.Workspace{}
	attachment := &Attachment{
		ModelWeights: &ConcernConfig{
			Provider: ProviderName(provider.name),
			Mode:     ModeRequired,
		},
		KVCache: &ConcernConfig{
			Provider: ProviderName(provider.name),
			Mode:     ModeRequired,
		},
	}

	mutations, err := collectMutations(context.Background(), ws, attachment, "ignored", "ignored", nil)
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

func TestApplyPresetCacheMutations_AppliesLabels(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	provider := &labelScopedProvider{
		concernTestProvider: concernTestProvider{name: "concern-test"},
	}
	Register(provider)

	ws := &kaitov1beta1.Workspace{}
	kubeClient := attachTestConfig(t, ws, &Attachment{
		ModelWeights: &ConcernConfig{Provider: ProviderName(provider.name), Mode: ModeRequired},
	})
	ctx := &generator.WorkspaceGeneratorContext{
		Ctx:        context.Background(),
		Workspace:  ws,
		KubeClient: kubeClient,
		Model: &mockModel{
			name:    "microsoft/phi-4",
			version: "https://huggingface.co/microsoft/phi-4/commit/revision-a",
		},
	}
	baseLabels := map[string]string{"workload-label": "enabled"}
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: baseLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: baseLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "model", Image: "kaito-base:latest"}},
				},
			},
		},
	}

	if err := ApplyPresetCacheMutations(ctx.Ctx, ctx.Workspace, ctx.KubeClient, ctx.Model, ss); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ss.Spec.Template.Labels["model-label"] != "enabled" {
		t.Fatalf("expected pod template label to be applied, got %v", ss.Spec.Template.Labels)
	}
	if _, exists := ss.Spec.Selector.MatchLabels["model-label"]; exists {
		t.Fatalf("cache label leaked into immutable selector: %v", ss.Spec.Selector.MatchLabels)
	}
	if len(ss.Spec.Template.Spec.Containers[0].Env) != 1 ||
		ss.Spec.Template.Spec.Containers[0].Env[0].Name != "MODEL_ENV" {
		t.Fatalf("expected cache env var to be applied, got %v", ss.Spec.Template.Spec.Containers[0].Env)
	}
	if provider.applicabilityCalls != 1 || provider.mutationCalls != 1 {
		t.Fatalf("expected one applicability and mutation call, got applicability=%d mutations=%d",
			provider.applicabilityCalls, provider.mutationCalls)
	}
	if got := ss.Spec.Template.Annotations[cacheModelNameAnnotation]; got != "microsoft/phi-4" {
		t.Fatalf("model identity annotation: got %q, want %q", got, "microsoft/phi-4")
	}
	if got := ss.Spec.Template.Annotations[cacheModelRevisionAnnotation]; got != "revision-a" {
		t.Fatalf("model revision annotation: got %q, want %q", got, "revision-a")
	}
}

func TestApplyPresetCacheMutations_UsesRenderedImageForApplicability(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &imageScopedProvider{
		concernTestProvider: concernTestProvider{name: "image-scoped-test"},
		expectedImage:       "kaito-base:v1",
	}
	Register(provider)

	ws := &kaitov1beta1.Workspace{}
	kubeClient := attachTestConfig(t, ws, &Attachment{
		ModelWeights: &ConcernConfig{Provider: ProviderName(provider.name), Mode: ModeRequired},
	})
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "model", Image: "kaito-base:v1"}},
				},
			},
		},
	}

	if err := ApplyPresetCacheMutations(context.Background(), ws, kubeClient,
		&mockModel{name: "microsoft/phi-4"}, ss); err != nil {
		t.Fatalf("ApplyPresetCacheMutations returned error: %v", err)
	}

	if provider.observedImage != provider.expectedImage {
		t.Fatalf("provider observed image %q, want %q", provider.observedImage, provider.expectedImage)
	}
	if ss.Spec.Template.Labels["model-label"] != "enabled" {
		t.Fatalf("expected image-applicable cache mutations, got labels %v", ss.Spec.Template.Labels)
	}
}

func TestApplyTemplateCacheMutations_AppliesLabelsAndEnvVars(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &concernTestProvider{name: "concern-test"}
	Register(provider)

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "workspace"},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{
		ModelWeights: &ConcernConfig{Provider: ProviderName(provider.name), Mode: ModeRequired},
		KVCache:      &ConcernConfig{Provider: ProviderName(provider.name), Mode: ModeRequired},
	})
	ss := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "model",
						Image: "kaito-base:latest",
						Args:  []string{"--load-format=runai_streamer", "--model=az://acct/container/model"},
						Env:   []corev1.EnvVar{{Name: "EXISTING", Value: "keep"}},
					}},
				},
			},
		},
	}

	if err := ApplyTemplateCacheMutations(context.Background(), ws, kubeClient, ss); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
	c := corev1.Container{Name: "model", Image: "kaito-base:latest"}
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
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: ProviderName(provider.name), Mode: ModeOpportunistic,
	}})
	ss := newTemplateSS(false)

	if err := ApplyTemplateCacheMutations(context.Background(), ws, kubeClient, ss); err != nil {
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
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: ProviderName(provider.name), Mode: ModeRequired,
	}})
	ss := newTemplateSS(false)

	if err := ApplyTemplateCacheMutations(context.Background(), ws, kubeClient, ss); err != nil {
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
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: ProviderName(provider.name), Mode: ModeOpportunistic,
	}})
	ss := newTemplateSS(true)

	if err := ApplyTemplateCacheMutations(context.Background(), ws, kubeClient, ss); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ss.Spec.Template.Labels["model-label"] != "enabled" {
		t.Fatalf("expected cache label when provider is applicable, got %v", ss.Spec.Template.Labels)
	}
}
