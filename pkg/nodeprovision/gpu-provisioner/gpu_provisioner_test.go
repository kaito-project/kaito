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

package gpuprovisioner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/test"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

func newTestWorkspace() *kaitov1beta1.Workspace {
	count := 1
	return &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workspace",
			Namespace: "default",
		},
		Resource: kaitov1beta1.ResourceSpec{
			Count:        &count,
			InstanceType: "Standard_NC6s_v3",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"apps": "test",
				},
			},
		},
		Status: kaitov1beta1.WorkspaceStatus{
			TargetNodeCount: 1,
		},
	}
}

func TestAzureGPUProvisionerImplementsInterface(t *testing.T) {
	mockClient := test.NewClient()
	expectations := utils.NewControllerExpectations()
	ncm := resource.NewNodeClaimManager(mockClient, nil, expectations)
	nm := resource.NewNodeManager(mockClient)

	var _ nodeprovision.NodeProvisioner = NewAzureGPUProvisioner(ncm, nm)
}

func TestAzureGPUProvisionerEnableDriftRemediationIsNoop(t *testing.T) {
	mockClient := test.NewClient()
	expectations := utils.NewControllerExpectations()
	ncm := resource.NewNodeClaimManager(mockClient, nil, expectations)
	nm := resource.NewNodeManager(mockClient)

	p := NewAzureGPUProvisioner(ncm, nm)
	err := p.EnableDriftRemediation(context.Background(), "default", "test-workspace")
	assert.NilError(t, err)
}

func TestAzureGPUProvisionerDisableDriftRemediationIsNoop(t *testing.T) {
	mockClient := test.NewClient()
	expectations := utils.NewControllerExpectations()
	ncm := resource.NewNodeClaimManager(mockClient, nil, expectations)
	nm := resource.NewNodeManager(mockClient)

	p := NewAzureGPUProvisioner(ncm, nm)
	err := p.DisableDriftRemediation(context.Background(), "default", "test-workspace")
	assert.NilError(t, err)
}

func TestAzureGPUProvisionerDeleteNodes(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Successfully deprovisions when no NodeClaims exist": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
		"Fails when NodeClaims cannot be listed": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("list error"))
			},
			expectedError: errors.New("list error"),
		},
		"Successfully deletes existing NodeClaims": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
		"Fails when NodeClaim deletion fails": {
			callMocks: func(c *test.MockClient) {
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
				c.On("Delete", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaim{}), mock.Anything).Return(errors.New("delete error"))
			},
			expectedError: errors.New("delete error"),
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			expectations := utils.NewControllerExpectations()
			ncm := resource.NewNodeClaimManager(mockClient, nil, expectations)
			nm := resource.NewNodeManager(mockClient)
			p := NewAzureGPUProvisioner(ncm, nm)

			ws := newTestWorkspace()
			err := p.DeleteNodes(context.Background(), ws)
			if tc.expectedError == nil {
				assert.NilError(t, err)
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestAzureGPUProvisionerEnsureNodesReady(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedReady bool
		expectedError bool
	}{
		"Returns not ready when NodeClaims exist but are not ready": {
			callMocks: func(c *test.MockClient) {
				// GetReadyNodes -> ListNodes (corev1.NodeList): no BYO nodes
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)

				// ListNodeClaim: 1 not-ready NodeClaim
				nodeClaimList := test.MockNodeClaimList
				relevantMap := c.CreateMapWithType(nodeClaimList)
				for _, obj := range nodeClaimList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)
					relevantMap[objKey] = &m
				}
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(nil)
			},
			expectedReady: false,
			expectedError: false,
		},
		"Returns error when ready nodes cannot be listed": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(errors.New("node list error"))
			},
			expectedReady: false,
			expectedError: true,
		},
		"Returns error when NodeClaims cannot be listed": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&corev1.NodeList{}), mock.Anything).Return(nil)
				c.On("List", mock.IsType(context.Background()), mock.IsType(&karpenterv1.NodeClaimList{}), mock.Anything).Return(errors.New("nodeclaim list error"))
			},
			expectedReady: false,
			expectedError: true,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			expectations := utils.NewControllerExpectations()
			ncm := resource.NewNodeClaimManager(mockClient, nil, expectations)
			nm := resource.NewNodeManager(mockClient)
			p := NewAzureGPUProvisioner(ncm, nm)

			ws := newTestWorkspace()
			ready, _, err := p.EnsureNodesReady(context.Background(), ws)
			if tc.expectedError {
				assert.Assert(t, err != nil)
			} else {
				assert.NilError(t, err)
				assert.Equal(t, tc.expectedReady, ready)
			}
		})
	}
}
