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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

func TestInferenceSetSyncControllerRevision(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		inferenceset  v1beta1.InferenceSet
		expectedError error
		verifyCalls   func(c *test.MockClient)
	}{

		"No new revision needed": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.ControllerRevision)
						*dep = appsv1.ControllerRevision{
							ObjectMeta: v1.ObjectMeta{
								Annotations: map[string]string{
									InferenceSetHashAnnotation: "8b215f13847260f94d2debfebec7ee9540a7b2c08c0d5cabdfdded1ca133f6cc",
								},
							},
							Revision: 1,
						}
					}).
					Return(nil)
				// Add mock for inferenceset retrieval in updateInferenceSetWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.InferenceSet)
						*ws = test.MockInferenceSetWithComputeHash
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Return(nil)
			},
			inferenceset:  test.MockInferenceSetWithComputeHash,
			expectedError: nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for InferenceSet
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Fail to create ControllerRevision": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(errors.New("failed to create ControllerRevision"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockInferenceSetFailToCreateCR.Name))
			},
			inferenceset:  test.MockInferenceSetFailToCreateCR,
			expectedError: errors.New("failed to create new ControllerRevision: failed to create ControllerRevision"),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Successfully create new ControllerRevision": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockInferenceSetFailToCreateCR.Name))
				// Add mock for inferenceset retrieval in updateInferenceSetWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.InferenceSet)
						*ws = test.MockInferenceSetSuccessful
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Return(nil)
			},
			inferenceset:  test.MockInferenceSetSuccessful,
			expectedError: nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for InferenceSet
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Successfully delete old ControllerRevision": {
			callMocks: func(c *test.MockClient) {
				revisions := &appsv1.ControllerRevisionList{}
				jsonData, _ := json.Marshal(test.MockInferenceSetWithUpdatedDeployment)

				for i := 0; i <= consts.MaxRevisionHistoryLimit; i++ {
					revision := &appsv1.ControllerRevision{
						ObjectMeta: v1.ObjectMeta{
							Name: fmt.Sprintf("revision-%d", i),
						},
						Revision: int64(i),
						Data:     runtime.RawExtension{Raw: jsonData},
					}
					revisions.Items = append(revisions.Items, *revision)
				}
				relevantMap := c.CreateMapWithType(revisions)

				for _, obj := range revisions.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockInferenceSetFailToCreateCR.Name))
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				// Add mock for inferenceset retrieval in updateInferenceSetWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.InferenceSet)
						*ws = test.MockInferenceSetWithDeleteOldCR
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Return(nil)
			},
			inferenceset:  test.MockInferenceSetWithDeleteOldCR,
			expectedError: nil,
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for InferenceSet
				c.AssertNumberOfCalls(t, "Delete", 1)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Fail to update InferenceSet annotations": {
			callMocks: func(c *test.MockClient) {
				revisions := &appsv1.ControllerRevisionList{}
				jsonData, _ := json.Marshal(test.MockInferenceSetWithUpdatedDeployment)

				for i := 0; i <= consts.MaxRevisionHistoryLimit; i++ {
					revision := &appsv1.ControllerRevision{
						ObjectMeta: v1.ObjectMeta{
							Name: fmt.Sprintf("revision-%d", i),
						},
						Revision: int64(i),
						Data:     runtime.RawExtension{Raw: jsonData},
					}
					revisions.Items = append(revisions.Items, *revision)
				}
				relevantMap := c.CreateMapWithType(revisions)

				for _, obj := range revisions.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), test.MockInferenceSetFailToCreateCR.Name))
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				// Add mock for inferenceset retrieval in updateInferenceSetWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1beta1.InferenceSet)
						*ws = test.MockInferenceSetUpdateCR
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.InferenceSet{}), mock.Anything).
					Return(fmt.Errorf("failed to update InferenceSet annotations"))
			},
			inferenceset:  test.MockInferenceSetUpdateCR,
			expectedError: fmt.Errorf("failed to update InferenceSet annotations: %w", fmt.Errorf("failed to update InferenceSet annotations")),
			verifyCalls: func(c *test.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 2) // 1 for ControllerRevision, 1 for InferenceSet
				c.AssertNumberOfCalls(t, "Delete", 1)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
	}
	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			reconciler := &InferenceSetReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.syncControllerRevision(ctx, &tc.inferenceset)
			if tc.expectedError == nil {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
			if tc.verifyCalls != nil {
				tc.verifyCalls(mockClient)
			}
		})
	}
}

func TestEnsureGatewayAPIInferenceExtension(t *testing.T) {
	test.RegisterTestModel()
	// Ensure GPU SKU lookup works inside inference dry-run
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		featureGate   bool
		runtimeName   model.RuntimeName
		isPreset      bool
		expectedError error
	}{
		"feature gate off returns nil": {
			callMocks:     func(c *test.MockClient) {},
			featureGate:   false,
			runtimeName:   model.RuntimeNameVLLM,
			isPreset:      true,
			expectedError: nil,
		},
		"runtime not vllm returns nil": {
			callMocks:     func(c *test.MockClient) {},
			featureGate:   true,
			runtimeName:   model.RuntimeNameHuggingfaceTransformers,
			isPreset:      true,
			expectedError: nil,
		},
		"not preset returns nil": {
			callMocks:     func(c *test.MockClient) {},
			featureGate:   true,
			runtimeName:   model.RuntimeNameVLLM,
			isPreset:      false,
			expectedError: nil,
		},
		"OCIRepository and HelmRelease found and up-to-date": {
			callMocks: func(c *test.MockClient) {
				// Default inference template ConfigMap exists in target namespace
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&sourcev1.OCIRepository{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&helmv2.HelmRelease{}), mock.Anything).Return(nil)

				ociRepository := manifests.GenerateInferencePoolOCIRepository(test.MockInferenceSetWithPresetVLLM)
				ociRepository.Status.Conditions = []v1.Condition{{Type: consts.ConditionReady, Status: v1.ConditionTrue}}
				c.CreateOrUpdateObjectInMap(ociRepository)

				helmRelease, _ := manifests.GenerateInferencePoolHelmRelease(test.MockInferenceSetWithPresetVLLM)
				helmRelease.Status.Conditions = []v1.Condition{{Type: consts.ConditionReady, Status: v1.ConditionTrue}}
				c.CreateOrUpdateObjectInMap(helmRelease)

				// Mock Update call for HelmRelease (in case specs are not equal)
				c.On("Update", mock.Anything, mock.IsType(&helmv2.HelmRelease{}), mock.Anything).Return(nil)

				// mock inferenceset.ListWorkspaces return one workspace with preset VLLM
				wsList := &v1beta1.WorkspaceList{}
				wsList.Items = append(wsList.Items, *test.MockWorkspaceWithPresetVLLM)
				c.On("List", mock.Anything, mock.IsType(&v1beta1.WorkspaceList{}), mock.Anything).Run(func(args mock.Arguments) {
					wsListArg := args.Get(1).(*v1beta1.WorkspaceList)
					*wsListArg = *wsList
				}).Return(nil)
			},
			featureGate:   true,
			runtimeName:   model.RuntimeNameVLLM,
			isPreset:      true,
			expectedError: nil,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			originalFeatureGate := featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension]
			featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] = tc.featureGate
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagGatewayAPIInferenceExtension] = originalFeatureGate
			}()

			iObj := test.MockInferenceSetWithPresetVLLM.DeepCopy()
			if !tc.isPreset {
				iObj.Spec.Template.Inference.Preset = nil
			}
			// Ensure runtime selection aligns with the test case
			if tc.runtimeName != model.RuntimeNameVLLM {
				if iObj.Annotations == nil {
					iObj.Annotations = map[string]string{}
				}
				iObj.Annotations[v1beta1.AnnotationWorkspaceRuntime] = string(tc.runtimeName)
			}

			mockClient := test.NewClient()
			if tc.callMocks != nil {
				tc.callMocks(mockClient)
			}

			reconciler := &InferenceSetReconciler{Client: mockClient}
			err := reconciler.ensureGatewayAPIInferenceExtension(context.Background(), iObj)
			if tc.expectedError != nil {
				assert.ErrorContains(t, err, tc.expectedError.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}

	reconciler := &InferenceSetReconciler{Client: test.NewClient()}
	err := reconciler.ensureGatewayAPIInferenceExtension(context.Background(), nil)
	if err == nil || err.Error() != "InferenceSet object is nil" {
		t.Errorf("Expected error for nil InferenceSet, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// TestInferenceSetBenchmarkAggregation covers the TPM sum and BenchmarkCompleted
// condition logic inside addOrUpdateInferenceSet.
// --------------------------------------------------------------------------

func TestInferenceSetBenchmarkAggregation(t *testing.T) {
	makeWorkspace := func(name string, tpm string) v1beta1.Workspace {
		ws := v1beta1.Workspace{
			ObjectMeta: v1.ObjectMeta{Name: name, Namespace: "default"},
			// Mark as succeeded so DetermineWorkspacePhase returns "succeeded".
			Status: v1beta1.WorkspaceStatus{
				State: v1beta1.WorkspaceStateReady,
				Conditions: []v1.Condition{
					{Type: string(v1beta1.WorkspaceConditionTypeSucceeded), Status: v1.ConditionTrue},
					{Type: string(v1beta1.WorkspaceConditionTypeInferenceStatus), Status: v1.ConditionTrue},
					{Type: string(v1beta1.ConditionTypeResourceStatus), Status: v1.ConditionTrue},
				},
			},
		}
		if tpm != "" {
			ws.Status.Performance = &v1beta1.Performance{
				Metrics: map[string]v1beta1.Metric{
					controllers.BenchmarkMetricPeakTPM: {Value: tpm},
				},
			}
		}
		return ws
	}

	makeInferenceSet := func(replicas int, benchmarkOff bool) *v1beta1.InferenceSet {
		iObj := &v1beta1.InferenceSet{
			ObjectMeta: v1.ObjectMeta{Name: "phi-4-mini", Namespace: "default"},
			Spec:       v1beta1.InferenceSetSpec{Replicas: lo.ToPtr(int32(replicas))},
		}
		if benchmarkOff {
			iObj.Annotations = map[string]string{
				v1beta1.AnnotationDisableBenchmark: "true",
			}
		}
		return iObj
	}

	tests := map[string]struct {
		workspaces            []v1beta1.Workspace
		inferenceset          *v1beta1.InferenceSet
		expectedTPM           string
		expectBenchmarkCond   bool
		expectBenchmarkStatus v1.ConditionStatus
		expectBenchmarkMsg    string
	}{
		"all replicas benchmarked — condition True, TPM is sum": {
			workspaces: []v1beta1.Workspace{
				makeWorkspace("ws-0", "100000"),
				makeWorkspace("ws-1", "200000"),
			},
			inferenceset:          makeInferenceSet(2, false),
			expectedTPM:           "300000",
			expectBenchmarkCond:   true,
			expectBenchmarkStatus: v1.ConditionTrue,
			expectBenchmarkMsg:    "2/2 replicas benchmarked",
		},
		"partial replicas benchmarked — condition False, TPM is partial sum": {
			workspaces: []v1beta1.Workspace{
				makeWorkspace("ws-0", "100000"),
				makeWorkspace("ws-1", ""), // no result yet
			},
			inferenceset:          makeInferenceSet(2, false),
			expectedTPM:           "100000",
			expectBenchmarkCond:   true,
			expectBenchmarkStatus: v1.ConditionFalse,
			expectBenchmarkMsg:    "1/2 replicas benchmarked",
		},
		"no replicas benchmarked — no TPM, condition False": {
			workspaces: []v1beta1.Workspace{
				makeWorkspace("ws-0", ""),
				makeWorkspace("ws-1", ""),
			},
			inferenceset:          makeInferenceSet(2, false),
			expectedTPM:           "",
			expectBenchmarkCond:   true,
			expectBenchmarkStatus: v1.ConditionFalse,
			expectBenchmarkMsg:    "0/2 replicas benchmarked",
		},
		"benchmark explicitly disabled — no condition set, TPM not written": {
			workspaces: []v1beta1.Workspace{
				makeWorkspace("ws-0", "100000"),
			},
			inferenceset: makeInferenceSet(1, true),
			// TPM is aggregated regardless, but not written to status when benchmark is disabled.
			// We verify only that the annotation gate works, not the aggregation itself.
			expectedTPM:         "100000",
			expectBenchmarkCond: false,
		},
		"all replicas benchmarked but replicas count mismatch — condition False": {
			// 2 workspaces benchmarked but Spec.Replicas is 3, so not all desired are done.
			workspaces: []v1beta1.Workspace{
				makeWorkspace("ws-0", "100000"),
				makeWorkspace("ws-1", "200000"),
			},
			inferenceset:          makeInferenceSet(3, false),
			expectedTPM:           "300000",
			expectBenchmarkCond:   true,
			expectBenchmarkStatus: v1.ConditionFalse,
			expectBenchmarkMsg:    "2/3 replicas benchmarked",
		},
		"zero replicas (scale-to-zero) — benchmark not applicable": {
			workspaces:            []v1beta1.Workspace{},
			inferenceset:          makeInferenceSet(0, false),
			expectedTPM:           "",
			expectBenchmarkCond:   true,
			expectBenchmarkStatus: v1.ConditionFalse,
			expectBenchmarkMsg:    "0/0 replicas benchmarked",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			totalTPM, _, benchmarkedReplicas, hasBenchmarkResult := aggregateBenchmarkResults(tc.workspaces)

			// Verify TPM aggregation.
			if tc.expectedTPM == "" {
				assert.False(t, hasBenchmarkResult, "expected no benchmark result")
			} else {
				assert.True(t, hasBenchmarkResult)
				assert.Equal(t, tc.expectedTPM, strconv.FormatFloat(totalTPM, 'f', -1, 64))
			}

			// Verify benchmark condition gate — annotation controls whether the condition is set.
			benchmarkEnabled := v1beta1.IsInferenceSetBenchmarkEnabled(tc.inferenceset)
			if !tc.expectBenchmarkCond {
				assert.False(t, benchmarkEnabled)
				return
			}

			assert.True(t, benchmarkEnabled)

			allBenchmarked := tc.inferenceset.Spec.Replicas != nil && benchmarkedReplicas == int(*tc.inferenceset.Spec.Replicas) && *tc.inferenceset.Spec.Replicas > 0
			if tc.expectBenchmarkStatus == v1.ConditionTrue {
				assert.True(t, allBenchmarked)
			} else {
				assert.False(t, allBenchmarked)
			}
			assert.Equal(t, tc.expectBenchmarkMsg,
				fmt.Sprintf("%d/%d replicas benchmarked", benchmarkedReplicas, *tc.inferenceset.Spec.Replicas))
		})
	}
}

func TestSelectWorkspacesToDelete(t *testing.T) {
	ctx := context.Background()
	const ns = "default"
	desiredImage := inference.GetBaseImageName()
	oldImage := desiredImage + "-old"

	type wsSpec struct {
		name        string
		ready       bool
		old         bool
		terminating bool
	}

	build := func(specs []wsSpec) ([]v1beta1.Workspace, []client.Object) {
		var wss []v1beta1.Workspace
		var objs []client.Object
		for _, s := range specs {
			ws := v1beta1.Workspace{ObjectMeta: v1.ObjectMeta{Name: s.name, Namespace: ns}}
			if s.terminating {
				now := v1.Now()
				ws.DeletionTimestamp = &now
				ws.Finalizers = []string{"kaito.sh/test"}
			}
			if s.ready {
				ws.Status.Conditions = []v1.Condition{{
					Type:               string(v1beta1.WorkspaceConditionTypeSucceeded),
					Status:             v1.ConditionTrue,
					Reason:             "ready",
					LastTransitionTime: v1.Now(),
				}}
			}
			wss = append(wss, ws)

			img := desiredImage
			if s.old {
				img = oldImage
			}
			objs = append(objs, &appsv1.StatefulSet{
				ObjectMeta: v1.ObjectMeta{Name: s.name, Namespace: ns},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: s.name, Image: img}}},
					},
				},
			})
		}
		return wss, objs
	}

	oldByName := func(specs []wsSpec) map[string]bool {
		m := make(map[string]bool)
		for _, s := range specs {
			m[s.name] = s.old
		}
		return m
	}

	tests := []struct {
		name            string
		specs           []wsSpec
		desiredReplicas int
		numToDelete     int
		wantDeleted     int
		wantOldDeleted  int
		wantNewDeleted  int
	}{
		{
			// Surge in flight: 3 old Ready + 1 new not-Ready. The controller must wait
			// for the new replica to become Ready before retiring an old one.
			name: "waits for new-image replica to become Ready",
			specs: []wsSpec{
				{name: "old-1", ready: true, old: true},
				{name: "old-2", ready: true, old: true},
				{name: "old-3", ready: true, old: true},
				{name: "new-1", ready: false, old: false},
			},
			desiredReplicas: 3,
			numToDelete:     1,
			wantDeleted:     0,
		},
		{
			// New replica is now Ready: retire exactly one old-image workspace.
			name: "retires an old-image replica once the new one is Ready",
			specs: []wsSpec{
				{name: "old-1", ready: true, old: true},
				{name: "old-2", ready: true, old: true},
				{name: "old-3", ready: true, old: true},
				{name: "new-1", ready: true, old: false},
			},
			desiredReplicas: 3,
			numToDelete:     1,
			wantDeleted:     1,
			wantOldDeleted:  1,
		},
		{
			// User scale-down during an upgrade: prefer removing old-image workspaces.
			name: "scale down prefers old-image workspaces",
			specs: []wsSpec{
				{name: "old-1", ready: true, old: true},
				{name: "old-2", ready: true, old: true},
				{name: "new-1", ready: true, old: false},
				{name: "new-2", ready: true, old: false},
			},
			desiredReplicas: 2,
			numToDelete:     2,
			wantDeleted:     2,
			wantOldDeleted:  2,
		},
		{
			// Hard scale-down: not enough old workspaces, so new ones must go too.
			name: "deletes new-image workspaces when surplus exceeds old count",
			specs: []wsSpec{
				{name: "old-1", ready: true, old: true},
				{name: "new-1", ready: true, old: false},
				{name: "new-2", ready: true, old: false},
				{name: "new-3", ready: true, old: false},
			},
			desiredReplicas: 1,
			numToDelete:     3,
			wantDeleted:     3,
			wantOldDeleted:  1,
			wantNewDeleted:  2,
		},
		{
			// A workspace already terminating counts toward the target without a new delete.
			name: "terminating workspace counts toward the target",
			specs: []wsSpec{
				{name: "old-1", terminating: true, old: true},
				{name: "old-2", ready: true, old: true},
				{name: "old-3", ready: true, old: true},
			},
			desiredReplicas: 2,
			numToDelete:     1,
			wantDeleted:     0,
		},
		{
			// A not-ready old workspace is retired first (free: does not reduce Ready count).
			name: "deletes not-ready old workspace first",
			specs: []wsSpec{
				{name: "old-1", ready: false, old: true},
				{name: "new-1", ready: true, old: false},
				{name: "new-2", ready: true, old: false},
			},
			desiredReplicas: 2,
			numToDelete:     1,
			wantDeleted:     1,
			wantOldDeleted:  1,
		},
		{
			// Scale-to-zero: no Ready floor to preserve, so every workspace is retired
			// (old-image ones first), even Ready ones.
			name: "scale to zero deletes all workspaces",
			specs: []wsSpec{
				{name: "old-1", ready: true, old: true},
				{name: "new-1", ready: true, old: false},
			},
			desiredReplicas: 0,
			numToDelete:     2,
			wantDeleted:     2,
			wantOldDeleted:  1,
			wantNewDeleted:  1,
		},
	}

	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wss, objs := build(tt.specs)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			c := &InferenceSetReconciler{Client: cl}

			toDelete, err := c.selectWorkspacesToDelete(ctx, wss, tt.desiredReplicas, tt.numToDelete)
			assert.NoError(t, err)
			assert.Len(t, toDelete, tt.wantDeleted)

			isOld := oldByName(tt.specs)
			oldDeleted, newDeleted := 0, 0
			for _, ws := range toDelete {
				if isOld[ws.Name] {
					oldDeleted++
				} else {
					newDeleted++
				}
			}
			assert.Equal(t, tt.wantOldDeleted, oldDeleted, "old workspaces deleted")
			assert.Equal(t, tt.wantNewDeleted, newDeleted, "new workspaces deleted")
		})
	}
}
