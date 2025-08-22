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

package nodeclaim

import (
	"context"
	"errors"
	"testing"

	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	awsv1beta1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1beta1"
	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateNodeClaim(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"NodeClaim creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("failed to create nodeClaim"))
			},
			expectedError: errors.New("failed to create nodeClaim"),
		},
		"A nodeClaim is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockNodeClaim := &test.MockNodeClaim
			t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
			err := CreateNodeClaim(context.Background(), mockNodeClaim, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestWaitForPendingNodeClaims(t *testing.T) {
	testcases := map[string]struct {
		callMocks           func(c *test.MockClient)
		nodeClaimConditions []status.Condition
		expectedError       error
	}{
		"Fail to list nodeClaims because associated nodeClaims cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("failed to retrieve nodeClaims"))
			},
			expectedError: errors.New("failed to retrieve nodeClaims"),
		},
		"Fail to list nodeClaims because nodeClaim status cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&test.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range test.MockNodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("fail to get nodeClaim"))
			},
			nodeClaimConditions: []status.Condition{
				{
					Type:   karpenterv1.ConditionTypeInitialized,
					Status: metav1.ConditionFalse,
				},
			},
			expectedError: errors.New("fail to get nodeClaim"),
		},
		"Successfully waits for all pending nodeClaims": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				c.CreateOrUpdateObjectInMap(&test.MockNodeClaim)

				//insert nodeClaim objects into the map
				for _, obj := range test.MockNodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			nodeClaimConditions: []status.Condition{
				{
					Type:   string(apis.ConditionReady),
					Status: metav1.ConditionTrue,
				},
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockNodeClaim := &karpenterv1.NodeClaim{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockNodeClaim, key)
				mockNodeClaim.Status.Conditions = tc.nodeClaimConditions
				mockClient.CreateOrUpdateObjectInMap(mockNodeClaim)
			}

			err := WaitForPendingNodeClaims(context.Background(), test.MockWorkspaceWithPreset, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestGenerateNodeClaimManifest(t *testing.T) {
	t.Run("Should generate a nodeClaim object from the given workspace when cloud provider set to azure", func(t *testing.T) {
		mockWorkspace := test.MockWorkspaceWithPreset
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

		nodeClaim := GenerateNodeClaimManifest("0", mockWorkspace)

		assert.Check(t, nodeClaim != nil, "NodeClaim must not be nil")
		assert.Equal(t, nodeClaim.Namespace, mockWorkspace.Namespace, "NodeClaim must have same namespace as workspace")
		assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceName], mockWorkspace.Name, "label must have same workspace name as workspace")
		assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceNamespace], mockWorkspace.Namespace, "label must have same workspace namespace as workspace")
		assert.Equal(t, nodeClaim.Labels[consts.LabelNodePool], consts.KaitoNodePoolName, "label must have same labels as workspace label selector")
		assert.Equal(t, nodeClaim.Annotations[karpenterv1.DoNotDisruptAnnotationKey], "true", "label must have do not disrupt annotation")
		assert.Equal(t, len(nodeClaim.Spec.Requirements), 4, " NodeClaim must have 4 NodeSelector Requirements")
		assert.Equal(t, nodeClaim.Spec.Requirements[1].NodeSelectorRequirement.Values[0], mockWorkspace.Resource.InstanceType, "NodeClaim must have same instance type as workspace")
		assert.Equal(t, nodeClaim.Spec.Requirements[2].NodeSelectorRequirement.Key, corev1.LabelOSStable, "NodeClaim must have OS label")
		assert.Check(t, nodeClaim.Spec.NodeClassRef != nil, "NodeClaim must have NodeClassRef")
		assert.Equal(t, nodeClaim.Spec.NodeClassRef.Kind, "AKSNodeClass", "NodeClaim must have 'AKSNodeClass' kind")
	})

	t.Run("Should generate a nodeClaim object from the given workspace when cloud provider set to aws", func(t *testing.T) {
		mockWorkspace := test.MockWorkspaceWithPreset
		t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)

		nodeClaim := GenerateNodeClaimManifest("0", mockWorkspace)

		assert.Check(t, nodeClaim != nil, "NodeClaim must not be nil")
		assert.Equal(t, nodeClaim.Namespace, mockWorkspace.Namespace, "NodeClaim must have same namespace as workspace")
		assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceName], mockWorkspace.Name, "label must have same workspace name as workspace")
		assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceNamespace], mockWorkspace.Namespace, "label must have same workspace namespace as workspace")
		assert.Equal(t, nodeClaim.Labels[consts.LabelNodePool], consts.KaitoNodePoolName, "label must have same labels as workspace label selector")
		assert.Equal(t, nodeClaim.Annotations[karpenterv1.DoNotDisruptAnnotationKey], "true", "label must have do not disrupt annotation")
		assert.Equal(t, len(nodeClaim.Spec.Requirements), 4, " NodeClaim must have 4 NodeSelector Requirements")
		assert.Check(t, nodeClaim.Spec.NodeClassRef != nil, "NodeClaim must have NodeClassRef")
		assert.Equal(t, nodeClaim.Spec.NodeClassRef.Kind, "EC2NodeClass", "NodeClaim must have 'EC2NodeClass' kind")
	})
}

func TestGenerateAKSNodeClassManifest(t *testing.T) {
	t.Run("Should generate a valid AKSNodeClass object with correct name and annotations", func(t *testing.T) {
		nodeClass := GenerateAKSNodeClassManifest(context.Background())

		assert.Check(t, nodeClass != nil, "AKSNodeClass must not be nil")
		assert.Equal(t, nodeClass.Name, consts.NodeClassName, "AKSNodeClass must have the correct name")
		assert.Equal(t, nodeClass.Annotations["kubernetes.io/description"], "General purpose AKSNodeClass for running Ubuntu 22.04 nodes", "AKSNodeClass must have the correct description annotation")
		assert.Equal(t, *nodeClass.Spec.ImageFamily, "Ubuntu2204", "AKSNodeClass must have the correct image family")
	})

	t.Run("Should generate a valid AKSNodeClass object with empty annotations if not provided", func(t *testing.T) {
		nodeClass := GenerateAKSNodeClassManifest(context.Background())

		assert.Check(t, nodeClass != nil, "AKSNodeClass must not be nil")
		assert.Equal(t, nodeClass.Name, consts.NodeClassName, "AKSNodeClass must have the correct name")
		assert.Check(t, nodeClass.Annotations != nil, "AKSNodeClass must have annotations")
		assert.Equal(t, *nodeClass.Spec.ImageFamily, "Ubuntu2204", "AKSNodeClass must have the correct image family")
	})

	t.Run("Should generate a valid AKSNodeClass object with correct spec", func(t *testing.T) {
		nodeClass := GenerateAKSNodeClassManifest(context.Background())

		assert.Check(t, nodeClass != nil, "AKSNodeClass must not be nil")
		assert.Equal(t, nodeClass.Name, consts.NodeClassName, "AKSNodeClass must have the correct name")
		assert.Equal(t, *nodeClass.Spec.ImageFamily, "Ubuntu2204", "AKSNodeClass must have the correct image family")
	})
}

func TestGenerateEC2NodeClassManifest(t *testing.T) {
	t.Run("Should generate a valid EC2NodeClass object with correct name and annotations", func(t *testing.T) {
		t.Setenv("CLUSTER_NAME", "test-cluster")

		nodeClass := GenerateEC2NodeClassManifest(context.Background())

		assert.Check(t, nodeClass != nil, "EC2NodeClass must not be nil")
		assert.Equal(t, nodeClass.Name, consts.NodeClassName, "EC2NodeClass must have the correct name")
		assert.Equal(t, nodeClass.Annotations["kubernetes.io/description"], "General purpose EC2NodeClass for running Amazon Linux 2 nodes", "EC2NodeClass must have the correct description annotation")
		assert.Equal(t, *nodeClass.Spec.AMIFamily, awsv1beta1.AMIFamilyAL2, "EC2NodeClass must have the correct AMI family")
		assert.Equal(t, nodeClass.Spec.Role, "KarpenterNodeRole-test-cluster", "EC2NodeClass must have the correct role")
	})

	t.Run("Should generate a valid EC2NodeClass object with correct subnet and security group selectors", func(t *testing.T) {
		t.Setenv("CLUSTER_NAME", "test-cluster")

		nodeClass := GenerateEC2NodeClassManifest(context.Background())

		assert.Check(t, nodeClass != nil, "EC2NodeClass must not be nil")
		assert.Equal(t, nodeClass.Spec.SubnetSelectorTerms[0].Tags["karpenter.sh/discovery"], "test-cluster", "EC2NodeClass must have the correct subnet selector")
		assert.Equal(t, nodeClass.Spec.SecurityGroupSelectorTerms[0].Tags["karpenter.sh/discovery"], "test-cluster", "EC2NodeClass must have the correct security group selector")
	})

	t.Run("Should handle missing CLUSTER_NAME environment variable", func(t *testing.T) {
		t.Setenv("CLUSTER_NAME", "")

		nodeClass := GenerateEC2NodeClassManifest(context.Background())

		assert.Check(t, nodeClass != nil, "EC2NodeClass must not be nil")
		assert.Equal(t, nodeClass.Spec.Role, "KarpenterNodeRole-", "EC2NodeClass must handle missing cluster name")
		assert.Equal(t, nodeClass.Spec.SubnetSelectorTerms[0].Tags["karpenter.sh/discovery"], "", "EC2NodeClass must handle missing cluster name in subnet selector")
		assert.Equal(t, nodeClass.Spec.SecurityGroupSelectorTerms[0].Tags["karpenter.sh/discovery"], "", "EC2NodeClass must handle missing cluster name in security group selector")
	})
}

func TestCreateKarpenterNodeClass(t *testing.T) {
	t.Run("Should create AKSNodeClass when cloud provider is Azure", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

		mockClient := test.NewClient()
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(nil)

		err := CreateKarpenterNodeClass(context.Background(), mockClient)
		assert.Check(t, err == nil, "Not expected to return error")
		mockClient.AssertCalled(t, "Create", mock.IsType(context.Background()), mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything)
	})

	t.Run("Should create EC2NodeClass when cloud provider is AWS", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)
		t.Setenv("CLUSTER_NAME", "test-cluster")

		mockClient := test.NewClient()
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&awsv1beta1.EC2NodeClass{}), mock.Anything).Return(nil)

		err := CreateKarpenterNodeClass(context.Background(), mockClient)
		assert.Check(t, err == nil, "Not expected to return error")
		mockClient.AssertCalled(t, "Create", mock.IsType(context.Background()), mock.IsType(&awsv1beta1.EC2NodeClass{}), mock.Anything)
	})

	t.Run("Should return error when cloud provider is unsupported", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", "unsupported")

		mockClient := test.NewClient()

		err := CreateKarpenterNodeClass(context.Background(), mockClient)
		assert.Error(t, err, "unsupported cloud provider unsupported")
	})

	t.Run("Should return error when Create call fails", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

		mockClient := test.NewClient()
		mockClient.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1alpha2.AKSNodeClass{}), mock.Anything).Return(errors.New("create failed"))

		err := CreateKarpenterNodeClass(context.Background(), mockClient)
		assert.Error(t, err, "create failed")
	})
}

func TestGetBringYourOwnNodes(t *testing.T) {
	t.Run("Should return all ready nodes when no label selector and node provisioning is disabled", func(t *testing.T) {
		// Save original feature gate value and restore after test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		readyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node2"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		notReadyNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node3"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*readyNode1, *readyNode2, *notReadyNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 2, "Expected 2 ready nodes")
		assert.Equal(t, nodes[0].Name, "node1", "Expected first node to be node1")
		assert.Equal(t, nodes[1].Name, "node2", "Expected second node to be node2")
	})

	t.Run("Should return only preferred nodes when specified and node provisioning enabled", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		preferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "preferred-node"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		nonPreferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "non-preferred-node"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*preferredNode, *nonPreferredNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{"preferred-node"},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 1, "Expected 1 preferred node")
		assert.Equal(t, nodes[0].Name, "preferred-node", "Expected preferred node")
	})

	t.Run("Should filter nodes by label selector", func(t *testing.T) {
		// Enable auto provisioning and ensure nodes aren't filtered by preferred nodes
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true // Disable provisioning so all ready nodes are returned
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		matchingNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "matching-node",
				Labels: map[string]string{"env": "production"},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*matchingNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "production"},
				},
				PreferredNodes: []string{},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 1, "Expected 1 matching node")
		assert.Equal(t, nodes[0].Name, "matching-node", "Expected matching node")
	})

	t.Run("Should return error when node list fails", func(t *testing.T) {
		mockClient := test.NewClient()

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
		}

		_, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Error(t, err, "list failed")
	})

	t.Run("Should skip not ready preferred nodes", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Create mock nodes
		notReadyPreferredNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "not-ready-preferred"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}},
			},
		}

		nodeList := &corev1.NodeList{
			Items: []corev1.Node{*notReadyPreferredNode},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{"not-ready-preferred"},
			},
		}

		nodes, err := GetBringYourOwnNodes(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodes), 0, "Expected no nodes since preferred node is not ready")
	})
}

func TestGetExistingNodeClaims(t *testing.T) {
	t.Run("Should return NodeClaims associated with workspace", func(t *testing.T) {
		mockClient := test.NewClient()

		nodeClaim1 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "nodeclaim1",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
		}
		nodeClaim2 := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "nodeclaim2",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
		}

		nodeClaimList := &karpenterv1.NodeClaimList{
			Items: []karpenterv1.NodeClaim{*nodeClaim1, *nodeClaim2},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
		}

		nodeClaims, err := GetExistingNodeClaims(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodeClaims), 2, "Expected 2 NodeClaims")
		assert.Equal(t, nodeClaims[0].Name, "nodeclaim1", "Expected first NodeClaim")
		assert.Equal(t, nodeClaims[1].Name, "nodeclaim2", "Expected second NodeClaim")
	})

	t.Run("Should return empty list when no NodeClaims found", func(t *testing.T) {
		mockClient := test.NewClient()

		nodeClaimList := &karpenterv1.NodeClaimList{
			Items: []karpenterv1.NodeClaim{},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
		}

		nodeClaims, err := GetExistingNodeClaims(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodeClaims), 0, "Expected no NodeClaims")
	})

	t.Run("Should return error when list fails", func(t *testing.T) {
		mockClient := test.NewClient()

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("list failed"))

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
		}

		_, err := GetExistingNodeClaims(context.Background(), mockClient, workspace)

		assert.Error(t, err, "failed to list NodeClaims: list failed")
	})

	t.Run("Should filter NodeClaims by workspace labels", func(t *testing.T) {
		mockClient := test.NewClient()

		// NodeClaim with matching labels
		matchingNodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "matching-nodeclaim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "default",
				},
			},
		}

		nodeClaimList := &karpenterv1.NodeClaimList{
			Items: []karpenterv1.NodeClaim{*matchingNodeClaim},
		}

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}),
			mock.MatchedBy(func(opts []client.ListOption) bool {
				// Verify that the correct options are passed (only MatchingLabels for cluster-scoped resource)
				return len(opts) == 1 // Only MatchingLabels
			})).Run(func(args mock.Arguments) {
			ncl := args.Get(1).(*karpenterv1.NodeClaimList)
			*ncl = *nodeClaimList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
		}

		nodeClaims, err := GetExistingNodeClaims(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, len(nodeClaims), 1, "Expected 1 matching NodeClaim")
		assert.Equal(t, nodeClaims[0].Name, "matching-nodeclaim", "Expected matching NodeClaim")
	})
}

func TestGetRequiredNodeClaimsCount(t *testing.T) {
	t.Run("Should return 0 when node auto provisioning is disabled", func(t *testing.T) {
		// Save original feature gate value and restore after test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
		}

		count, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, count, 0, "Expected 0 NodeClaims when auto provisioning disabled")
	})

	t.Run("Should return 1 for non-inference workload", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Mock empty node list (no BYO nodes)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
			Inference: nil, // Non-inference workload
		}

		count, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, count, 1, "Expected 1 NodeClaim for non-inference workload")
	})

	t.Run("Should return target node count for inference workload", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Mock empty node list (no BYO nodes)
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
			Inference: &kaitov1beta1.InferenceSpec{}, // Inference workload
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 3,
				},
			},
		}

		count, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, count, 3, "Expected 3 NodeClaims for inference workload")
	})

	t.Run("Should subtract available BYO nodes from target count", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Mock node list with 2 available BYO nodes
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "byo-node1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		readyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "byo-node2"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{Items: []corev1.Node{*readyNode1, *readyNode2}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{"byo-node1", "byo-node2"}, // Both nodes are preferred
			},
			Inference: &kaitov1beta1.InferenceSpec{},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 5, // Target 5 nodes
				},
			},
		}

		count, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, count, 3, "Expected 3 NodeClaims (5 target - 2 BYO nodes)")
	})

	t.Run("Should return 0 when BYO nodes meet target", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Mock node list with enough BYO nodes
		readyNode1 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "byo-node1"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}
		readyNode2 := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "byo-node2"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}

		nodeList := &corev1.NodeList{Items: []corev1.Node{*readyNode1, *readyNode2}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{"byo-node1", "byo-node2"},
			},
			Inference: &kaitov1beta1.InferenceSpec{},
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: &kaitov1beta1.InferenceStatus{
					TargetNodeCount: 2, // Target exactly matches BYO nodes
				},
			},
		}

		count, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, count, 0, "Expected 0 NodeClaims when BYO nodes meet target")
	})

	t.Run("Should propagate error from GetBringYourOwnNodes", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("list failed"))

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
		}

		_, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Error(t, err, "failed to get available BYO nodes: list failed")
	})

	t.Run("Should handle workspace without inference status", func(t *testing.T) {
		// Ensure feature gate is false (enabled provisioning)
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		mockClient := test.NewClient()

		// Mock empty node list
		nodeList := &corev1.NodeList{Items: []corev1.Node{}}
		mockClient.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
			nl := args.Get(1).(*corev1.NodeList)
			*nl = *nodeList
		}).Return(nil)

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-workspace", Namespace: "default"},
			Resource: kaitov1beta1.ResourceSpec{
				LabelSelector:  &metav1.LabelSelector{},
				PreferredNodes: []string{},
			},
			Inference: &kaitov1beta1.InferenceSpec{}, // Has inference spec but no status
			Status: kaitov1beta1.WorkspaceStatus{
				Inference: nil, // No inference status
			},
		}

		count, err := GetRequiredNodeClaimsCount(context.Background(), mockClient, workspace)

		assert.Check(t, err == nil, "Not expected to return error")
		assert.Equal(t, count, 1, "Expected 1 NodeClaim when inference status is nil")
	})
}

func TestNodeIsReadyAndNotDeleting(t *testing.T) {
	t.Run("Should return true for ready node without deletion timestamp", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "ready-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == true, "Expected ready node without deletion timestamp to return true")
	})

	t.Run("Should return false for node with deletion timestamp", func(t *testing.T) {
		now := metav1.Now()
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deleting-node",
				DeletionTimestamp: &now,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node with deletion timestamp to return false")
	})

	t.Run("Should return false for not ready node", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "not-ready-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected not ready node to return false")
	})

	t.Run("Should return false for node with unknown ready status", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "unknown-ready-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionUnknown,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node with unknown ready status to return false")
	})

	t.Run("Should return false for node without ready condition", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "no-condition-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeDiskPressure,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node without ready condition to return false")
	})

	t.Run("Should return false for node with empty conditions", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "empty-conditions-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected node with empty conditions to return false")
	})

	t.Run("Should return false for both deleting and not ready node", func(t *testing.T) {
		now := metav1.Now()
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "deleting-not-ready-node",
				DeletionTimestamp: &now,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == false, "Expected deleting and not ready node to return false")
	})

	t.Run("Should return true when ready condition is among multiple conditions", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "multi-condition-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeMemoryPressure,
						Status: corev1.ConditionFalse,
					},
					{
						Type:   corev1.NodeDiskPressure,
						Status: corev1.ConditionFalse,
					},
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   corev1.NodePIDPressure,
						Status: corev1.ConditionFalse,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		assert.Check(t, result == true, "Expected ready node among multiple conditions to return true")
	})

	t.Run("Should return true when ready condition appears multiple times with mixed statuses", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "mixed-ready-conditions-node",
				DeletionTimestamp: nil,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionFalse,
					},
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}

		result := NodeIsReadyAndNotDeleting(node)
		// The function uses lo.Find which returns true if ANY condition matches, so it will find the true condition
		assert.Check(t, result == true, "Expected node with mixed ready conditions to return true (finds any true condition)")
	})
}
