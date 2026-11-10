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
	// RequeueNeeded is true while an active attachment should be periodically rechecked.
	RequeueNeeded bool
}

var managedConditionTypes = []kaitov1beta1.ConditionType{
	kaitov1beta1.WorkspaceConditionTypeModelCacheReady,
	kaitov1beta1.WorkspaceConditionTypeKVCacheReady,
}

// ReconcileCache checks cache provider infrastructure readiness and sets workspace conditions.
// When a kubeClient is supplied it also verifies the cache was actually injected
// into the rendered inference StatefulSet, downgrading *CacheReady to False when
// it was not — so the condition means "infrastructure ready AND injected".
// Returns whether the workspace deployment should be blocked (Required mode + not ready).
// This should be called early in the reconcile loop, after nodes are ready.
func ReconcileCache(ctx context.Context, kubeClient client.Client, ws *kaitov1beta1.Workspace, status *kaitov1beta1.WorkspaceStatus) ReconcileResult {
	// Compute cache conditions separately and merge only their final states into
	// the Workspace status, avoiding False -> True -> False transition churn.
	cacheStatus := &kaitov1beta1.WorkspaceStatus{}
	defer func() {
		returnedTypes := make(map[string]struct{}, len(cacheStatus.Conditions))
		for _, condition := range cacheStatus.Conditions {
			meta.SetStatusCondition(&status.Conditions, condition)
			returnedTypes[condition.Type] = struct{}{}
		}
		for _, conditionType := range managedConditionTypes {
			if _, returned := returnedTypes[string(conditionType)]; !returned {
				meta.RemoveStatusCondition(&status.Conditions, string(conditionType))
			}
		}
	}()

	if !HasAttachment(ws) {
		return ReconcileResult{}
	}

	attachment, err := ResolveEffectiveAttachment(ctx, kubeClient, ws)
	if err != nil {
		klog.ErrorS(err, "Failed to resolve cache attachment", "workspace", klog.KObj(ws))
		return ReconcileResult{BlockDeployment: true, RequeueNeeded: true}
	}
	if attachment == nil {
		return ReconcileResult{}
	}

	var ss *appsv1.StatefulSet
	var workloadReadErr error
	if kubeClient != nil {
		fetched := &appsv1.StatefulSet{}
		switch err := kubeClient.Get(ctx, client.ObjectKey{Name: ws.Name, Namespace: ws.Namespace}, fetched); {
		case err == nil:
			ss = fetched
		case apierrors.IsNotFound(err):
		default:
			workloadReadErr = err
		}
	}

	// Active attachments are periodically reconciled so provider readiness loss
	// and recovery update both workload mutations and Workspace conditions.
	result := ReconcileResult{RequeueNeeded: attachmentHasActiveConcern(attachment)}

	// Check model weights cache readiness
	if config := attachment.ModelWeights; config != nil && config.Mode != ModeDisabled {
		applicable, known, deferGate := providerApplicability(config, CacheConcernModelWeights, ws, ss)
		if !known || applicable {
			ready, reason := checkProviderReady(ctx, CacheConcernModelWeights, config)
			setCacheCondition(cacheStatus, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelCacheReady, ready, reason)

			if !ready && config.Mode == ModeRequired && !deferGate {
				result.BlockDeployment = true
			}
		}
	}

	// Check KV cache readiness
	if config := attachment.KVCache; config != nil && config.Mode != ModeDisabled {
		applicable, known, deferGate := providerApplicability(config, CacheConcernKVCache, ws, ss)
		if !known || applicable {
			ready, reason := checkProviderReady(ctx, CacheConcernKVCache, config)
			setCacheCondition(cacheStatus, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeKVCacheReady, ready, reason)

			if !ready && config.Mode == ModeRequired && !deferGate {
				result.BlockDeployment = true
			}
		}
	}

	// Verify the configured cache was actually injected into the rendered
	// StatefulSet, downgrading *CacheReady to False when it was not, so the
	// condition reflects the inference pod rather than just backend
	// infrastructure readiness. This needs the workload, so it is skipped when
	// no client is available (unit tests) and treats a missing StatefulSet as
	// not-yet-injected. Infrastructure failures remain False and retain their
	// more specific reasons.
	if kubeClient != nil {
		if workloadReadErr != nil {
			klog.V(2).InfoS("Failed to fetch StatefulSet for cache injection verification",
				"workspace", ws.Name, "error", workloadReadErr)
			markCacheVerificationFailed(cacheStatus, ws.GetGeneration(),
				fmt.Sprintf("failed to verify cache injection: %v", workloadReadErr))
			return result
		}
		verifyCacheInjection(ctx, ws, attachment, ss, cacheStatus)
	}

	return result
}

func attachmentHasActiveConcern(attachment *Attachment) bool {
	return attachment != nil &&
		((attachment.ModelWeights != nil && attachment.ModelWeights.Mode != ModeDisabled) ||
			(attachment.KVCache != nil && attachment.KVCache.Mode != ModeDisabled))
}

// providerApplicability reports whether applicability is known for the current
// workload. Readiness gating is always deferred for applicability-aware
// providers because an existing workload may differ from the newly rendered
// desired workload. Mutation collection evaluates the desired workload before
// checking provider availability.
func providerApplicability(config *ConcernConfig, concern CacheConcern, ws *kaitov1beta1.Workspace,
	ss *appsv1.StatefulSet) (applicable, known, deferGate bool) {
	p, err := Get(config.Provider)
	if err != nil {
		return true, true, false
	}
	if _, ok := p.(PodApplicabilityChecker); !ok {
		return true, true, false
	}
	if ss == nil {
		return false, false, true
	}
	return providerAppliesTo(p, concern, ws, ss), true, true
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
func verifyCacheInjection(ctx context.Context, ws *kaitov1beta1.Workspace,
	attachment *Attachment, ss *appsv1.StatefulSet, status *kaitov1beta1.WorkspaceStatus) {

	if attachment == nil {
		return
	}

	if attachment.ModelWeights != nil && attachment.ModelWeights.Mode != ModeDisabled &&
		conditionIsTrue(status, kaitov1beta1.WorkspaceConditionTypeModelCacheReady) {
		if injected, reason := concernInjected(ctx, ws, attachment, CacheConcernModelWeights, ss); !injected {
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelCacheReady, false, reason)
		}
	}

	if attachment.KVCache != nil && attachment.KVCache.Mode != ModeDisabled &&
		conditionIsTrue(status, kaitov1beta1.WorkspaceConditionTypeKVCacheReady) {
		if injected, reason := concernInjected(ctx, ws, attachment, CacheConcernKVCache, ss); !injected {
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

// markCacheVerificationFailed downgrades only infrastructure-ready concerns.
// Conditions already False retain their more specific provider failure reason.
func markCacheVerificationFailed(status *kaitov1beta1.WorkspaceStatus, generation int64, message string) {
	for _, conditionType := range managedConditionTypes {
		if conditionIsTrue(status, conditionType) {
			setCacheCondition(status, generation, conditionType, false, message)
		}
	}
}

// concernInjected reports whether the provider's mutations for a concern are
// actually present on the rendered pod template. It reuses the model identity
// recorded during rendering and confirms the expected labels, env vars, volumes,
// and volume mounts landed on the workload.
func concernInjected(ctx context.Context, ws *kaitov1beta1.Workspace,
	attachment *Attachment, concern CacheConcern, ss *appsv1.StatefulSet) (bool, string) {

	if ss == nil {
		return false, "inference workload not yet created; cache not injected"
	}

	// Workloads created before these annotations were introduced fall back to
	// empty values, preserving the previous behavior until their next rollout.
	modelName := ss.Spec.Template.Annotations[cacheModelNameAnnotation]
	modelRevision := ss.Spec.Template.Annotations[cacheModelRevisionAnnotation]

	m, err := collectMutations(ctx, ws, singleConcernAttachment(attachment, concern),
		modelName, modelRevision, ss)
	if err != nil {
		return false, fmt.Sprintf("cache not injected: %v", err)
	}
	if m == nil || (len(m.Labels) == 0 && len(m.EnvVars) == 0 && len(m.Volumes) == 0 &&
		len(m.VolumeMounts) == 0) {
		return false, "cache configured but not injected into inference pod " +
			"(provider not applicable to this workload or infrastructure unavailable)"
	}

	for k, v := range m.Labels {
		actual, present := ss.Spec.Template.Labels[k]
		if !present || actual != v {
			return false, fmt.Sprintf("expected cache label %q not present on inference pod", k)
		}
	}

	volumeNames := make(map[string]struct{}, len(ss.Spec.Template.Spec.Volumes))
	for _, volume := range ss.Spec.Template.Spec.Volumes {
		volumeNames[volume.Name] = struct{}{}
	}
	for _, expected := range m.Volumes {
		if _, ok := volumeNames[expected.Name]; !ok {
			return false, fmt.Sprintf("expected cache volume %q not present on inference pod", expected.Name)
		}
	}

	if len(m.EnvVars) > 0 || len(m.VolumeMounts) > 0 {
		targetFound := false
		for _, container := range ss.Spec.Template.Spec.Containers {
			if !isCacheContainer(container.Image) {
				continue
			}
			targetFound = true

			envNames := make(map[string]struct{}, len(container.Env))
			for _, env := range container.Env {
				envNames[env.Name] = struct{}{}
			}
			for _, expected := range m.EnvVars {
				if _, ok := envNames[expected.Name]; !ok {
					return false, fmt.Sprintf(
						"expected cache env var %q not present on inference container %q",
						expected.Name, container.Name)
				}
			}

			mountsByPath := make(map[string]string, len(container.VolumeMounts))
			for _, mount := range container.VolumeMounts {
				mountsByPath[mount.MountPath] = mount.Name
			}
			for _, expected := range m.VolumeMounts {
				if actualName, ok := mountsByPath[expected.MountPath]; !ok || actualName != expected.Name {
					return false, fmt.Sprintf(
						"expected cache volume mount %q at %q not present on inference container %q",
						expected.Name, expected.MountPath, container.Name)
				}
			}
		}

		if !targetFound {
			return false, "no cache-target inference container found"
		}
	}

	return true, ""
}

// singleConcernAttachment returns a shallow copy carrying only the requested concern.
func singleConcernAttachment(attachment *Attachment, concern CacheConcern) *Attachment {
	out := *attachment
	switch concern {
	case CacheConcernModelWeights:
		out.KVCache = nil
	case CacheConcernKVCache:
		out.ModelWeights = nil
	}
	return &out
}

// checkProviderReady resolves a provider, validates attachment parameters, and checks readiness.
func checkProviderReady(ctx context.Context, concern CacheConcern, config *ConcernConfig) (bool, string) {
	p, err := Get(config.Provider)
	if err != nil {
		klog.V(2).InfoS("Cache provider not registered", "provider", config.Provider, "error", err)
		return false, fmt.Sprintf("provider %q not registered", config.Provider)
	}
	if err := validateParameters(p, concern, config.Parameters); err != nil {
		return false, err.Error()
	}

	cacheName := config.Parameters["cacheName"]
	available, err := p.IsAvailable(ctx, cacheName)
	if err != nil {
		klog.V(2).InfoS("Cache provider availability check failed", "provider", config.Provider, "error", err)
		return false, fmt.Sprintf("availability check failed: %v", err)
	}
	if !available {
		return false, "cache infrastructure not installed"
	}

	ready, reason, err := p.IsReady(ctx, cacheName)
	if err != nil {
		klog.V(2).InfoS("Cache provider readiness check failed", "provider", config.Provider, "error", err)
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
