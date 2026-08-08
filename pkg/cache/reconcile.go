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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// ReconcileResult contains the outcome of cache reconciliation.
type ReconcileResult struct {
	// BlockDeployment is true when a Required-mode cache is not yet ready.
	BlockDeployment bool
	// RequeueNeeded is true when the cache is not yet ready and should be rechecked.
	RequeueNeeded bool
}

// ReconcileCache checks cache provider infrastructure readiness and sets workspace conditions.
// When a kubeClient is supplied it also verifies the cache was actually injected
// into the rendered inference StatefulSet, downgrading *CacheReady to False when
// it was not — so the condition means "infrastructure ready AND injected".
// Returns whether the workspace deployment should be blocked (Required mode + not ready).
// This should be called early in the reconcile loop, after nodes are ready.
func ReconcileCache(ctx context.Context, kubeClient client.Client, ws *kaitov1beta1.Workspace, status *kaitov1beta1.WorkspaceStatus) ReconcileResult {
	if ws.Cache == nil {
		return ReconcileResult{}
	}

	result := ReconcileResult{}

	// Check model weights cache readiness
	if ws.Cache.ModelCache != nil && ws.Cache.ModelCache.Mode != kaitov1beta1.CacheModeDisabled {
		cacheName := extractCacheName(ctx, kubeClient, ws.Namespace, ws.Cache.ModelCache.Config)
		ready, reason := checkProviderReady(ctx, ws.Cache.ModelCache.Provider, cacheName)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelCacheReady, ready, reason)

		// Required mode blocks deployment until the cache is ready — indefinitely.
		if !ready && ws.Cache.ModelCache.Mode == kaitov1beta1.CacheModeRequired {
			result.BlockDeployment = true
			result.RequeueNeeded = true
		}
	}

	// Check KV cache readiness
	if ws.Cache.KVCache != nil && ws.Cache.KVCache.Mode != kaitov1beta1.CacheModeDisabled {
		cacheName := extractCacheName(ctx, kubeClient, ws.Namespace, ws.Cache.KVCache.Config)
		ready, reason := checkProviderReady(ctx, ws.Cache.KVCache.Provider, cacheName)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeKVCacheReady, ready, reason)

		// Required mode blocks deployment until the cache is ready — indefinitely.
		if !ready && ws.Cache.KVCache.Mode == kaitov1beta1.CacheModeRequired {
			result.BlockDeployment = true
			result.RequeueNeeded = true
		}
	}

	// Verify the configured cache was actually injected into the rendered
	// StatefulSet, downgrading *CacheReady to False when it was not, so the
	// condition reflects the inference pod rather than just backend
	// infrastructure readiness. This needs the workload, so it is skipped when
	// no client is available (unit tests) and treats a missing StatefulSet as
	// not-yet-injected. It is also skipped while we are blocking deployment,
	// since the workload is not being created and the conditions already carry
	// the more informative infrastructure-not-ready reason.
	if kubeClient != nil && !result.BlockDeployment {
		var ss *appsv1.StatefulSet
		fetched := &appsv1.StatefulSet{}
		switch err := kubeClient.Get(ctx, client.ObjectKey{Name: ws.Name, Namespace: ws.Namespace}, fetched); {
		case err == nil:
			ss = fetched
		case apierrors.IsNotFound(err):
			ss = nil
		default:
			klog.V(2).InfoS("Failed to fetch StatefulSet for cache injection verification",
				"workspace", ws.Name, "error", err)
			return result
		}
		verifyCacheInjection(ctx, kubeClient, ws, ss, status)
	}

	return result
}

// verifyCacheInjection re-evaluates cache conditions against the *rendered*
// StatefulSet, downgrading ModelCacheReady/KVCacheReady to False when a
// configured concern produced no mutations on the actual workload — i.e. the
// provider was not applicable, its infrastructure was unavailable, or nothing
// was injected. This makes the condition reflect the inference pod rather than
// just backend infrastructure readiness, closing the "green but no-op" gap where
// a Ready cache CR let the condition report success even though the pod never
// consumed the cache.
//
// It only ever downgrades a condition that the infrastructure-ready checks above
// already set to True; an already-False condition is left untouched so its
// (more specific) infrastructure/timeout reason is preserved. The effective rule
// becomes Ready == infrastructure ready AND mutations injected. ss may be nil
// when the workload does not yet exist, in which case the concern is reported
// not-yet injected.
func verifyCacheInjection(ctx context.Context, kubeClient client.Client,
	ws *kaitov1beta1.Workspace, ss *appsv1.StatefulSet, status *kaitov1beta1.WorkspaceStatus) {

	if ws.Cache == nil {
		return
	}

	if ws.Cache.ModelCache != nil && ws.Cache.ModelCache.Mode != kaitov1beta1.CacheModeDisabled &&
		conditionIsTrue(status, kaitov1beta1.WorkspaceConditionTypeModelCacheReady) {
		if injected, reason := concernInjected(ctx, kubeClient, ws, CacheConcernModelWeights, ss); !injected {
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelCacheReady, false, reason)
		}
	}

	if ws.Cache.KVCache != nil && ws.Cache.KVCache.Mode != kaitov1beta1.CacheModeDisabled &&
		conditionIsTrue(status, kaitov1beta1.WorkspaceConditionTypeKVCacheReady) {
		if injected, reason := concernInjected(ctx, kubeClient, ws, CacheConcernKVCache, ss); !injected {
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeKVCacheReady, false, reason)
		}
	}
}

// conditionIsTrue reports whether the named condition is currently set to True.
func conditionIsTrue(status *kaitov1beta1.WorkspaceStatus, condType kaitov1beta1.ConditionType) bool {
	c := meta.FindStatusCondition(status.Conditions, string(condType))
	return c != nil && c.Status == metav1.ConditionTrue
}

// concernInjected reports whether the provider's mutations for a concern are
// actually present on the rendered pod template. It recomputes the concern's
// mutations via the same render-time path (collectMutations) so the two cannot
// disagree, and confirms any provider-declared labels landed on the workload.
func concernInjected(ctx context.Context, kubeClient client.Client,
	ws *kaitov1beta1.Workspace, concern CacheConcern, ss *appsv1.StatefulSet) (bool, string) {

	if ss == nil {
		return false, "inference workload not yet created; cache not injected"
	}

	// Evaluate this concern in isolation so we can attribute the result to the
	// correct condition, reusing the exact render-time collectMutations path.
	m, err := collectMutations(ctx, kubeClient, singleConcernWorkspace(ws, concern), "", "", ss)
	if err != nil {
		return false, fmt.Sprintf("cache not injected: %v", err)
	}
	if m == nil || (len(m.Labels) == 0 && len(m.EnvVars) == 0 && len(m.Volumes) == 0 &&
		len(m.VolumeMounts) == 0 && len(m.InitContainers) == 0) {
		return false, "cache configured but not injected into inference pod " +
			"(provider not applicable to this workload or infrastructure unavailable)"
	}

	for k, v := range m.Labels {
		if ss.Spec.Template.Labels[k] != v {
			return false, fmt.Sprintf("expected cache label %q not present on inference pod", k)
		}
	}

	return true, ""
}

// singleConcernWorkspace returns a shallow copy of ws whose Cache carries only
// the given concern, so collectMutations evaluates that concern in isolation.
func singleConcernWorkspace(ws *kaitov1beta1.Workspace, concern CacheConcern) *kaitov1beta1.Workspace {
	cache := *ws.Cache
	switch concern {
	case CacheConcernModelWeights:
		cache.KVCache = nil
	case CacheConcernKVCache:
		cache.ModelCache = nil
	}
	out := *ws
	out.Cache = &cache
	return &out
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
