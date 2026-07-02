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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestReconcileCache_FeatureGateDisabled(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "noop",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
	if result.BlockDeployment {
		t.Error("should not block when feature gate is disabled")
	}
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

func TestReconcileCache_NoopProvider_Opportunistic(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	// Ensure noop is registered
	Register(&noopTestProvider{})

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "noop-test",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
	if result.BlockDeployment {
		t.Error("should not block in Opportunistic mode")
	}

	// Should have set condition
	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil {
		t.Fatal("expected ModelWeightsCacheReady condition to be set")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected condition True, got %s", cond.Status)
	}
}

func TestReconcileCache_UnregisteredProvider_Required(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "nonexistent",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
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
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "nonexistent",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
	if result.BlockDeployment {
		t.Error("should not block in Opportunistic mode even if provider missing")
	}
}

func TestReconcileCache_RequiredTimeoutProceeds(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	Register(&statefulTestProvider{name: "unavailable-test", available: false})

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.NewTime(time.Now().Add(-DefaultCacheReadyTimeout - time.Minute)),
		},
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "unavailable-test",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
	if result.BlockDeployment {
		t.Fatal("should not block after the cache ready timeout is exceeded")
	}
	if result.RequeueNeeded {
		t.Fatal("should not requeue after the cache ready timeout is exceeded")
	}

	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if cond == nil {
		t.Fatal("expected model cache ready condition to be set")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected condition False, got %s", cond.Status)
	}
	if !strings.Contains(cond.Message, "timeout exceeded") {
		t.Fatalf("expected timeout message, got %q", cond.Message)
	}
}

func TestReconcileCache_KVCacheRequiredUnregisteredProviderBlocks(t *testing.T) {
	isolateProviderRegistry(t)
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
		Cache: &kaitov1beta1.CacheSpec{
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "missing-kv-provider",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
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
		Cache: &kaitov1beta1.CacheSpec{
			ModelCache: &kaitov1beta1.ModelCacheSpec{
				Provider: "noop-test",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
			KVCache: &kaitov1beta1.KVCacheSpec{
				Provider: "missing-kv-provider",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), nil, ws, status)
	if !result.BlockDeployment {
		t.Fatal("should block when either required cache concern is not ready")
	}

	modelCond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
	if modelCond == nil || modelCond.Status != metav1.ConditionTrue {
		t.Fatalf("expected model cache to be ready, got %#v", modelCond)
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
func (p *noopTestProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _, _ string) (*PodMutations, error) {
	return &PodMutations{}, nil
}

func (p *noopTestProvider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}
