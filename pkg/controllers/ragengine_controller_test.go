// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	awsv1beta1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1beta1"
	"github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
)

func TestApplyRAGEngineResource(t *testing.T) {
	test.RegisterTestModel()
	testcases := map[string]struct {
		callMocks                   func(c *test.MockClient)
		karpenterFeatureGateEnabled bool
		expectedError               error
		ragengine                   v1alpha1.RAGEngine
	}{
		"Fail to apply ragengine because associated machines cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(errors.New("failed to retrieve machines"))
			},
			ragengine:     *test.MockRAGEngineDistributedModel,
			expectedError: errors.New("failed to retrieve machines"),
		},
		"Fail to apply ragengine because can't get qualified nodes": {
			callMocks: func(c *test.MockClient) {
				machineList := test.MockMachineList
				relevantMap := c.CreateMapWithType(machineList)
				c.CreateOrUpdateObjectInMap(&test.MockMachine)

				//insert machine objects into the map
				for _, obj := range machineList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("failed to list nodes"))
			},
			ragengine:     *test.MockRAGEngineDistributedModel,
			expectedError: errors.New("failed to list nodes"),
		},
		"Fail to apply ragengine because associated nodeClaim cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1beta1.NodeClaimList{}), mock.Anything).Return(errors.New("failed to retrieve nodeClaims"))

			},
			karpenterFeatureGateEnabled: true,
			ragengine:                   *test.MockRAGEngineDistributedModel,
			expectedError:               errors.New("failed to retrieve nodeClaims"),
		},
		"Fail to apply ragengine with nodeClaims because can't get qualified nodes": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&test.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range nodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1beta1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("failed to list nodes"))
			},
			karpenterFeatureGateEnabled: true,
			ragengine:                   *test.MockRAGEngineDistributedModel,
			expectedError:               errors.New("failed to list nodes"),
		},
		"Successfully apply ragengine resource with machine": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range test.MockNodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1alpha1.RAGEngine{}), mock.Anything).Return(nil)

			},
			ragengine:     *test.MockRAGEngineDistributedModel,
			expectedError: nil,
		},
		"Successfully apply ragengine resource with nodeClaim": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range nodeList.Items {
					n := obj
					objKey := client.ObjectKeyFromObject(&n)

					relevantMap[objKey] = &n
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1beta1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.NodeClaim{}), mock.Anything).Return(nil)

				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha1.RAGEngine{}), mock.Anything).Return(nil)
				c.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&v1alpha1.RAGEngine{}), mock.Anything).Return(nil)

			},
			karpenterFeatureGateEnabled: true,
			ragengine:                   *test.MockRAGEngineDistributedModel,
			expectedError:               nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockMachine := &v1alpha5.Machine{}
			mockNodeClaim := &v1beta1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockMachine, key)
				mockMachine.Status.Conditions = apis.Conditions{
					{
						Type:   apis.ConditionReady,
						Status: corev1.ConditionTrue,
					},
				}
				mockClient.CreateOrUpdateObjectInMap(mockMachine)

				mockClient.GetObjectFromMap(mockNodeClaim, key)
				mockNodeClaim.Status.Conditions = apis.Conditions{
					{
						Type:   apis.ConditionReady,
						Status: corev1.ConditionTrue,
					},
				}
				mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
			}

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			featuregates.FeatureGates[consts.FeatureFlagKarpenter] = tc.karpenterFeatureGateEnabled
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
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Fails to get qualified nodes because can't list nodes": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("Failed to list nodes"))
			},
			expectedError: errors.New("Failed to list nodes"),
		},
		"Gets all qualified nodes": {
			callMocks: func(c *test.MockClient) {
				nodeList := test.MockNodeList
				deletedNode := corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Name: "node4",
						Labels: map[string]string{
							corev1.LabelInstanceTypeStable: "Standard_NC12s_v3",
						},
						DeletionTimestamp: &v1.Time{Time: time.Now()},
					},
				}
				nodeList.Items = append(nodeList.Items, deletedNode)

				relevantMap := c.CreateMapWithType(nodeList)
				//insert node objects into the map
				for _, obj := range test.MockNodeList.Items {
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
			mockClient := test.NewClient()
			mockRAGEngine := test.MockRAGEngineDistributedModel
			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
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
	test.RegisterTestModel()
	testcases := map[string]struct {
		callMocks             func(c *test.MockClient)
		objectConditions      apis.Conditions
		ragengine             v1alpha1.RAGEngine
		karpenterFeatureGates bool
		expectedError         error
	}{
		"A machine is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
			},
			objectConditions: apis.Conditions{
				{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionTrue,
				},
			},
			ragengine:     *test.MockRAGEngineDistributedModel,
			expectedError: nil,
		},
		"An Azure nodeClaim is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1beta1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				os.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			},
			objectConditions: apis.Conditions{
				{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionTrue,
				},
			},
			ragengine:             *test.MockRAGEngineDistributedModel,
			karpenterFeatureGates: true,
			expectedError:         nil,
		},
		"An AWS nodeClaim is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&awsv1beta1.EC2NodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&awsv1beta1.EC2NodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1beta1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1beta1.NodeClaim{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.Node{}), mock.Anything).Return(nil)
				os.Setenv("CLOUD_PROVIDER", "aws")
			},
			objectConditions: apis.Conditions{
				{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionTrue,
				},
			},
			ragengine:             *test.MockRAGEngineDistributedModel,
			karpenterFeatureGates: true,
			expectedError:         nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			mockMachine := &v1alpha5.Machine{}
			mockNodeClaim := &v1beta1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockMachine, key)
				mockMachine.Status.Conditions = tc.objectConditions
				mockClient.CreateOrUpdateObjectInMap(mockMachine)

				if tc.karpenterFeatureGates {
					mockClient.GetObjectFromMap(mockNodeClaim, key)
					mockNodeClaim.Status.Conditions = tc.objectConditions
					mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
				}
			}

			tc.callMocks(mockClient)

			reconciler := &RAGEngineReconciler{
				Client: mockClient,
				Scheme: test.NewTestScheme(),
			}
			ctx := context.Background()
			featuregates.FeatureGates[consts.FeatureFlagKarpenter] = tc.karpenterFeatureGates

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
