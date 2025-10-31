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

package inferenceset

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
)

var (
	inferencesetPhaseCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kaito_inferenceset_count",
			Help: "Number of InferenceSets in a certain phase (succeeded, error, pending, deleting)",
		},
		[]string{"phase"},
	)
)

func init() {
	metrics.Registry.MustRegister(inferencesetPhaseCount)
}

func monitorInferenceSets(ctx context.Context, k8sClient client.Client) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var isList kaitov1alpha1.InferenceSetList

			if err := k8sClient.List(ctx, &isList); err != nil {
				klog.Errorf("failed to list all inferencesets: %v", err)
				inferencesetPhaseCount.Reset()
				continue
			}

			phaseCounts := map[string]float64{
				"succeeded": 0,
				"error":     0,
				"pending":   0,
				"deleting":  0,
			}

			for _, is := range isList.Items {
				phase := determineInferenceSetPhase(&is)
				if _, ok := phaseCounts[phase]; !ok {
					phaseCounts[phase] = 0
				}
				phaseCounts[phase]++
			}

			for phase, count := range phaseCounts {
				inferencesetPhaseCount.WithLabelValues(phase).Set(count)
			}
		}
	}
}

func determineInferenceSetPhase(is *kaitov1alpha1.InferenceSet) string {
	for _, cond := range is.Status.Conditions {
		switch kaitov1alpha1.ConditionType(cond.Type) {
		case kaitov1alpha1.InferenceSetConditionTypeDeleting:
			if cond.Status == metav1.ConditionTrue {
				return "deleting"
			}
		case kaitov1alpha1.InferenceSetConditionTypeReady:
			if cond.Status == metav1.ConditionTrue {
				return "ready"
			}
			if cond.Status == metav1.ConditionFalse {
				return "error"
			}
		}
	}
	return "pending"
}
