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

package controllers

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

var (
	workspacePhaseCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kaito_workspace_count",
			Help: "Number of Workspaces in a certain phase (succeeded, error, pending, deleting)",
		},
		[]string{"phase"},
	)

	workspacePresetCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kaito_workspace_preset_count",
			Help: "Number of Workspaces using each preset model, by preset name",
		},
		[]string{"preset_name"},
	)

	workspacePVCAllocatedBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kaito_workspace_pvc_allocated_bytes",
			Help: "Allocated (requested) PVC storage in bytes per PVC associated with a workspace",
		},
		[]string{"workspace_name", "workspace_namespace", "pvc_name"},
	)

	workspacePVCCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kaito_workspace_pvc_count",
			Help: "Number of PVCs associated with each workspace",
		},
		[]string{"workspace_name", "workspace_namespace"},
	)
)

func init() {
	metrics.Registry.MustRegister(workspacePhaseCount)
	metrics.Registry.MustRegister(workspacePresetCount)
	metrics.Registry.MustRegister(workspacePVCAllocatedBytes)
	metrics.Registry.MustRegister(workspacePVCCount)
}

func monitorWorkspaces(ctx context.Context, k8sClient client.Client) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var wsList kaitov1beta1.WorkspaceList

			if err := k8sClient.List(ctx, &wsList); err != nil {
				klog.Errorf("failed to list all workspaces: %v", err)
				workspacePhaseCount.Reset()
				workspacePresetCount.Reset()
				continue
			}

			phaseCounts := map[string]float64{
				"succeeded": 0,
				"error":     0,
				"pending":   0,
				"deleting":  0,
			}
			presetCounts := map[string]float64{}

			for _, ws := range wsList.Items {
				phase := DetermineWorkspacePhase(&ws)
				if _, ok := phaseCounts[phase]; !ok {
					phaseCounts[phase] = 0
				}
				phaseCounts[phase]++

				if name := getWorkspacePresetName(&ws); name != "" {
					presetCounts[name]++
				}
			}

			for phase, count := range phaseCounts {
				workspacePhaseCount.WithLabelValues(phase).Set(count)
			}

			// Reset before re-setting so to remove stale keys
			workspacePresetCount.Reset()
			for preset, count := range presetCounts {
				workspacePresetCount.WithLabelValues(preset).Set(count)
			}

			collectPVCMetrics(ctx, k8sClient)
		}
	}
}

func collectPVCMetrics(ctx context.Context, k8sClient client.Client) {
	var pvcList corev1.PersistentVolumeClaimList
	if err := k8sClient.List(ctx, &pvcList); err != nil {
		klog.Errorf("failed to list PVCs for volume metrics: %v", err)
		workspacePVCAllocatedBytes.Reset()
		workspacePVCCount.Reset()
		return
	}

	workspacePVCAllocatedBytes.Reset()
	workspacePVCCount.Reset()

	pvcCounts := map[string]map[string]float64{} // namespace -> workspace name -> count

	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		wsName, ok := pvc.Labels[kaitov1beta1.LabelWorkspaceName]
		if !ok || wsName == "" {
			continue // Not a KAITO-managed PVC
		}
		wsNamespace := pvc.Namespace

		if pvcCounts[wsNamespace] == nil {
			pvcCounts[wsNamespace] = map[string]float64{}
		}
		pvcCounts[wsNamespace][wsName]++

		// Prefer actual allocated capacity from status; fall back to spec request
		var storageBytes float64
		if allocated, exists := pvc.Status.Capacity[corev1.ResourceStorage]; exists {
			storageBytes = float64(allocated.Value())
		} else if requested, exists := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; exists {
			storageBytes = float64(requested.Value())
		}

		workspacePVCAllocatedBytes.WithLabelValues(wsName, wsNamespace, pvc.Name).Set(storageBytes)
	}

	for ns, workspaces := range pvcCounts {
		for wsName, count := range workspaces {
			workspacePVCCount.WithLabelValues(wsName, ns).Set(count)
		}
	}
}

func getWorkspacePresetName(ws *kaitov1beta1.Workspace) string {
	if ws != nil && ws.Inference != nil && ws.Inference.Preset != nil {
		return string(ws.Inference.Preset.Name)
	}
	return ""
}

func DetermineWorkspacePhase(ws *kaitov1beta1.Workspace) string {
	for _, cond := range ws.Status.Conditions {
		switch kaitov1beta1.ConditionType(cond.Type) {
		case kaitov1beta1.WorkspaceConditionTypeDeleting:
			if cond.Status == metav1.ConditionTrue {
				return "deleting"
			}
		case kaitov1beta1.WorkspaceConditionTypeSucceeded:
			if cond.Status == metav1.ConditionTrue {
				return "succeeded"
			}
			if cond.Status == metav1.ConditionFalse {
				return "error"
			}
		}
	}
	return "pending"
}
