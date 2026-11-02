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
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
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

// A StatefulSet update leaves the previous status and ready pods visible until
// the StatefulSet controller observes and completes the new template revision.
// The write-once benchmark must not capture those old pods' logs in that window.
func TestBenchmarkCollectionWaitsForStatefulSetRollout(t *testing.T) {
	var logRequests atomic.Int32
	var replacementServing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/default/pods/rollout-0/log" {
			http.NotFound(w, r)
			return
		}
		logRequests.Add(1)
		value := 123
		if replacementServing.Load() {
			value = 456
		}
		fmt.Fprintf(w, "KAITO_BENCHMARK_RESULT 2026-11-02T00:00:00Z {\"vllm_total_tpm\":%d}\n", value)
	}))
	t.Cleanup(server.Close)
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)
	previousClient := k8sclient.GetGlobalClientGoClient()
	k8sclient.SetGlobalClientGoClient(clientset)
	t.Cleanup(func() { k8sclient.SetGlobalClientGoClient(previousClient) })

	for _, tc := range []struct {
		name      string
		mutate    func(*appsv1.StatefulSet)
		wantReady bool
	}{
		{name: "old generation with all old pods ready", mutate: func(ss *appsv1.StatefulSet) {
			ss.Status.ObservedGeneration = 1
			ss.Status.CurrentRevision = "old"
			ss.Status.UpdateRevision = "old"
		}},
		{name: "new generation observed but no pods updated", mutate: func(ss *appsv1.StatefulSet) {
			ss.Status.CurrentRevision = "old"
			ss.Status.UpdatedReplicas = 0
		}},
		{name: "partially updated distributed workload", mutate: func(ss *appsv1.StatefulSet) {
			ss.Spec.Replicas = ptr.To(int32(2))
			ss.Status.ReadyReplicas = 2
			ss.Status.CurrentRevision = "old"
		}},
		{name: "all pods updated but current revision not promoted", mutate: func(ss *appsv1.StatefulSet) {
			ss.Status.CurrentRevision = "old"
		}},
		{name: "matching revisions but updated count incomplete", mutate: func(ss *appsv1.StatefulSet) {
			ss.Status.UpdatedReplicas = 0
		}},
		{name: "revision not yet initialized", mutate: func(ss *appsv1.StatefulSet) {
			ss.Status.CurrentRevision = ""
			ss.Status.UpdateRevision = ""
		}},
		{name: "replacement pod not ready", mutate: func(ss *appsv1.StatefulSet) {
			ss.Status.ReadyReplicas = 0
		}},
		{name: "scaled to zero", mutate: func(ss *appsv1.StatefulSet) {
			ss.Spec.Replicas = ptr.To(int32(0))
			ss.Status.ReadyReplicas = 0
			ss.Status.UpdatedReplicas = 0
		}},
		{name: "completed rollout", wantReady: true},
		{name: "completed distributed rollout", wantReady: true, mutate: func(ss *appsv1.StatefulSet) {
			ss.Spec.Replicas = ptr.To(int32(2))
			ss.Status.ReadyReplicas = 2
			ss.Status.UpdatedReplicas = 2
		}},
		{name: "default single replica", wantReady: true, mutate: func(ss *appsv1.StatefulSet) {
			ss.Spec.Replicas = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logRequests.Store(0)
			replacementServing.Store(tc.wantReady)
			ws := test.MockWorkspaceWithPresetVLLM.DeepCopy()
			ws.Name, ws.Namespace = "rollout", "default"
			ws.Annotations = map[string]string{v1beta1.AnnotationWorkspaceRuntime: "vllm"}
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: ws.Name, Namespace: ws.Namespace, Generation: 2},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(1)),
					Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name: ws.Name,
						StartupProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{
							Command: []string{"python3", "/workspace/vllm/benchmark_entrypoint.py"},
						}}},
					}}}},
				},
				Status: appsv1.StatefulSetStatus{
					ObservedGeneration: 2, ReadyReplicas: 1, UpdatedReplicas: 1,
					CurrentRevision: "new", UpdateRevision: "new",
				},
			}
			if tc.mutate != nil {
				tc.mutate(ss)
			}
			c := fake.NewClientBuilder().WithScheme(targetTransitionScheme(t)).WithObjects(ss).Build()
			r := &WorkspaceReconciler{Client: c}
			ready, applicable, reason, message, err := r.collectInferenceReadyStatus(t.Context(), ws)
			require.NoError(t, err)
			require.True(t, applicable)
			require.Equal(t, tc.wantReady, ready)

			// Mimic deferred status sync immediately after resetBenchmarkOnRollout.
			status := &v1beta1.WorkspaceStatus{State: v1beta1.WorkspaceStateReady}
			applyInferenceWorkspaceStatus(t.Context(), status, ws, buildReconcileErrMessageAppender(nil),
				ready, metav1.ConditionTrue, applicable, reason, message)
			if tc.wantReady {
				require.Equal(t, int32(1), logRequests.Load())
				require.Equal(t, "456", status.Performance.Metrics[BenchmarkMetricPeakTPM].Value)
				require.True(t, meta.IsStatusConditionTrue(status.Conditions, string(v1beta1.WorkspaceConditionTypeBenchmarkCompleted)))
				// The replacement metrics remain write-once after successful collection.
				applyInferenceWorkspaceStatus(t.Context(), status, ws, buildReconcileErrMessageAppender(nil),
					ready, metav1.ConditionTrue, applicable, reason, message)
				require.Equal(t, int32(1), logRequests.Load())
			} else {
				require.Zero(t, logRequests.Load(), "must not read old pod logs before rollout completes")
				require.Nil(t, status.Performance)
				require.Nil(t, meta.FindStatusCondition(status.Conditions, string(v1beta1.WorkspaceConditionTypeBenchmarkCompleted)))
				require.Equal(t, v1beta1.WorkspaceStateNotReady, status.State)
			}
		})
	}
}
