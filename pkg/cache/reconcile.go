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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (
	// DefaultCacheReadyTimeout is the maximum time to block deployment in Required mode
	// waiting for cache to become ready. After this, deployment proceeds without cache.
	DefaultCacheReadyTimeout = 10 * time.Minute
)

// ReconcileResult contains the outcome of cache reconciliation.
type ReconcileResult struct {
	// BlockDeployment is true when a Required-mode cache is not yet ready.
	BlockDeployment bool
	// RequeueNeeded is true when the cache is not yet ready and should be rechecked.
	RequeueNeeded bool
}

// ReconcileCache checks cache provider infrastructure readiness and sets workspace conditions.
// Returns whether the workspace deployment should be blocked (Required mode + not ready).
// This should be called early in the reconcile loop, after nodes are ready.
func ReconcileCache(ctx context.Context, kubeClient client.Client, ws *kaitov1beta1.Workspace, status *kaitov1beta1.WorkspaceStatus) ReconcileResult {
	if !featuregates.FeatureGates[consts.FeatureFlagDistributedCache] {
		return ReconcileResult{}
	}
	if ws.Cache == nil {
		return ReconcileResult{}
	}

	result := ReconcileResult{}
	elapsed := time.Since(ws.CreationTimestamp.Time)

	// Check model weights cache readiness
	if ws.Cache.ModelCache != nil && ws.Cache.ModelCache.Mode != kaitov1beta1.CacheModeDisabled {
		cacheName := extractCacheName(ctx, kubeClient, ws.Namespace, ws.Cache.ModelCache.Config)
		ready, reason := checkProviderReady(ctx, ws.Cache.ModelCache.Provider, cacheName)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelCacheReady, ready, reason)

		if !ready && ws.Cache.ModelCache.Mode == kaitov1beta1.CacheModeRequired {
			if elapsed > DefaultCacheReadyTimeout {
				klog.V(2).InfoS("Cache ready timeout exceeded, proceeding without model cache",
					"workspace", ws.Name, "elapsed", elapsed, "timeout", DefaultCacheReadyTimeout)
				setCacheCondition(status, ws.GetGeneration(),
					kaitov1beta1.WorkspaceConditionTypeModelCacheReady, false,
					fmt.Sprintf("timeout exceeded (%s): proceeding without cache", DefaultCacheReadyTimeout))
			} else {
				result.BlockDeployment = true
				result.RequeueNeeded = true
			}
		}
	}

	// Check KV cache readiness
	if ws.Cache.KVCache != nil && ws.Cache.KVCache.Mode != kaitov1beta1.CacheModeDisabled {
		cacheName := extractCacheName(ctx, kubeClient, ws.Namespace, ws.Cache.KVCache.Config)
		ready, reason := checkProviderReady(ctx, ws.Cache.KVCache.Provider, cacheName)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeKVCacheReady, ready, reason)

		if !ready && ws.Cache.KVCache.Mode == kaitov1beta1.CacheModeRequired {
			if elapsed > DefaultCacheReadyTimeout {
				klog.V(2).InfoS("Cache ready timeout exceeded, proceeding without KV cache",
					"workspace", ws.Name, "elapsed", elapsed, "timeout", DefaultCacheReadyTimeout)
				setCacheCondition(status, ws.GetGeneration(),
					kaitov1beta1.WorkspaceConditionTypeKVCacheReady, false,
					fmt.Sprintf("timeout exceeded (%s): proceeding without cache", DefaultCacheReadyTimeout))
			} else {
				result.BlockDeployment = true
				result.RequeueNeeded = true
			}
		}
	}

	return result
}

// checkProviderReady resolves a provider and checks its readiness.
func checkProviderReady(ctx context.Context, providerName kaitov1beta1.CacheProvider, cacheName string) (bool, string) {
	p, err := Get(providerName)
	if err != nil {
		klog.V(2).InfoS("Cache provider not registered", "provider", providerName, "error", err)
		return false, fmt.Sprintf("provider %q not registered", providerName)
	}

	available, err := p.IsAvailable(ctx, cacheName)
	if err != nil {
		klog.V(2).InfoS("Cache provider availability check failed", "provider", providerName, "error", err)
		return false, fmt.Sprintf("availability check failed: %v", err)
	}
	if !available {
		return false, "cache infrastructure not installed"
	}

	ready, reason, err := p.IsReady(ctx, cacheName)
	if err != nil {
		klog.V(2).InfoS("Cache provider readiness check failed", "provider", providerName, "error", err)
		return false, fmt.Sprintf("readiness check failed: %v", err)
	}

	return ready, reason
}

// setCacheCondition sets a cache-related condition on the workspace status.
func setCacheCondition(status *kaitov1beta1.WorkspaceStatus, generation int64,
	condType kaitov1beta1.ConditionType, ready bool, reason string) {
	condStatus := metav1.ConditionFalse
	condReason := "CacheNotReady"
	if ready {
		condStatus = metav1.ConditionTrue
		condReason = "CacheReady"
	}

	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               string(condType),
		Status:             condStatus,
		Reason:             condReason,
		Message:            reason,
		ObservedGeneration: generation,
	})
}
