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

package ragengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	awsv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/testutil"
	"github.com/kaito-project/kaito/pkg/apis"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestApplyRAGEngineResource(t *testing.T) {
	testutil.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		expectedError error
		ragengine     v1beta1.RAGEngine
	}{
		"Fail to apply ragengine because associated nodeClaim cannot be retrieved": {
			callMocks: func(c *testutil.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("failed to retrieve nodeClaims"))

			},
			ragengine:     *testutil.MockRAGEngineDistributedModel,
			expectedError: errors.New("failed to retrieve nodeClaims"),
		},
		"Fail to apply ragengine with nodeClaims because can't get qualified nodes": {
			callMocks: func(c *testutil.MockClient) {
				nodeClaimList := testutil.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&testutil.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range nodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("failed to list nodes"))
			},
			ragengine:     *testutil.MockRAGEngineDistributedModel,
			expectedError: errors.New("failed to list nodes"),
		},
		"Successfully apply ragengine resource with nodeClaim": {
			callMocks: func(c *testutil.MockClient) {
				nodeList := testutil.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}
				node1 := testutil.MockNodeList.Items[0]
				//insert node object into the map
				c.CreateOrUpdateObjectInMap(&node1)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)

			},
			ragengine:     *testutil.MockRAGEngineDistributedModel,
			expectedError: nil,
		},
		"Successfully apply ragengine resource with nodeClaim and preferred nodes": {
			callMocks: func(c *testutil.MockClient) {
				nodeList := testutil.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}
				node1 := testutil.MockNodeList.Items[0]
				//insert node object into the map
				c.CreateOrUpdateObjectInMap(&node1)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithPreferredNodes,
			expectedError: nil,
		},
		"Update node Failed with NotFound error": {
			callMocks: func(c *testutil.MockClient) {
				nodeList := testutil.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(apierrors.NewNotFound(corev1.Resource("Node"), "node1"))

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineDistributedModel,
			expectedError: apierrors.NewNotFound(corev1.Resource("Node"), "node1"),
		},
		"RAGEngine with no inference service should succeed": {
			callMocks: func(c *testutil.MockClient) {
				nodeList := testutil.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}
				node1 := testutil.MockNodeList.Items[0]
				//insert node object into the map
				c.CreateOrUpdateObjectInMap(&node1)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithNoInferenceService,
			expectedError: nil,
		},
	}

	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			mockNodeClaim := &karpenterv1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockNodeClaim, key)
				mockNodeClaim.Status.Conditions = []status.Condition{
					{
						Type:   string(apis.ConditionReady),
						Status: v1.ConditionTrue,
					},
				}
				mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
			}

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.applyRAGEngineResource(ctx, &tc.ragengine)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestGetAllQualifiedNodesforRAGEngine(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		expectedError error
	}{
		"Fails to get qualified nodes because can't list nodes": {
			callMocks: func(c *testutil.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("Failed to list nodes"))
			},
			expectedError: errors.New("Failed to list nodes"),
		},
		"Gets all qualified nodes": {
			callMocks: func(c *testutil.MockClient) {
				nodeList := testutil.MockNodeList
				deletedNode := corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node4",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NV36ads_A10_v5",
						},
						DeletionTimestamp: &v1.Time{Time: time.Now()},
					},
				}
				nodeList.Items = append(nodeList.Items, deletedNode)

				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range testutil.MockNodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			mockRAGEngine := testutil.MockRAGEngineDistributedModel
			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			tc.callMocks(mockClient)

			nodes, err := reconciler.getAllQualifiedNodes(ctx, mockRAGEngine)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
				assert.Check(t, nodes != nil, "Response node array should not be nil")
				assert.Check(t, len(nodes) == 1, "One out of three nodes should be qualified")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
				assert.Check(t, nodes == nil, "Response node array should be nil")
			}
		})
	}
}

func TestCreateAndValidateMachineNodeforRAGEngine(t *testing.T) {
	testutil.RegisterTestModel()
	testcases := map[string]struct {
		callMocks        func(c *testutil.MockClient)
		cloudProvider    string
		objectConditions []status.Condition
		ragengine        v1beta1.RAGEngine
		expectedError    error
	}{
		"An Azure nodeClaim is successfully created": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			cloudProvider: consts.AzureCloudName,
			objectConditions: []status.Condition{
				{
					Type:   string(apis.ConditionReady),
					Status: v1.ConditionTrue,
				},
			},
			ragengine:     *testutil.MockRAGEngineDistributedModel,
			expectedError: nil,
		},
		"An AWS nodeClaim is successfully created": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&awsv1.EC2NodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&awsv1.EC2NodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			cloudProvider: consts.AWSCloudName,
			objectConditions: []status.Condition{
				{
					Type:   string(apis.ConditionReady),
					Status: v1.ConditionTrue,
				},
			},
			ragengine:     *testutil.MockRAGEngineDistributedModel,
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			mockNodeClaim := &karpenterv1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockNodeClaim, key)
				mockNodeClaim.Status.Conditions = tc.objectConditions
				mockNodeClaim.Status.NodeName = "test-node"
				mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
			}

			if tc.cloudProvider != "" {
				t.Setenv("CLOUD_PROVIDER", tc.cloudProvider)

			}

			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			node, err := reconciler.createAndValidateNode(ctx, &tc.ragengine)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
				assert.Check(t, node != nil, "Response node should not be nil")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestUpdateControllerRevision1(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		ragengine     v1beta1.RAGEngine
		expectedError error
		verifyCalls   func(c *testutil.MockClient)
	}{

		"No new revision needed": {
			callMocks: func(c *testutil.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.ControllerRevision)
						*dep = appsv1.ControllerRevision{
							ObjectMeta: v1.ObjectMeta{
								Annotations: map[string]string{
									RAGEngineHashAnnotation: "f9e0ee8d79842f43edf3d9edfe7a6130cda56d0dff37d614ded22d9e2675dda5",
								},
							},
						}
					}).
					Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Return(nil)
			},
			ragengine:     testutil.MockRAGEngineWithComputeHash,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Fail to create ControllerRevision": {
			callMocks: func(c *testutil.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(errors.New("failed to create ControllerRevision"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), testutil.MockRAGEngineFailToCreateCR.Name))
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Return(nil)
			},
			ragengine:     testutil.MockRAGEngineFailToCreateCR,
			expectedError: errors.New("failed to create new ControllerRevision: failed to create ControllerRevision"),
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
		"Successfully create new ControllerRevision": {
			callMocks: func(c *testutil.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), testutil.MockRAGEngineFailToCreateCR.Name))
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Return(nil)
			},
			ragengine:     testutil.MockRAGEngineSuccessful,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Successfully delete old ControllerRevision": {
			callMocks: func(c *testutil.MockClient) {
				revisions := &appsv1.ControllerRevisionList{}
				jsonData, _ := json.Marshal(testutil.MockRAGEngineWithUpdatedDeployment)

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
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), testutil.MockRAGEngineFailToCreateCR.Name))
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Return(nil)
			},
			ragengine:     testutil.MockRAGEngineWithDeleteOldCR,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 1)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 1)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},
	}
	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.syncControllerRevision(ctx, &tc.ragengine)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
			if tc.verifyCalls != nil {
				tc.verifyCalls(mockClient)
			}
		})
	}
}

func TestApplyRAG(t *testing.T) {
	testutil.RegisterTestModel()
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		ragengine     v1beta1.RAGEngine
		expectedError error
		verifyCalls   func(c *testutil.MockClient)
	}{

		"Fail because associated workload with ragengine cannot be retrieved": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(errors.New("Failed to get resource"))
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithRevision1,
			expectedError: errors.New("Failed to get resource"),
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 5)
				c.AssertNumberOfCalls(t, "Delete", 0)
			},
		},

		"Create preset inference because inference workload did not exist": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Times(4)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
					depObj := &appsv1.Deployment{}
					key := client.ObjectKey{Namespace: "kaito", Name: "testRAGEngine"}
					c.GetObjectFromMap(depObj, key)
					depObj.Status.ReadyReplicas = 1
					c.CreateOrUpdateObjectInMap(depObj)
				})
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine: *testutil.MockRAGEngineWithRevision1,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 7)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
			expectedError: nil,
		},

		"Apply inference from existing workload": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *testutil.MockRAGDeploymentUpdated.DeepCopy()
					}).
					Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithRevision1,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 3)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Update deployment with new configuration": {
			callMocks: func(c *testutil.MockClient) {
				// Mocking existing Deployment object
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *testutil.MockRAGDeploymentUpdated.DeepCopy()
					}).
					Return(nil)

				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithPreset,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 3)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 1)
			},
		},

		"Create RAG with no compute resources": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Times(4)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
					depObj := &appsv1.Deployment{}
					key := client.ObjectKey{Namespace: "kaito", Name: "testRAGEngine"}
					c.GetObjectFromMap(depObj, key)
					depObj.Status.ReadyReplicas = 1
					c.CreateOrUpdateObjectInMap(depObj)
				})
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine: *testutil.MockRAGEngineWithNoComputeResource,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 7)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
			expectedError: nil,
		},

		"Create RAG with no inference resources": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Times(4)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
					depObj := &appsv1.Deployment{}
					key := client.ObjectKey{Namespace: "kaito", Name: "testRAGEngine"}
					c.GetObjectFromMap(depObj, key)
					depObj.Status.ReadyReplicas = 1
					c.CreateOrUpdateObjectInMap(depObj)
				})
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithNoInferenceService,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 7)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Create RAG with no compute or inference resources": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Times(4)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil).Run(func(args mock.Arguments) {
					depObj := &appsv1.Deployment{}
					key := client.ObjectKey{Namespace: "kaito", Name: "testRAGEngine"}
					c.GetObjectFromMap(depObj, key)
					depObj.Status.ReadyReplicas = 1
					c.CreateOrUpdateObjectInMap(depObj)
				})
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     *testutil.MockRAGEngineWithNoComputeResourceAndInferenceService,
			expectedError: nil,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 7)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.applyRAG(ctx, &tc.ragengine)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
			if tc.verifyCalls != nil {
				tc.verifyCalls(mockClient)
			}
		})
	}
}

func TestEnsureService(t *testing.T) {
	testutil.RegisterTestModel()
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		expectedError error
		ragengine     v1beta1.RAGEngine
		verifyCalls   func(c *testutil.MockClient)
	}{

		"Existing service is found for RAGEngine": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			ragengine:     *testutil.MockRAGEngineWithPreset,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 0)
				c.AssertNumberOfCalls(t, "Get", 1)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Service creation fails": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(testutil.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(errors.New("cannot create service"))
			},
			expectedError: errors.New("cannot create service"),
			ragengine:     *testutil.MockRAGEngineWithPreset,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 4)
				c.AssertNumberOfCalls(t, "Get", 4)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Successfully creates a new service": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(testutil.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			ragengine:     *testutil.MockRAGEngineWithPreset,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 4)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Successfully creates a new service with no compute": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(testutil.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			ragengine:     *testutil.MockRAGEngineWithNoComputeResource,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 4)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Successfully creates a new service with no inference": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(testutil.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			ragengine:     *testutil.MockRAGEngineWithNoInferenceService,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 4)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},

		"Successfully creates a new service with no compute or inference": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(testutil.NotFoundError())
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			ragengine:     *testutil.MockRAGEngineWithNoComputeResourceAndInferenceService,
			verifyCalls: func(c *testutil.MockClient) {
				c.AssertNumberOfCalls(t, "List", 0)
				c.AssertNumberOfCalls(t, "Create", 1)
				c.AssertNumberOfCalls(t, "Get", 4)
				c.AssertNumberOfCalls(t, "Delete", 0)
				c.AssertNumberOfCalls(t, "Update", 0)
			},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.ensureService(ctx, &tc.ragengine)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
			if tc.verifyCalls != nil {
				tc.verifyCalls(mockClient)
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	testutil.RegisterTestModel()
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	mockRAGEngineDistributedModel0Node := testutil.MockRAGEngine
	mockRAGEngineDistributedModel0Node.Spec.InferenceService = &v1beta1.InferenceServiceSpec{
		URL: "http://example.com/inference",
	}
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		ragengine     *v1beta1.RAGEngine
		expectedError error
		expectRequeue bool
	}{
		"RAGEngine not found - should return without error": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "RAGEngine"}, "test-ragengine"))
			},
			ragengine:     nil,
			expectedError: nil,
			expectRequeue: false,
		},
		"Successfully reconcile RAGEngine without deletion timestamp": {
			callMocks: func(c *testutil.MockClient) {
				ragengine := mockRAGEngineDistributedModel0Node.DeepCopy()
				ragengine.Finalizers = []string{} // No finalizer initially

				deployment := testutil.MockDeploymentUpdated.DeepCopy()
				deployment.Finalizers = []string{} // No finalizer initially

				nodeClaim := testutil.MockNodeClaim.DeepCopy()
				nodeClaim.Status.Conditions = []status.Condition{
					{
						Type:   string(apis.ConditionReady),
						Status: v1.ConditionTrue,
					},
				}
				nodeClaim.Finalizers = []string{} // No finalizer initially

				nodes := testutil.MockNodes[0].DeepCopy()
				nodes.Finalizers = []string{} // No finalizer initially

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*v1beta1.RAGEngine)
						*dep = *ragengine
					}).Return(nil)

				// ensureFinalizer calls
				c.On("Patch", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything, mock.Anything).Return(nil)

				// syncControllerRevision calls
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), "test-revision"))
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)

				// addRAGEngine calls
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*karpenterv1.NodeClaim)
						*dep = *nodeClaim
					}).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*corev1.Node)
						*dep = *nodes
					}).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *deployment
					}).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *deployment
					}).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *deployment
					}).Return(nil)
			},
			ragengine:     mockRAGEngineDistributedModel0Node,
			expectedError: nil,
			expectRequeue: false,
		},
		"Successfully create new RAGEngine without compute resources": {
			callMocks: func(c *testutil.MockClient) {
				ragengine := testutil.MockRAGEngineWithNoComputeResource.DeepCopy()
				ragengine.Finalizers = []string{} // No finalizer initially

				deployment := testutil.MockDeploymentUpdated.DeepCopy()
				deployment.Finalizers = []string{} // No finalizer initially

				nodeClaim := testutil.MockNodeClaim.DeepCopy()
				nodeClaim.Status.Conditions = []status.Condition{
					{
						Type:   string(apis.ConditionReady),
						Status: v1.ConditionTrue,
					},
				}
				nodeClaim.Finalizers = []string{} // No finalizer initially

				nodes := testutil.MockNodes[0].DeepCopy()
				nodes.Finalizers = []string{} // No finalizer initially

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*v1beta1.RAGEngine)
						*dep = *ragengine
					}).Return(nil)

				// ensureFinalizer calls
				c.On("Patch", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything, mock.Anything).Return(nil)

				// syncControllerRevision calls
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), "test-revision"))
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)

				// addRAGEngine calls
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Once()
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *deployment
					}).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			ragengine:     testutil.MockRAGEngineWithNoComputeResource,
			expectedError: nil,
			expectRequeue: false,
		},
		"Successfully create new RAGEngine without inference service": {
			callMocks: func(c *testutil.MockClient) {
				ragengine := testutil.MockRAGEngineWithNoInferenceService.DeepCopy()
				ragengine.Finalizers = []string{} // No finalizer initially

				deployment := testutil.MockDeploymentUpdated.DeepCopy()
				deployment.Finalizers = []string{} // No finalizer initially

				nodeClaim := testutil.MockNodeClaim.DeepCopy()
				nodeClaim.Status.Conditions = []status.Condition{
					{
						Type:   string(apis.ConditionReady),
						Status: v1.ConditionTrue,
					},
				}
				nodeClaim.Finalizers = []string{} // No finalizer initially

				nodes := testutil.MockNodes[0].DeepCopy()
				nodes.Finalizers = []string{} // No finalizer initially

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*v1beta1.RAGEngine)
						*dep = *ragengine
					}).Return(nil)

				// ensureFinalizer calls
				c.On("Patch", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything, mock.Anything).Return(nil)

				// syncControllerRevision calls
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), "test-revision"))
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)

				// addRAGEngine calls
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*karpenterv1.NodeClaim)
						*dep = *nodeClaim
					}).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*corev1.Node)
						*dep = *nodes
					}).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Once()
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *deployment
					}).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			ragengine:     testutil.MockRAGEngineWithNoInferenceService,
			expectedError: nil,
			expectRequeue: false,
		},
		"Successfully create new RAGEngine without compute or inference service": {
			callMocks: func(c *testutil.MockClient) {
				ragengine := testutil.MockRAGEngineWithNoComputeResourceAndInferenceService.DeepCopy()
				ragengine.Finalizers = []string{} // No finalizer initially

				deployment := testutil.MockDeploymentUpdated.DeepCopy()
				deployment.Finalizers = []string{} // No finalizer initially

				nodeClaim := testutil.MockNodeClaim.DeepCopy()
				nodeClaim.Status.Conditions = []status.Condition{
					{
						Type:   string(apis.ConditionReady),
						Status: v1.ConditionTrue,
					},
				}
				nodeClaim.Finalizers = []string{} // No finalizer initially

				nodes := testutil.MockNodes[0].DeepCopy()
				nodes.Finalizers = []string{} // No finalizer initially

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*v1beta1.RAGEngine)
						*dep = *ragengine
					}).Return(nil)

				// ensureFinalizer calls
				c.On("Patch", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything, mock.Anything).Return(nil)

				// syncControllerRevision calls
				c.On("List", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevisionList{}), mock.Anything, mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).
					Return(apierrors.NewNotFound(appsv1.Resource("ControllerRevision"), "test-revision"))
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.ControllerRevision{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)

				// addRAGEngine calls
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(testutil.NotFoundError()).Once()
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Service{}), mock.Anything).Return(nil)
				c.On("Get", mock.Anything, mock.Anything, mock.IsType(&appsv1.Deployment{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*appsv1.Deployment)
						*dep = *deployment
					}).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&appsv1.Deployment{}), mock.Anything).Return(nil)
			},
			ragengine:     testutil.MockRAGEngineWithNoComputeResourceAndInferenceService,
			expectedError: nil,
			expectRequeue: false,
		},
		"RAGEngine with deletion timestamp - should call deleteRAGEngine": {
			callMocks: func(c *testutil.MockClient) {
				ragengine := testutil.MockRAGEngineDistributedModel.DeepCopy()
				ragengine.DeletionTimestamp = &v1.Time{Time: time.Now()}
				ragengine.Finalizers = []string{consts.RAGEngineFinalizer}

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Run(func(args mock.Arguments) {
						dep := args.Get(2).(*v1beta1.RAGEngine)
						*dep = *ragengine
					}).Return(nil)

				// deleteRAGEngine calls
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			ragengine:     testutil.MockRAGEngine,
			expectedError: nil,
			expectRequeue: false,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-ragengine",
					Namespace: "test-namespace",
				},
			}

			result, err := reconciler.Reconcile(ctx, req)

			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}

			if tc.expectRequeue {
				assert.Check(t, result.RequeueAfter > 0, "Expected requeue")
			} else {
				assert.Check(t, result.RequeueAfter == 0, "Not expected to requeue")
			}
		})
	}
}

func TestEnsureFinalizer(t *testing.T) {
	testcases := map[string]struct {
		callMocks          func(c *testutil.MockClient)
		ragengine          *v1beta1.RAGEngine
		expectedError      error
		shouldAddFinalizer bool
	}{
		"Finalizer already exists - no patch needed": {
			callMocks: func(c *testutil.MockClient) {
				// No patch call expected
			},
			ragengine: &v1beta1.RAGEngine{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{consts.RAGEngineFinalizer},
				},
			},
			expectedError:      nil,
			shouldAddFinalizer: false,
		},
		"Finalizer does not exist - should add it": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Patch", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything, mock.Anything).Return(nil)
			},
			ragengine: &v1beta1.RAGEngine{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			expectedError:      nil,
			shouldAddFinalizer: true,
		},
		"Patch fails when adding finalizer": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Patch", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything, mock.Anything).
					Return(errors.New("patch failed"))
			},
			ragengine: &v1beta1.RAGEngine{
				ObjectMeta: v1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			expectedError:      errors.New("patch failed"),
			shouldAddFinalizer: true,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			err := reconciler.ensureFinalizer(ctx, tc.ragengine)

			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}

			if tc.shouldAddFinalizer {
				mockClient.AssertCalled(t, "Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			}
		})
	}
}

func TestDeleteRAGEngine(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		expectedError error
	}{
		"Successfully delete RAGEngine": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
		"Status update fails": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).
					Return(errors.New("status update failed"))
			},
			expectedError: errors.New("status update failed"),
		},
		"Garbage collection fails": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).
					Return(errors.New("failed to list nodeclaims"))
			},
			expectedError: errors.New("failed to list nodeclaims"),
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()
			ragengine := testutil.MockRAGEngine.DeepCopy()
			ragengine.Finalizers = []string{consts.RAGEngineFinalizer}

			result, err := reconciler.deleteRAGEngine(ctx, ragengine)

			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
				assert.Check(t, result.RequeueAfter == 0, "Not expected to requeue")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestComputeHash(t *testing.T) {
	testcases := map[string]struct {
		ragengine1    *v1beta1.RAGEngine
		ragengine2    *v1beta1.RAGEngine
		shouldBeEqual bool
	}{
		"Same spec should produce same hash": {
			ragengine1: &v1beta1.RAGEngine{
				Spec: &v1beta1.RAGEngineSpec{
					Compute: &v1beta1.ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
						Count:        &[]int{1}[0],
					},
				},
			},
			ragengine2: &v1beta1.RAGEngine{
				Spec: &v1beta1.RAGEngineSpec{
					Compute: &v1beta1.ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
						Count:        &[]int{1}[0],
					},
				},
			},
			shouldBeEqual: true,
		},
		"Different spec should produce different hash": {
			ragengine1: &v1beta1.RAGEngine{
				Spec: &v1beta1.RAGEngineSpec{
					Compute: &v1beta1.ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
						Count:        &[]int{1}[0],
					},
				},
			},
			ragengine2: &v1beta1.RAGEngine{
				Spec: &v1beta1.RAGEngineSpec{
					Compute: &v1beta1.ResourceSpec{
						InstanceType: "Standard_NC96ads_A100_v4",
						Count:        &[]int{1}[0],
					},
				},
			},
			shouldBeEqual: false,
		},
		"Different count should produce different hash": {
			ragengine1: &v1beta1.RAGEngine{
				Spec: &v1beta1.RAGEngineSpec{
					Compute: &v1beta1.ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
						Count:        &[]int{1}[0],
					},
				},
			},
			ragengine2: &v1beta1.RAGEngine{
				Spec: &v1beta1.RAGEngineSpec{
					Compute: &v1beta1.ResourceSpec{
						InstanceType: "Standard_NV36ads_A10_v5",
						Count:        &[]int{2}[0],
					},
				},
			},
			shouldBeEqual: false,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			hash1 := computeHash(tc.ragengine1)
			hash2 := computeHash(tc.ragengine2)

			assert.Check(t, hash1 != "", "Hash should not be empty")
			assert.Check(t, hash2 != "", "Hash should not be empty")
			assert.Check(t, len(hash1) == 64, "Hash should be 64 characters (SHA256)")
			assert.Check(t, len(hash2) == 64, "Hash should be 64 characters (SHA256)")

			if tc.shouldBeEqual {
				assert.Equal(t, hash1, hash2, "Hashes should be equal")
			} else {
				assert.Check(t, hash1 != hash2, "Hashes should be different")
			}
		})
	}
}

func TestEnsureNodePlugins(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *testutil.MockClient)
		expectedError error
		node          *corev1.Node
		setupMocks    func(c *testutil.MockClient, node *corev1.Node)
	}{
		"Node plugin already installed": {
			callMocks: func(c *testutil.MockClient) {
				// Node already has the nvidia plugin
			},
			expectedError: nil,
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						testutil.LabelKeyNvidia: testutil.LabelValueNvidia,
					},
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						testutil.CapacityNvidiaGPU: resource.MustParse("1"),
					},
				},
			},
			setupMocks: func(c *testutil.MockClient, node *corev1.Node) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						n := args.Get(2).(*corev1.Node)
						*n = *node
					}).Return(nil)
			},
		},
		"Node plugin needs to be installed": {
			callMocks: func(c *testutil.MockClient) {
				// First call - node without plugin
				// Second call - node with plugin installed
				nodeWithoutPlugin := &corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "test-node",
					},
				}
				nodeWithPlugin := &corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							testutil.LabelKeyNvidia: testutil.LabelValueNvidia,
						},
					},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							testutil.CapacityNvidiaGPU: resource.MustParse("1"),
						},
					},
				}

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						n := args.Get(2).(*corev1.Node)
						*n = *nodeWithoutPlugin
					}).Return(nil).Once()

				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						n := args.Get(2).(*corev1.Node)
						*n = *nodeWithPlugin
					}).Return(nil)
			},
			expectedError: nil,
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-node",
				},
			},
		},
		"Node get fails": {
			callMocks: func(c *testutil.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Return(errors.New("failed to get node"))
			},
			expectedError: errors.New("failed to get node"),
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-node",
				},
			},
		},
		"Node update fails with NotFound error": {
			callMocks: func(c *testutil.MockClient) {
				nodeWithoutPlugin := &corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "test-node",
					},
				}

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).
					Run(func(args mock.Arguments) {
						n := args.Get(2).(*corev1.Node)
						*n = *nodeWithoutPlugin
					}).Return(nil)

				c.On("Update", mock.IsType(context.Background()), mock.IsType(&corev1.Node{}), mock.Anything).
					Return(apierrors.NewNotFound(corev1.Resource("Node"), "test-node"))

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1beta1.RAGEngine{}), mock.Anything).Return(nil)
			},
			expectedError: apierrors.NewNotFound(corev1.Resource("Node"), "test-node"),
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-node",
				},
			},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := testutil.NewClient()
			if tc.setupMocks != nil {
				tc.setupMocks(mockClient, tc.node)
			}
			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: testutil.NewTestScheme(),
			}
			ctx := context.Background()

			ragengine := testutil.MockRAGEngine.DeepCopy()

			err := reconciler.ensureNodePlugins(ctx, ragengine, tc.node)

			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error %v", err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestNewRAGEngineReconciler(t *testing.T) {
	mockClient := testutil.NewClient()
	scheme := testutil.NewTestScheme()
	log := klog.NewKlogr()
	recorder := &record.FakeRecorder{}

	reconciler := NewRAGEngineReconciler(mockClient, scheme, log, recorder)

	assert.Check(t, reconciler != nil, "Reconciler should not be nil")
	assert.Check(t, reconciler.Client == mockClient, "Client should be set correctly")
	assert.Check(t, reconciler.Scheme == scheme, "Scheme should be set correctly")
	assert.Check(t, reconciler.Log == log, "Log should be set correctly")
	assert.Check(t, reconciler.Recorder == recorder, "Recorder should be set correctly")
}
