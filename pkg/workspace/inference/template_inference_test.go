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

package inference

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	v1 "k8s.io/api/apps/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateTemplateInference(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
		description   string
	}{
		"Fail to create template inference because deployment creation fails": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded returns false (no scaling needed)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Return(test.NotFoundError())
				// CreateResource fails
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(errors.New("Failed to create resource"))
			},
			expectedError: errors.New("Failed to create resource"),
			description:   "Should fail when deployment creation fails",
		},
		"Successfully creates template inference because deployment already exists": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded returns false (no scaling needed)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Return(test.NotFoundError())
				// CreateResource succeeds but deployment already exists
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(test.IsAlreadyExistsError())
			},
			expectedError: nil,
			description:   "Should succeed when deployment already exists",
		},
		"Successfully creates template inference by creating a new deployment": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded returns false (no scaling needed)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Return(test.NotFoundError())
				// CreateResource succeeds
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			description:   "Should succeed when creating new deployment",
		},
		"Successfully scales existing deployment when target node count differs": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded finds existing deployment with different replicas
				existingDeployment := &v1.Deployment{
					Spec: v1.DeploymentSpec{
						Replicas: func() *int32 { i := int32(1); return &i }(),
					},
				}
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Run(func(args mock.Arguments) {
					dep := args.Get(2).(*v1.Deployment)
					*dep = *existingDeployment
				}).Return(nil)
				// Update is called to scale the deployment
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			description:   "Should succeed when scaling existing deployment",
		},
		"Fails to scale existing deployment due to update error": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded finds existing deployment
				existingDeployment := &v1.Deployment{
					Spec: v1.DeploymentSpec{
						Replicas: func() *int32 { i := int32(1); return &i }(),
					},
				}
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Run(func(args mock.Arguments) {
					dep := args.Get(2).(*v1.Deployment)
					*dep = *existingDeployment
				}).Return(nil)
				// Update fails
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(errors.New("Failed to update deployment"))
			},
			expectedError: errors.New("Failed to update deployment"),
			description:   "Should fail when deployment update fails during scaling",
		},
		"Successfully handles deployment with matching replicas (no scaling needed)": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded finds existing deployment with matching replicas
				existingDeployment := &v1.Deployment{
					Spec: v1.DeploymentSpec{
						Replicas: func() *int32 { i := int32(2); return &i }(), // matches TargetNodeCount
					},
				}
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Run(func(args mock.Arguments) {
					dep := args.Get(2).(*v1.Deployment)
					*dep = *existingDeployment
				}).Return(nil)
				// No update is called since replicas match
				// CreateResource is called since scaling returns false
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(test.IsAlreadyExistsError())
			},
			expectedError: nil,
			description:   "Should succeed when deployment exists with correct replicas",
		},
		"Fails when ScaleDeploymentIfNeeded encounters get error": {
			callMocks: func(c *test.MockClient) {
				// ScaleDeploymentIfNeeded fails to get deployment (not NotFound error)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Return(errors.New("Failed to get deployment"))
			},
			expectedError: errors.New("Failed to get deployment"),
			description:   "Should fail when get deployment fails with non-NotFound error",
		},
		"Successfully handles workspace without inference status": {
			callMocks: func(c *test.MockClient) {
				// When workspace.Status.Inference is nil, ScaleDeploymentIfNeeded returns false immediately
				// So CreateResource is called
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
			description:   "Should succeed when workspace has no inference status",
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			// Create a workspace with inference status for scaling tests
			workspace := test.MockWorkspaceWithInferenceTemplate

			// Only set inference status for tests that need it (not for the "without inference status" test)
			if k != "Successfully handles workspace without inference status" {
				workspace.Status.Inference = &kaitov1beta1.InferenceStatus{
					TargetNodeCount: int32(2),
				}
			} else {
				workspace.Status.Inference = nil
			}

			obj, err := CreateTemplateInference(context.Background(), workspace, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
				assert.Check(t, obj != nil, "Return object should not be nil")

				deploymentObj, ok := obj.(*v1.Deployment)
				assert.Check(t, ok, "Returned object should be of type *v1.Deployment")
				assert.Check(t, deploymentObj != nil, "Returned object should not be nil")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}

			// Verify all mock expectations were met
			mockClient.AssertExpectations(t)
		})
	}
}
