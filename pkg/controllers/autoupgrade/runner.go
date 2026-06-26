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

package autoupgrade

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/robfig/cron/v3"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	inferencesetutil "github.com/kaito-project/kaito/pkg/utils/inferenceset"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
)

const (
	// DefaultInterval is the default polling interval for the AutoUpgradeRunner.
	DefaultInterval = 5 * time.Minute

	// DefaultMaintenanceWindowDuration is the default duration for a maintenance window
	// if none is specified.
	DefaultMaintenanceWindowDuration = 4 * time.Hour

	// AnnotationUpgradeStartTime records when the upgrade was initiated for a workspace.
	AnnotationUpgradeStartTime = "kaito.sh/upgrade-start-time"
)

// AutoUpgradeRunner is a background goroutine that detects base image version drift
// and sequentially upgrades Workspaces managed by InferenceSets with autoUpgrade enabled.
type AutoUpgradeRunner struct {
	Client   client.Client
	Interval time.Duration
}

// Start implements manager.Runnable. It polls every Interval and reconciles all
// InferenceSets with autoUpgrade enabled.
func (r *AutoUpgradeRunner) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.reconcileAll(ctx)
		}
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
func (r *AutoUpgradeRunner) NeedLeaderElection() bool { return true }

// reconcileAll lists all InferenceSets with autoUpgrade enabled and processes each.
func (r *AutoUpgradeRunner) reconcileAll(ctx context.Context) {
	klog.InfoS("AutoUpgradeRunner: reconcileAll tick", "desiredImage", inference.GetBaseImageName(), "desiredTag", inference.GetBaseImageTag())
	inferenceSetList := &kaitov1beta1.InferenceSetList{}
	if err := r.Client.List(ctx, inferenceSetList); err != nil {
		klog.ErrorS(err, "AutoUpgradeRunner: failed to list InferenceSets")
		return
	}

	for i := range inferenceSetList.Items {
		inferenceSetObj := &inferenceSetList.Items[i]
		if inferenceSetObj.DeletionTimestamp != nil {
			continue
		}
		r.reconcileInferenceSet(ctx, inferenceSetObj)
	}
}

// reconcileInferenceSet handles a single InferenceSet's auto-upgrade lifecycle.
func (r *AutoUpgradeRunner) reconcileInferenceSet(ctx context.Context, inferenceSetObj *kaitov1beta1.InferenceSet) {
	enabled := inferenceSetObj.Spec.AutoUpgrade != nil && inferenceSetObj.Spec.AutoUpgrade.Enabled
	if !enabled {
		klog.V(4).InfoS("AutoUpgradeRunner: auto-upgrade disabled, skipping", "inferenceset", klog.KObj(inferenceSetObj))
		return
	}

	// Get the current controller-embedded base image tag.
	desiredImage := inference.GetBaseImageName()
	desiredTag := inference.GetBaseImageTag()

	// List Workspaces belonging to this InferenceSet.
	wsList, err := inferencesetutil.ListWorkspaces(ctx, inferenceSetObj, r.Client)
	if err != nil {
		klog.ErrorS(err, "AutoUpgradeRunner: failed to list workspaces", "inferenceset", klog.KObj(inferenceSetObj))
		return
	}
	sort.SliceStable(wsList.Items, func(i, j int) bool {
		return wsList.Items[i].UID < wsList.Items[j].UID
	})

	// Categorize workspaces into drifted (needs upgrade) and upgrading (in progress).
	toUpgrade, upgrading, err := r.categorizeWorkspaces(ctx, wsList.Items, desiredImage, desiredTag)
	if err != nil {
		klog.ErrorS(err, "AutoUpgradeRunner: failed to categorize workspaces", "inferenceset", klog.KObj(inferenceSetObj))
		return
	}
	klog.InfoS("AutoUpgradeRunner: categorized workspaces", "inferenceset", klog.KObj(inferenceSetObj),
		"toUpgrade", len(toUpgrade), "upgrading", len(upgrading))

	// Update status if drift count changed.
	newDriftCount := len(toUpgrade) + len(upgrading)
	if r.statusNeedsUpdate(inferenceSetObj, newDriftCount) {
		// Mark success when drift transitions from >0 to 0 (upgrade just completed).
		markSuccess := newDriftCount == 0 && r.previousDriftCount(inferenceSetObj) > 0
		if err := r.updateStatus(ctx, inferenceSetObj, newDriftCount, markSuccess); err != nil {
			return
		}
	}

	// Return if all workspaces are up-to-date.
	if len(toUpgrade) == 0 && len(upgrading) == 0 {
		return
	}

	// If any workspace is still being upgraded, wait for it to succeed before moving to the next.
	if len(upgrading) > 0 {
		return
	}

	if len(toUpgrade) == 0 {
		klog.V(4).InfoS("AutoUpgradeRunner: no drifted workspaces, skipping", "inferenceset", klog.KObj(inferenceSetObj))
		return
	}

	// Maintenance window check.
	if !r.isWithinMaintenanceWindow(inferenceSetObj) {
		klog.V(4).InfoS("AutoUpgradeRunner: outside maintenance window, skipping", "inferenceset", klog.KObj(inferenceSetObj))
		return
	}

	// Tag the next drifted workspace for upgrade.
	r.tagWorkspaceForUpgrade(ctx, inferenceSetObj, &toUpgrade[0], desiredTag)
}

// categorizeWorkspaces classifies workspaces into two groups:
//   - toUpgrade: StatefulSet image differs from desired and no upgrade label set for current version.
//   - upgrading: has the upgrade label for the current desired version but not yet fully ready.
//
// Workspaces that are running the desired image AND are inference ready
// are considered complete and excluded from both lists.
func (r *AutoUpgradeRunner) categorizeWorkspaces(ctx context.Context, workspaces []kaitov1beta1.Workspace, desiredImage, desiredTag string) (toUpgrade, upgrading []kaitov1beta1.Workspace, err error) {
	for i := range workspaces {
		ws := &workspaces[i]
		if ws.DeletionTimestamp != nil {
			continue
		}

		ss := &appsv1.StatefulSet{}
		if err := resources.GetResource(ctx, ws.Name, ws.Namespace, r.Client, ss); err != nil {
			return nil, nil, fmt.Errorf("failed to get StatefulSet for workspace %s: %w", ws.Name, err)
		}

		if isWorkspaceInDesiredState(ss, desiredImage) {
			continue
		} else if ws.Labels[kaitov1alpha1.LabelUpgradeToVersion] == desiredTag {
			upgrading = append(upgrading, workspaces[i])
		} else {
			toUpgrade = append(toUpgrade, workspaces[i])
		}
	}
	return
}

// previousDriftCount returns the last recorded NumDriftedWorkspaces, or 0 if unset.
func (r *AutoUpgradeRunner) previousDriftCount(isObj *kaitov1beta1.InferenceSet) int {
	if isObj.Status.AutoUpgrade == nil || isObj.Status.AutoUpgrade.NumDriftedWorkspaces == nil {
		return 0
	}
	return *isObj.Status.AutoUpgrade.NumDriftedWorkspaces
}

// statusNeedsUpdate returns true if the InferenceSet's current NumDriftedWorkspaces
// differs from the new drift count.
func (r *AutoUpgradeRunner) statusNeedsUpdate(isObj *kaitov1beta1.InferenceSet, newDriftCount int) bool {
	if isObj.Status.AutoUpgrade == nil || isObj.Status.AutoUpgrade.NumDriftedWorkspaces == nil {
		return true
	}
	return *isObj.Status.AutoUpgrade.NumDriftedWorkspaces != newDriftCount
}

// updateStatus updates the InferenceSet status with drift count and optionally records upgrade success.
func (r *AutoUpgradeRunner) updateStatus(ctx context.Context, isObj *kaitov1beta1.InferenceSet, driftCount int, markSuccess bool) error {
	key := &client.ObjectKey{Name: isObj.Name, Namespace: isObj.Namespace}
	now := metav1.Now()
	err := inferencesetutil.UpdateInferenceSetStatus(ctx, r.Client, key, func(status *kaitov1beta1.InferenceSetStatus) error {
		if status.AutoUpgrade == nil {
			status.AutoUpgrade = &kaitov1beta1.AutoUpgradeStatus{}
		}
		status.AutoUpgrade.NumDriftedWorkspaces = &driftCount
		if markSuccess {
			status.AutoUpgrade.LastSuccessfulUpgradeTime = &now
		}
		return nil
	})
	if err != nil {
		klog.ErrorS(err, "AutoUpgradeRunner: failed to update status", "inferenceset", klog.KObj(isObj))
	}
	return err
}

// isWithinMaintenanceWindow checks if the current time is within the configured maintenance window.
// Returns true if no window is configured (upgrades any time).
func (r *AutoUpgradeRunner) isWithinMaintenanceWindow(inferenceSetObj *kaitov1beta1.InferenceSet) bool {
	if inferenceSetObj.Spec.AutoUpgrade == nil || inferenceSetObj.Spec.AutoUpgrade.MaintenanceWindow == nil {
		return true
	}

	window := inferenceSetObj.Spec.AutoUpgrade.MaintenanceWindow
	schedule, err := cron.ParseStandard(window.Schedule)
	if err != nil {
		klog.ErrorS(err, "AutoUpgradeRunner: failed to parse cron schedule", "schedule", window.Schedule)
		return false
	}

	duration := DefaultMaintenanceWindowDuration
	if window.Duration != nil {
		duration = window.Duration.Duration
	}

	now := time.Now().UTC()
	return isWithinWindow(schedule, duration, now)
}

// isWithinWindow checks if `now` falls within a window that opened at the most recent
// cron tick before `now` and stays open for `duration`.
func isWithinWindow(schedule cron.Schedule, duration time.Duration, now time.Time) bool {
	// Find the most recent cron tick at or before now.
	// cron.Schedule.Next() gives the NEXT time after a given time.
	// To find the most recent tick, we go back by duration+1min and scan forward.
	searchStart := now.Add(-duration - time.Minute)
	tick := schedule.Next(searchStart)

	// Walk forward through ticks to find the most recent one at or before now.
	for tick.Before(now) || tick.Equal(now) {
		windowEnd := tick.Add(duration)
		if now.Before(windowEnd) || now.Equal(windowEnd) {
			return true
		}
		tick = schedule.Next(tick)
	}
	return false
}

// tagWorkspaceForUpgrade adds the upgrade-to-version label and start-time annotation to a Workspace.
func (r *AutoUpgradeRunner) tagWorkspaceForUpgrade(ctx context.Context, isObj *kaitov1beta1.InferenceSet, ws *kaitov1beta1.Workspace, desiredTag string) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-read the latest version to avoid conflicts.
		latestWs := &kaitov1beta1.Workspace{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(ws), latestWs); err != nil {
			return err
		}
		patch := client.MergeFrom(latestWs.DeepCopy())
		if latestWs.Labels == nil {
			latestWs.Labels = make(map[string]string)
		}
		latestWs.Labels[kaitov1alpha1.LabelUpgradeToVersion] = desiredTag
		if latestWs.Annotations == nil {
			latestWs.Annotations = make(map[string]string)
		}
		latestWs.Annotations[AnnotationUpgradeStartTime] = time.Now().UTC().Format(time.RFC3339)
		return r.Client.Patch(ctx, latestWs, patch)
	})
	if err != nil {
		klog.ErrorS(err, "AutoUpgradeRunner: failed to tag workspace for upgrade",
			"workspace", klog.KObj(ws), "targetVersion", desiredTag)
		return
	}
	klog.InfoS("AutoUpgradeRunner: tagged workspace for upgrade",
		"workspace", klog.KObj(ws), "targetVersion", desiredTag, "inferenceset", klog.KObj(isObj))
}

// isWorkspaceInDesiredState returns true if the workspace's StatefulSet is running
// the desired image and all replicas are ready with no pending rollout.
func isWorkspaceInDesiredState(ss *appsv1.StatefulSet, desiredImage string) bool {
	if workspace.GetInferenceContainerImage(ss) != desiredImage {
		return false
	}
	replicas := int32(1)
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	return ss.Status.ReadyReplicas == replicas &&
		ss.Generation == ss.Status.ObservedGeneration
}
