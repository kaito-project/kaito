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
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

func TestInferenceSetSyncControllerRevision(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		inferenceset  v1alpha1.InferenceSet
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
									InferenceSetHashAnnotation: "be5369aea1bec8fc674d900b229888f4b17739e647ca53509953bf2f1f2c0121",
								},
							},
							Revision: 1,
						}
					}).
					Return(nil)
				// Add mock for inferenceset retrieval in updateInferenceSetWithRetry
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1alpha1.InferenceSet)
						*ws = test.MockInferenceSetWithComputeHash
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
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
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1alpha1.InferenceSet)
						*ws = test.MockInferenceSetSuccessful
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
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
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1alpha1.InferenceSet)
						*ws = test.MockInferenceSetWithDeleteOldCR
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
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
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
					Run(func(args mock.Arguments) {
						ws := args.Get(2).(*v1alpha1.InferenceSet)
						*ws = test.MockInferenceSetUpdateCR
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1alpha1.InferenceSet{}), mock.Anything).
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

				helmRelease, _ := manifests.GenerateInferencePoolHelmRelease(test.MockInferenceSetWithPresetVLLM, false)
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
