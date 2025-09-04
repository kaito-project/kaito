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

	presetModelCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kaito_preset_model_count",
			Help: "Number of Workspaces using each preset model",
		},
		[]string{"model"},
	)
)

func init() {
	metrics.Registry.MustRegister(workspacePhaseCount)
	metrics.Registry.MustRegister(presetModelCount)
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
				presetModelCount.Reset()
				continue
			}

			phaseCounts := map[string]float64{
				"succeeded": 0,
				"error":     0,
				"pending":   0,
				"deleting":  0,
			}

			modelCounts := make(map[string]float64)

			for _, ws := range wsList.Items {
				phase := determineWorkspacePhase(&ws)
				if _, ok := phaseCounts[phase]; !ok {
					phaseCounts[phase] = 0
				}
				phaseCounts[phase]++

				// Count preset models from this workspace
				wsModelCounts := countPresetModels(&ws)
				for modelName, count := range wsModelCounts {
					if _, ok := modelCounts[modelName]; !ok {
						modelCounts[modelName] = 0
					}
					modelCounts[modelName] += count
				}
			}

			for phase, count := range phaseCounts {
				workspacePhaseCount.WithLabelValues(phase).Set(count)
			}

			presetModelCount.Reset()
			for model, count := range modelCounts {
				presetModelCount.WithLabelValues(model).Set(count)
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

func countPresetModels(ws *kaitov1beta1.Workspace) map[string]float64 {
	modelCounts := make(map[string]float64)

	if ws == nil {
		return modelCounts
	}

	modelsInWorkspace := make(map[string]struct{})

	if ws.Inference != nil && ws.Inference.Preset != nil && ws.Inference.Preset.Name != "" {
		modelsInWorkspace[string(ws.Inference.Preset.Name)] = struct{}{}
	}

	if ws.Tuning != nil && ws.Tuning.Preset != nil && ws.Tuning.Preset.Name != "" {
		modelsInWorkspace[string(ws.Tuning.Preset.Name)] = struct{}{}
	}

	for modelName := range modelsInWorkspace {
		modelCounts[modelName] = 1
	}

	return modelCounts
}
