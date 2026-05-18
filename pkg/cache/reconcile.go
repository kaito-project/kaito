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

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
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
func ReconcileCache(ctx context.Context, ws *kaitov1beta1.Workspace, status *kaitov1beta1.WorkspaceStatus) ReconcileResult {
	if !featuregates.FeatureGates[consts.FeatureFlagDistributedCache] {
		return ReconcileResult{}
	}
	if ws.Cache == nil {
		return ReconcileResult{}
	}

	result := ReconcileResult{}

	// Check model weights cache readiness
	if ws.Cache.ModelWeights != nil && ws.Cache.ModelWeights.Mode != kaitov1beta1.CacheModeDisabled {
		ready, reason := checkProviderReady(ctx, ws.Cache.ModelWeights.Provider)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, ready, reason)

		if !ready && ws.Cache.ModelWeights.Mode == kaitov1beta1.CacheModeRequired {
			result.BlockDeployment = true
			result.RequeueNeeded = true
		}
	}

	// Check KV cache readiness
	if ws.Cache.KVCache != nil && ws.Cache.KVCache.Mode != kaitov1beta1.CacheModeDisabled {
		ready, reason := checkProviderReady(ctx, ws.Cache.KVCache.Provider)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeKVCacheReady, ready, reason)

		if !ready && ws.Cache.KVCache.Mode == kaitov1beta1.CacheModeRequired {
			result.BlockDeployment = true
			result.RequeueNeeded = true
		}
	}

	return result
}

// PrewarmResult contains the outcome of prewarm reconciliation.
type PrewarmResult struct {
	// Phase is the current prewarm status.
	Phase PrewarmPhase
	// RequeueNeeded is true when prewarm is in progress and should be rechecked.
	RequeueNeeded bool
}

// ReconcilePrewarm checks and triggers model prewarm for cache-enabled workspaces.
// This should be called after model resolution in the reconcile loop, since it
// needs the model name/revision to build the prewarm request.
// The kubeClient is used to create/query prewarm Jobs.
func ReconcilePrewarm(ctx context.Context, ws *kaitov1beta1.Workspace, kubeClient client.Client,
	modelName, modelRevision, modelAccessSecret string,
	status *kaitov1beta1.WorkspaceStatus) PrewarmResult {

	if !featuregates.FeatureGates[consts.FeatureFlagDistributedCache] {
		return PrewarmResult{Phase: PrewarmPhaseSucceeded}
	}
	if ws.Cache == nil || ws.Cache.ModelWeights == nil {
		return PrewarmResult{Phase: PrewarmPhaseSucceeded}
	}
	if ws.Cache.ModelWeights.Mode == kaitov1beta1.CacheModeDisabled {
		return PrewarmResult{Phase: PrewarmPhaseSucceeded}
	}
	if !ws.Cache.ModelWeights.PrewarmOnDeploy {
		return PrewarmResult{Phase: PrewarmPhaseSucceeded}
	}

	providerName := ws.Cache.ModelWeights.Provider
	p, err := Get(providerName)
	if err != nil {
		klog.V(2).InfoS("Cache provider not registered for prewarm", "provider", providerName, "error", err)
		return PrewarmResult{Phase: PrewarmPhaseFailed}
	}

	// Check if a prewarm Job already exists for this model.
	phase, msg := checkPrewarmJobStatus(ctx, kubeClient, ws.Namespace, ws.Name, modelName)

	switch phase {
	case PrewarmPhaseSucceeded:
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, true, "prewarm completed")
		return PrewarmResult{Phase: PrewarmPhaseSucceeded}

	case PrewarmPhaseRunning, PrewarmPhasePending:
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, false, msg)
		return PrewarmResult{Phase: phase, RequeueNeeded: true}

	case PrewarmPhaseFailed:
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, false, msg)
		// Could retry here in future; for now, report failure.
		return PrewarmResult{Phase: PrewarmPhaseFailed}

	default:
		// No Job exists yet — create prewarm Job.
		req := PrewarmRequest{
			ModelName:         modelName,
			ModelRevision:     modelRevision,
			ModelAccessSecret: modelAccessSecret,
		}

		// Validate provider config first.
		if err := p.Prewarm(ctx, req); err != nil {
			klog.ErrorS(err, "Failed to validate prewarm config", "model", modelName, "provider", providerName)
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, false,
				fmt.Sprintf("prewarm config invalid: %v", err))
			return PrewarmResult{Phase: PrewarmPhaseFailed}
		}

		// Build and create the prewarm Job.
		builder, ok := p.(PrewarmJobBuilder)
		if !ok {
			klog.V(2).InfoS("Provider does not support Job-based prewarm", "provider", providerName)
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, false,
				"provider does not support prewarm Jobs")
			return PrewarmResult{Phase: PrewarmPhaseFailed}
		}

		job := builder.BuildPrewarmJob(req, ws.Namespace)
		SetPrewarmOwnerReference(job, ws)

		if err := kubeClient.Create(ctx, job); err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Race condition: Job was created between our check and create.
				// Requeue to pick up its status on next reconcile.
				return PrewarmResult{Phase: PrewarmPhasePending, RequeueNeeded: true}
			}
			klog.ErrorS(err, "Failed to create prewarm Job", "model", modelName, "job", job.Name)
			setCacheCondition(status, ws.GetGeneration(),
				kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, false,
				fmt.Sprintf("failed to create prewarm Job: %v", err))
			return PrewarmResult{Phase: PrewarmPhaseFailed}
		}

		klog.InfoS("Created prewarm Job", "job", job.Name, "model", modelName, "namespace", ws.Namespace)
		setCacheCondition(status, ws.GetGeneration(),
			kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady, false, "prewarm Job created")
		return PrewarmResult{Phase: PrewarmPhasePending, RequeueNeeded: true}
	}
}

// checkPrewarmJobStatus looks for a prewarm Job matching the workspace and model,
// and returns its current phase.
func checkPrewarmJobStatus(ctx context.Context, kubeClient client.Client, namespace, workspaceName, modelName string) (PrewarmPhase, string) {
	jobList := &batchv1.JobList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		"kaito.sh/cache-prewarm": "true",
		"kaito.sh/workspace":    workspaceName,
	})

	if err := kubeClient.List(ctx, jobList, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		if apierrors.IsNotFound(err) {
			return PrewarmPhaseUnknown, "no prewarm Job found"
		}
		klog.V(2).InfoS("Error listing prewarm Jobs", "error", err)
		return PrewarmPhaseUnknown, fmt.Sprintf("error checking prewarm status: %v", err)
	}

	if len(jobList.Items) == 0 {
		return PrewarmPhaseUnknown, "no prewarm Job found"
	}

	// Use the most recent Job (by creation timestamp).
	job := findLatestJob(jobList.Items)
	return jobPhase(job), jobStatusMessage(job)
}

// jobPhase determines the phase of a Job from its conditions.
func jobPhase(job *batchv1.Job) PrewarmPhase {
	for _, cond := range job.Status.Conditions {
		switch cond.Type {
		case batchv1.JobComplete:
			if cond.Status == "True" {
				return PrewarmPhaseSucceeded
			}
		case batchv1.JobFailed:
			if cond.Status == "True" {
				return PrewarmPhaseFailed
			}
		}
	}

	if job.Status.Active > 0 {
		return PrewarmPhaseRunning
	}

	return PrewarmPhasePending
}

// jobStatusMessage builds a human-readable message from Job status.
func jobStatusMessage(job *batchv1.Job) string {
	for _, cond := range job.Status.Conditions {
		if cond.Status == "True" {
			return fmt.Sprintf("Job %s: %s - %s", job.Name, cond.Type, cond.Message)
		}
	}
	if job.Status.Active > 0 {
		return fmt.Sprintf("Job %s: running (%d active)", job.Name, job.Status.Active)
	}
	return fmt.Sprintf("Job %s: pending", job.Name)
}

// findLatestJob returns the most recently created Job from a list.
func findLatestJob(jobs []batchv1.Job) *batchv1.Job {
	latest := &jobs[0]
	for i := range jobs {
		if jobs[i].CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = &jobs[i]
		}
	}
	return latest
}

// SetPrewarmOwnerReference sets owner reference on a prewarm Job so it is
// cleaned up when the workspace is deleted.
func SetPrewarmOwnerReference(job *batchv1.Job, ws *kaitov1beta1.Workspace) {
	job.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "kaito.sh/v1beta1",
			Kind:       "Workspace",
			Name:       ws.Name,
			UID:        ws.UID,
		},
	}
	// Add workspace label for lookup.
	if job.Labels == nil {
		job.Labels = make(map[string]string)
	}
	job.Labels["kaito.sh/workspace"] = ws.Name
}

// GetPrewarmJob retrieves a prewarm Job by workspace name.
func GetPrewarmJob(ctx context.Context, kubeClient client.Client, namespace, workspaceName string) (*batchv1.Job, error) {
	job := &batchv1.Job{}
	// Try by deterministic name first.
	err := kubeClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      "cache-prewarm-" + workspaceName,
	}, job)
	if err == nil {
		return job, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	return nil, nil
}

// checkProviderReady resolves a provider and checks its readiness.
func checkProviderReady(ctx context.Context, providerName kaitov1beta1.CacheProvider) (bool, string) {
	p, err := Get(providerName)
	if err != nil {
		klog.V(2).InfoS("Cache provider not registered", "provider", providerName, "error", err)
		return false, fmt.Sprintf("provider %q not registered", providerName)
	}

	available, err := p.IsAvailable(ctx)
	if err != nil {
		klog.V(2).InfoS("Cache provider availability check failed", "provider", providerName, "error", err)
		return false, fmt.Sprintf("availability check failed: %v", err)
	}
	if !available {
		return false, "cache infrastructure not installed"
	}

	ready, reason, err := p.IsReady(ctx)
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
