// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
)

func init() {
	metrics.Registry.MustRegister(workspacePhaseCount)
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
				continue
			}

			phaseCounts := map[string]float64{
				"succeeded": 0,
				"error":     0,
				"pending":   0,
				"deleting":  0,
			}

			for _, ws := range wsList.Items {
				phase := determineWorkspacePhase(&ws)
				if _, ok := phaseCounts[phase]; !ok {
					phaseCounts[phase] = 0
				}
				phaseCounts[phase]++
			}

			for phase, count := range phaseCounts {
				workspacePhaseCount.WithLabelValues(phase).Set(count)
			}
		}
	}
}

func determineWorkspacePhase(ws *kaitov1beta1.Workspace) string {
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
