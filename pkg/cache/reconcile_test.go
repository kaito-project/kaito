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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestReconcileCache_FeatureGateDisabled(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{
		Cache: &kaitov1beta1.CacheSpec{
			ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
				Provider: "noop",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), ws, status)
	if result.BlockDeployment {
		t.Error("should not block when feature gate is disabled")
	}
}

func TestReconcileCache_NilCache(t *testing.T) {
	featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = true
	defer func() { featuregates.FeatureGates[consts.FeatureFlagDistributedCache] = false }()

	ws := &kaitov1beta1.Workspace{}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), ws, status)
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
		Cache: &kaitov1beta1.CacheSpec{
			ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
				Provider: "noop-test",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), ws, status)
	if result.BlockDeployment {
		t.Error("should not block in Opportunistic mode")
	}

	// Should have set condition
	cond := meta.FindStatusCondition(status.Conditions, string(kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady))
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
		Cache: &kaitov1beta1.CacheSpec{
			ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
				Provider: "nonexistent",
				Mode:     kaitov1beta1.CacheModeRequired,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), ws, status)
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
		Cache: &kaitov1beta1.CacheSpec{
			ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
				Provider: "nonexistent",
				Mode:     kaitov1beta1.CacheModeOpportunistic,
			},
		},
	}
	status := &kaitov1beta1.WorkspaceStatus{}

	result := ReconcileCache(context.Background(), ws, status)
	if result.BlockDeployment {
		t.Error("should not block in Opportunistic mode even if provider missing")
	}
}

// noopTestProvider is a simple test provider that's always ready.
type noopTestProvider struct{}

func (p *noopTestProvider) Name() string                                  { return "noop-test" }
func (p *noopTestProvider) IsAvailable(_ context.Context) (bool, error)   { return true, nil }
func (p *noopTestProvider) IsReady(_ context.Context) (bool, string, error) {
	return true, "always ready", nil
}
func (p *noopTestProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _ string) (*PodMutations, error) {
	return &PodMutations{}, nil
}
func (p *noopTestProvider) Prewarm(_ context.Context, _ PrewarmRequest) error  { return nil }
func (p *noopTestProvider) Cleanup(_ context.Context, _ PrewarmRequest) error { return nil }
