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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func seedCompletedBenchmark(ws *v1beta1.Workspace) {
	ws.Status.Performance = &v1beta1.Performance{Metrics: map[string]v1beta1.Metric{
		"peakTokensPerMinute": {Value: "123", Config: map[string]string{"engineVersion": "old"}},
	}}
	meta.SetStatusCondition(&ws.Status.Conditions, metav1.Condition{
		Type: string(v1beta1.WorkspaceConditionTypeBenchmarkCompleted), Status: metav1.ConditionTrue,
		Reason: "BenchmarkCompleted", Message: "Benchmark completed", ObservedGeneration: ws.Generation,
		LastTransitionTime: metav1.NewTime(time.Unix(1, 0)),
	})
}

func requireBenchmarkStatus(t *testing.T, c client.Client, ws *v1beta1.Workspace, cleared bool) {
	t.Helper()
	stored := &v1beta1.Workspace{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), stored))
	if cleared {
		require.Nil(t, stored.Status.Performance)
		require.Nil(t, meta.FindStatusCondition(stored.Status.Conditions, string(v1beta1.WorkspaceConditionTypeBenchmarkCompleted)))
	} else {
		require.Equal(t, ws.Status.Performance, stored.Status.Performance)
		require.Equal(t, ws.Status.Conditions, stored.Status.Conditions)
	}
}

func TestBenchmarkResetOnVLLMTargetRollout(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	test.RegisterTestModel()
	for _, gate := range []string{consts.FeatureFlagVLLM, consts.FeatureFlagEnableCPUInference, consts.FeatureFlagDisableNodeAutoProvisioning} {
		old := featuregates.FeatureGates[gate]
		t.Cleanup(func() { featuregates.FeatureGates[gate] = old })
		featuregates.FeatureGates[gate] = true
	}
	for _, tc := range []struct {
		name                 string
		cpuToGPU, failUpdate bool
	}{
		{name: "GPU to CPU"},
		{name: "CPU to GPU", cpuToGPU: true},
		{name: "failed GPU to CPU", failUpdate: true},
		{name: "failed CPU to GPU", cpuToGPU: true, failUpdate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			ws.Annotations = map[string]string{v1beta1.AnnotationWorkspaceRuntime: "vllm", v1beta1.WorkspaceRevisionAnnotation: "1"}
			ws.Resource.InstanceType = "Standard_NC24ads_A100_v4"
			if tc.cpuToGPU {
				ws.Resource.InstanceType = "Standard_D4s_v3"
			}
			seedCompletedBenchmark(ws)
			failUpdate := false
			updateErr := errors.New("workload update rejected")
			c := fake.NewClientBuilder().WithScheme(targetTransitionScheme(t)).WithStatusSubresource(ws).
				WithObjects(ws, targetTransitionGPUNode()).WithInterceptorFuncs(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*appsv1.StatefulSet); ok && failUpdate {
						return updateErr
					}
					return c.Update(ctx, obj, opts...)
				},
			}).Build()
			r := &WorkspaceReconciler{Client: c}
			require.NoError(t, r.applyInference(t.Context(), ws))
			before := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), before))
			// An unchanged reconcile must retain a completed benchmark.
			require.NoError(t, r.applyInference(t.Context(), ws))
			requireBenchmarkStatus(t, c, ws, false)
			ws.Resource.InstanceType = "Standard_D4s_v3"
			if tc.cpuToGPU {
				ws.Resource.InstanceType = "Standard_NC24ads_A100_v4"
			}
			failUpdate = tc.failUpdate
			err := r.applyInference(t.Context(), ws)
			after := &appsv1.StatefulSet{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(ws), after))
			if tc.failUpdate {
				require.ErrorIs(t, err, updateErr)
				require.Equal(t, before, after)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.cpuToGPU, inferencePodUsesGPU(&after.Spec.Template.Spec))
				require.NotEqual(t, before.Spec.Template.Spec.Containers[0].Image, after.Spec.Template.Spec.Containers[0].Image)
			}
			requireBenchmarkStatus(t, c, ws, !tc.failUpdate)
		})
	}
}
