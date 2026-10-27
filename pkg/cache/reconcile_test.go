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
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type statefulSetGetErrorClient struct {
	client.Client
	err error
}

func (c *statefulSetGetErrorClient) Get(ctx context.Context, key client.ObjectKey,
	obj client.Object, opts ...client.GetOption) error {
	if _, isStatefulSet := obj.(*appsv1.StatefulSet); isStatefulSet {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestReconcileCache_NilCache(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
	if result.BlockDeployment {
		t.Error("should not block when cache is nil")
	}
}

func TestReconcileCache_StatefulSetReadFailureDowngradesAndRequeues(t *testing.T) {
	isolateProviderRegistry(t)
	Register(&concernTestProvider{name: "inject-test"})

	ws := modelCacheWorkspace()
	kubeClient := &statefulSetGetErrorClient{
		Client: attachTestConfig(t, ws, modelCacheAttachment("inject-test")),
		err:    errors.New("API server unavailable"),
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)

	if result.BlockDeployment {
		t.Fatal("Opportunistic cache verification failure must not block deployment")
	}
	if !result.RequeueNeeded {
		t.Fatal("expected retry after StatefulSet read failure")
	}

	condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("expected unverified cache readiness to be False, got %#v", condition)
	}
	if !strings.Contains(condition.Message, "API server unavailable") {
		t.Fatalf("expected verification error in condition, got %q", condition.Message)
	}
}

func TestReconcileCache_NoAttachmentRemovesStaleCacheConditions(t *testing.T) {
	ws := &kaitov1beta1.Workspace{}
	status := &kaitov1beta1.WorkspaceStatus{
		Conditions: []metav1.Condition{
			{Type: string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady), Status: metav1.ConditionTrue},
			{Type: string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady), Status: metav1.ConditionTrue},
			{Type: string(kaitov1beta1.ConditionTypeResourceStatus), Status: metav1.ConditionTrue},
		},
	}

	ReconcileCache(context.Background(), nil, ws, status)

	if condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady)); condition != nil {
		t.Fatalf("expected stale model cache condition to be removed, got %#v", condition)
	}
	if condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady)); condition != nil {
		t.Fatalf("expected stale KV cache condition to be removed, got %#v", condition)
	}
	if condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.ConditionTypeResourceStatus)); condition == nil {
		t.Fatal("expected unrelated condition to be preserved")
	}
}

func TestReconcileCache_RemovesConditionForAbsentConcern(t *testing.T) {
	isolateProviderRegistry(t)
	Register(&noopTestProvider{})

	ws := &kaitov1beta1.Workspace{}
	kubeClient := attachTestConfig(t, ws, &Attachment{
		KVCache: &ConcernConfig{Provider: "noop-test", Mode: ModeOpportunistic},
	})
	status := &kaitov1beta1.WorkspaceStatus{
		Conditions: []metav1.Condition{{
			Type:   string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady),
			Status: metav1.ConditionTrue,
		}},
	}

	ReconcileCache(context.Background(), kubeClient, ws, status)

	if condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady)); condition != nil {
		t.Fatalf("expected removed concern's condition to be removed, got %#v", condition)
	}
	if condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady)); condition == nil {
		t.Fatal("expected configured KV cache condition to be retained")
	}
}

func TestReconcileCache_NoopProvider_Opportunistic(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	// Ensure noop is registered
	Register(&noopTestProvider{})

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: "noop-test", Mode: ModeOpportunistic,
	}})
	if err := appsv1.AddToScheme(kubeClient.Scheme()); err != nil {
		t.Fatalf("add apps API to test scheme: %v", err)
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if result.BlockDeployment {
		t.Error("should not block in Opportunistic mode")
	}

	// Should have set condition
	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil {
		t.Fatal("expected ModelWeightsCacheReady condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition False while workload is absent, got %s", cond.Status)
	}
}

func TestReconcileCache_PreservesTransitionTimeWhenFinalStatusIsUnchanged(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&concernTestProvider{name: "inject-test"})
	ws := modelCacheWorkspace()
	kubeClient := attachTestConfig(t, ws, modelCacheAttachment("inject-test"))
	if err := appsv1.AddToScheme(kubeClient.Scheme()); err != nil {
		t.Fatalf("add apps API to test scheme: %v", err)
	}

	transitionTime := metav1.NewTime(time.Unix(100, 0))
	status := &kaitov1beta1.WorkspaceStatus{
		Conditions: []metav1.Condition{{
			Type:               string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady),
			Status:             metav1.ConditionFalse,
			Reason:             "CacheNotReady",
			Message:            "inference workload not yet created",
			ObservedGeneration: ws.GetGeneration(),
			LastTransitionTime: transitionTime,
		}},
	}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if result.BlockDeployment {
		t.Fatal("Opportunistic cache should not block deployment")
	}

	cond := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil {
		t.Fatal("expected ModelCacheReady condition")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected final condition False while workload is absent, got %s", cond.Status)
	}
	if !cond.LastTransitionTime.Time.Equal(transitionTime.Time) {
		t.Fatalf("transition time changed without a final status transition: got %v, want %v",
			cond.LastTransitionTime, transitionTime)
	}
}

func TestReconcileCache_UnregisteredProvider_Required(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: "nonexistent", Mode: ModeRequired,
	}})
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if !result.BlockDeployment {
		t.Error("should block when Required mode provider is not registered")
	}
	if !result.RequeueNeeded {
		t.Error("should requeue when blocking")
	}
}

func TestReconcileCache_UnregisteredProvider_Opportunistic(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: "nonexistent", Mode: ModeOpportunistic,
	}})
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if result.BlockDeployment {
		t.Error("should not block in Opportunistic mode even if provider missing")
	}
}

func TestReconcileCache_RequiredBlocksIndefinitely(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&statefulTestProvider{name: "unavailable-test", available: false})

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			// Even long after creation, Required mode keeps blocking until ready.
			CreationTimestamp: metav1.NewTime(time.Now().Add(-24 * time.Hour)),
		},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{ModelWeights: &ConcernConfig{
		Provider: "unavailable-test", Mode: ModeRequired,
	}})
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if !result.BlockDeployment {
		t.Fatal("Required mode should block deployment until the cache is ready, regardless of elapsed time")
	}
	if !result.RequeueNeeded {
		t.Fatal("Required mode should requeue while the cache is not ready")
	}

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil {
		t.Fatal("expected model cache ready condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected condition False, got %s", cond.Status)
	}
}

func TestReconcileCache_KVCacheRequiredUnregisteredProviderBlocks(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{KVCache: &ConcernConfig{
		Provider: "missing-kv-provider", Mode: ModeRequired,
	}})
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if !result.BlockDeployment {
		t.Fatal("should block when required KV cache provider is not registered")
	}
	if !result.RequeueNeeded {
		t.Fatal("should requeue when required KV cache is blocking deployment")
	}
}

func TestReconcileCache_BothConcernsOneFailsStillBlocks(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&noopTestProvider{})

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
	}
	kubeClient := attachTestConfig(t, ws, &Attachment{
		ModelWeights: &ConcernConfig{Provider: "noop-test", Mode: ModeRequired},
		KVCache:      &ConcernConfig{Provider: "missing-kv-provider", Mode: ModeRequired},
	})
	if err := appsv1.AddToScheme(kubeClient.Scheme()); err != nil {
		t.Fatalf("add apps API to test scheme: %v", err)
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), kubeClient, ws, status)
	if !result.BlockDeployment {
		t.Fatal("should block when either required cache concern is not ready")
	}

	modelCond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if modelCond == nil || modelCond.Status != metav1.ConditionFalse {
		t.Fatalf("expected model cache to be not injected while deployment is blocked, got %#v", modelCond)
	}

	kvCond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady))
	if kvCond == nil || kvCond.Status != metav1.ConditionFalse {
		t.Fatalf("expected KV cache to be not ready, got %#v", kvCond)
	}
}

// noopTestProvider is a simple test provider that's always ready.
type noopTestProvider struct{}

func (p *noopTestProvider) Name() string                                          { return "noop-test" }
func (p *noopTestProvider) IsAvailable(_ context.Context, _ string) (bool, error) { return true, nil }
func (p *noopTestProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "always ready", nil
}
func (p *noopTestProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _ string, _ map[string]string) (*PodMutations, error) {
	return &PodMutations{}, nil
}

type fixedMutationProvider struct {
	name      ProviderName
	mutations *PodMutations
}

func (p *fixedMutationProvider) Name() string { return string(p.name) }
func (p *fixedMutationProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (p *fixedMutationProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "ready", nil
}
func (p *fixedMutationProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _ string, _ map[string]string) (*PodMutations, error) {
	return p.mutations, nil
}

type modelIdentityProvider struct {
	name ProviderName
}

func (p *modelIdentityProvider) Name() string { return string(p.name) }
func (p *modelIdentityProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (p *modelIdentityProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return true, "ready", nil
}
func (p *modelIdentityProvider) PodMutations(_ context.Context, _ CacheConcern,
	_ *kaitov1beta1.Workspace, modelName, modelRevision string,
	_ map[string]string) (*PodMutations, error) {
	return &PodMutations{
		Labels: map[string]string{
			"cache.test/model": modelName + "-" + modelRevision,
		},
	}, nil
}

// stsWithPodLabels builds a minimal StatefulSet whose pod template carries the
// given labels (and optional container args), for exercising injection
// verification against a rendered workload.
func stsWithPodLabels(name, namespace string, podLabels map[string]string, args ...string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "main",
						Image: "kaito-base:latest",
						Args:  args,
					}},
				},
			},
		},
	}
}

func modelCacheWorkspace() *kaitov1beta1.Workspace {
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "default", CreationTimestamp: metav1.Now()},
	}
}

func modelCacheAttachment(provider ProviderName) *Attachment {
	return &Attachment{ModelWeights: &ConcernConfig{Provider: provider, Mode: ModeOpportunistic}}
}

func TestVerifyCacheInjection_NilStatefulSet_Downgrades(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&concernTestProvider{name: "inject-test"})
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("inject-test")

	status := &kaitov1beta1.WorkspaceStatus{}
	// Pre-set the condition True to prove verification downgrades it.
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, nil, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected ModelCacheReady downgraded to False when workload absent, got %#v", cond)
	}
	if !strings.Contains(cond.Message, "not yet created") {
		t.Errorf("expected 'not yet created' reason, got %q", cond.Message)
	}
}

func TestVerifyCacheInjection_InjectedLabelPresent_StaysReady(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&concernTestProvider{name: "inject-test"})
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("inject-test")

	// concernTestProvider injects Labels{"model-label":"enabled"} for model weights.
	ss := stsWithPodLabels("ws", "default", map[string]string{"model-label": "enabled"})
	ss.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "MODEL_ENV", Value: "weights"}}

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected ModelCacheReady to stay True when label injected, got %#v", cond)
	}
}

func TestVerifyCacheInjection_ReusesRenderedModelIdentity(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &modelIdentityProvider{name: "model-identity-test"}
	Register(provider)
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment(provider.name)
	ss := stsWithPodLabels("ws", "default", map[string]string{
		"cache.test/model": "model-a-revision-a",
	})
	ss.Spec.Template.Annotations = map[string]string{
		cacheModelNameAnnotation:     "model-a",
		cacheModelRevisionAnnotation: "revision-a",
	}

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(),
		kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected ModelCacheReady to stay True with matching model identity, got %#v", cond)
	}
}

func TestVerifyCacheInjection_LabelMissing_Downgrades(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&concernTestProvider{name: "inject-test"})
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("inject-test")

	// Pod template is missing the provider's expected label.
	ss := stsWithPodLabels("ws", "default", map[string]string{"other": "x"})

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected downgrade to False when injected label missing, got %#v", cond)
	}
	if !strings.Contains(cond.Message, "expected cache label") {
		t.Errorf("expected missing-label reason, got %q", cond.Message)
	}
}

func TestVerifyCacheInjection_EmptyLabelMissing_Downgrades(t *testing.T) {
	isolateProviderRegistry(t)
	Register(&fixedMutationProvider{
		name: "empty-label-test",
		mutations: &PodMutations{
			Labels: map[string]string{"cache.example.com/inject": ""},
		},
	})

	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("empty-label-test")
	ss := stsWithPodLabels("ws", "default", map[string]string{"other": "x"})
	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(),
		kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	condition := meta.FindStatusCondition(status.Conditions,
		string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("expected missing empty-valued label to downgrade readiness, got %#v", condition)
	}
}

func TestVerifyCacheInjection_ProviderNotApplicable_Downgrades(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	// streamerScopedProvider only applies when the workload loads via runai_streamer.
	Register(&streamerScopedProvider{concernTestProvider{name: "scoped-test"}})
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("scoped-test")

	// No runai_streamer arg → provider not applicable → nothing injected.
	ss := stsWithPodLabels("ws", "default", nil, "--load-format", "auto")

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected downgrade to False when provider not applicable, got %#v", cond)
	}
	if !strings.Contains(cond.Message, "not injected") {
		t.Errorf("expected not-injected reason, got %q", cond.Message)
	}
}

func TestVerifyCacheInjection_EmptyMutations_Downgrades(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	// noopTestProvider is ready but injects nothing.
	Register(&noopTestProvider{})
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("noop-test")

	ss := stsWithPodLabels("ws", "default", map[string]string{"anything": "x"})

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected downgrade to False when provider injects nothing, got %#v", cond)
	}
}

func TestVerifyCacheInjection_KVEnvMissing_Downgrades(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	// concernTestProvider expects KV_ENV, but the rendered container is missing it.
	Register(&concernTestProvider{name: "kv-test"})
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "default", CreationTimestamp: metav1.Now()},
	}
	attachment := &Attachment{KVCache: &ConcernConfig{Provider: "kv-test", Mode: ModeOpportunistic}}

	ss := stsWithPodLabels("ws", "default", nil)

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeKVCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected KVCacheReady downgraded to False when KV env is missing, got %#v", cond)
	}
	if !strings.Contains(cond.Message, `expected cache env var "KV_ENV"`) {
		t.Errorf("expected missing-env reason, got %q", cond.Message)
	}
}

func TestVerifyCacheInjection_KVEnvPresent_StaysReady(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&concernTestProvider{name: "kv-test"})
	ws := modelCacheWorkspace()
	attachment := &Attachment{KVCache: &ConcernConfig{Provider: "kv-test", Mode: ModeOpportunistic}}
	ss := stsWithPodLabels("ws", "default", nil)
	ss.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "KV_ENV", Value: "user-override"}}

	status := &kaitov1beta1.WorkspaceStatus{}
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeKVCacheReady, true, "infra ready")

	verifyCacheInjection(context.Background(), ws, attachment, ss, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady))
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected KVCacheReady to stay True when KV env is present, got %#v", cond)
	}
}

func TestVerifyCacheInjection_VolumeAndMountPresence(t *testing.T) {
	tests := []struct {
		name        string
		volumes     []corev1.Volume
		mounts      []corev1.VolumeMount
		wantReady   bool
		wantMessage string
	}{
		{
			name:        "missing volume",
			mounts:      []corev1.VolumeMount{{Name: "cache-client", MountPath: "/cache"}},
			wantMessage: `expected cache volume "cache-client"`,
		},
		{
			name:        "missing mount",
			volumes:     []corev1.Volume{{Name: "cache-client"}},
			wantMessage: `expected cache volume mount "cache-client"`,
		},
		{
			name:      "volume and mount present",
			volumes:   []corev1.Volume{{Name: "cache-client"}},
			mounts:    []corev1.VolumeMount{{Name: "cache-client", MountPath: "/custom-cache"}},
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateProviderRegistry(t)
			Register(&fixedMutationProvider{
				name: "volume-test",
				mutations: &PodMutations{
					Volumes:      []corev1.Volume{{Name: "cache-client"}},
					VolumeMounts: []corev1.VolumeMount{{Name: "cache-client", MountPath: "/cache"}},
				},
			})

			ws := modelCacheWorkspace()
			attachment := modelCacheAttachment("volume-test")
			ss := stsWithPodLabels("ws", "default", nil)
			ss.Spec.Template.Spec.Volumes = tt.volumes
			ss.Spec.Template.Spec.Containers[0].VolumeMounts = tt.mounts

			status := &kaitov1beta1.WorkspaceStatus{}
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelCacheReady, true, "infra ready")

			verifyCacheInjection(context.Background(), ws, attachment, ss, status)

			cond := meta.FindStatusCondition(status.Conditions,
				string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
			if cond == nil {
				t.Fatal("expected ModelCacheReady condition")
			}
			if got := cond.Status == metav1.ConditionTrue; got != tt.wantReady {
				t.Fatalf("ready = %v, want %v; condition = %#v", got, tt.wantReady, cond)
			}
			if tt.wantMessage != "" && !strings.Contains(cond.Message, tt.wantMessage) {
				t.Errorf("expected reason containing %q, got %q", tt.wantMessage, cond.Message)
			}
		})
	}
}

func TestVerifyCacheInjection_AlreadyFalse_NotClobbered(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&concernTestProvider{name: "inject-test"})
	ws := modelCacheWorkspace()
	attachment := modelCacheAttachment("inject-test")

	status := &kaitov1beta1.WorkspaceStatus{}
	// Infra check already marked it False with a specific reason; verification
	// must not overwrite that (e.g. with a generic "not yet created").
	setCacheCondition(status, ws.GetGeneration(), kaitov1beta1.WorkspaceConditionTypeModelCacheReady, false, "cache infrastructure not installed")

	verifyCacheInjection(context.Background(), ws, attachment, nil, status)

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil || cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected condition to remain False, got %#v", cond)
	}
	if cond.Message != "cache infrastructure not installed" {
		t.Errorf("expected infra reason preserved, got %q", cond.Message)
	}
}
