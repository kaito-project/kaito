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
