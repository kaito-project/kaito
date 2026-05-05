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

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	"github.com/awslabs/operatorpkg/status"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/apis"
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
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("failed to create nodeClaim"))
			},
			expectedError: errors.New("failed to create nodeClaim"),
		},
		"A nodeClaim is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&azurev1beta1.AKSNodeClass{}), mock.Anything).Return(nil)
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
	testCases := []struct {
		name                     string
		cloudProvider            string
		workspace                *kaitov1beta1.Workspace
		useOptions               bool
		options                  ManifestOptions
		expectedNodeClassKind    string
		expectedNodeClassGroup   string
		expectedImageFamily      string
		expectImageFamilyPresent bool
	}{
		{
			name:                     "azure default manifest",
			cloudProvider:            consts.AzureCloudName,
			workspace:                test.MockWorkspaceWithPreset,
			useOptions:               false,
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
		{
			name:                     "azure default node image family is set via startup parameter",
			cloudProvider:            consts.AzureCloudName,
			workspace:                test.MockWorkspaceWithPreset.DeepCopy(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "AzureLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "azurelinux",
			expectImageFamilyPresent: true,
		},
		{
			name:          "workspace annotation overrides startup parameter when annotation is supported",
			cloudProvider: consts.AzureCloudName,
			workspace: func() *kaitov1beta1.Workspace {
				w := test.MockWorkspaceWithPreset.DeepCopy()
				w.Annotations = map[string]string{kaitov1beta1.AnnotationNodeImageFamily: "Ubuntu"}
				return w
			}(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "AzureLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
		{
			name:          "unsupported workspace annotation is ignored when startup parameter is supported",
			cloudProvider: consts.AzureCloudName,
			workspace: func() *kaitov1beta1.Workspace {
				w := test.MockWorkspaceWithPreset.DeepCopy()
				w.Annotations = map[string]string{kaitov1beta1.AnnotationNodeImageFamily: "CustomLinux"}
				return w
			}(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "AzureLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "azurelinux",
			expectImageFamilyPresent: true,
		},
		{
			name:          "unsupported startup parameter and unsupported workspace annotation are both ignored",
			cloudProvider: consts.AzureCloudName,
			workspace: func() *kaitov1beta1.Workspace {
				w := test.MockWorkspaceWithPreset.DeepCopy()
				w.Annotations = map[string]string{kaitov1beta1.AnnotationNodeImageFamily: "CustomLinux"}
				return w
			}(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: "CustomLinux"},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectImageFamilyPresent: false,
		},
		{
			name:                     "image family defaults to ubuntu when startup parameter is empty and workspace annotation missing",
			cloudProvider:            consts.AzureCloudName,
			workspace:                test.MockWorkspaceWithPreset.DeepCopy(),
			useOptions:               true,
			options:                  ManifestOptions{DefaultNodeImageFamily: ""},
			expectedNodeClassKind:    "KaitoNodeClass",
			expectedNodeClassGroup:   "kaito.sh",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
		{
			name:                     "aws default manifest",
			cloudProvider:            consts.AWSCloudName,
			workspace:                test.MockWorkspaceWithPreset,
			useOptions:               false,
			expectedNodeClassKind:    "EC2NodeClass",
			expectedNodeClassGroup:   "karpenter.k8s.aws",
			expectedImageFamily:      "ubuntu",
			expectImageFamilyPresent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tc.cloudProvider)

			workspace := tc.workspace.DeepCopy()
			if tc.name == "image family defaults to ubuntu when startup parameter is empty and workspace annotation missing" {
				workspace.Annotations = nil
			}

			var nodeClaim *karpenterv1.NodeClaim
			if tc.useOptions {
				nodeClaim = GenerateNodeClaimManifestWithOptions("0", workspace, tc.options)
			} else {
				nodeClaim = GenerateNodeClaimManifest("0", workspace)
			}

			assert.Check(t, nodeClaim != nil, "NodeClaim must not be nil")
			assert.Equal(t, nodeClaim.Namespace, workspace.Namespace, "NodeClaim must have same namespace as workspace")
			assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceName], workspace.Name, "label must have same workspace name as workspace")
			assert.Equal(t, nodeClaim.Labels[kaitov1beta1.LabelWorkspaceNamespace], workspace.Namespace, "label must have same workspace namespace as workspace")
			assert.Equal(t, nodeClaim.Labels[consts.LabelNodePool], consts.KaitoNodePoolName, "label must have same labels as workspace label selector")
			assert.Equal(t, nodeClaim.Annotations[karpenterv1.DoNotDisruptAnnotationKey], "true", "label must have do not disrupt annotation")
			assert.Equal(t, len(nodeClaim.Spec.Requirements), 4, " NodeClaim must have 4 NodeSelector Requirements")
			assert.Equal(t, nodeClaim.Spec.Requirements[1].Values[0], workspace.Resource.InstanceType, "NodeClaim must have same instance type as workspace")
			assert.Equal(t, nodeClaim.Spec.Requirements[2].Key, corev1.LabelOSStable, "NodeClaim must have OS label")
			assert.Check(t, nodeClaim.Spec.NodeClassRef != nil, "NodeClaim must have NodeClassRef")
			assert.Equal(t, nodeClaim.Spec.NodeClassRef.Kind, tc.expectedNodeClassKind, "NodeClaim must have expected nodeclass kind")
			assert.Equal(t, nodeClaim.Spec.NodeClassRef.Group, tc.expectedNodeClassGroup, "NodeClaim must have expected nodeclass group")

			imageFamily, found := nodeClaim.Annotations[kaitov1beta1.AnnotationNodeImageFamily]
			if tc.expectImageFamilyPresent {
				assert.Check(t, found, "NodeClaim should contain node image family annotation")
				assert.Equal(t, imageFamily, tc.expectedImageFamily, "NodeClaim image family annotation mismatch")
			} else {
				assert.Check(t, !found, "NodeClaim should not contain node image family annotation")
			}
		})
	}
}
