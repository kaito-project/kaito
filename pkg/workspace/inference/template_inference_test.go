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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateTemplateInference(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Successfully scales existing deployment and returns without creating": {
			callMocks: func(c *test.MockClient) {
				// Mock Get call for ScaleDeploymentIfNeeded - deployment exists
				existingDeployment := &v1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testWorkspace",
						Namespace: "kaito",
					},
					Spec: v1.DeploymentSpec{
						Replicas: &[]int32{1}[0], // Current replicas: 1
					},
				}
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Run(func(args mock.Arguments) {
					dep := args[2].(*v1.Deployment)
					*dep = *existingDeployment
				}).Return(nil)

				// Mock Update call for scaling
				c.On("Update", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
		"Deployment exists but no scaling needed, then creates resource": {
			callMocks: func(c *test.MockClient) {
				// Mock Get call for ScaleDeploymentIfNeeded - deployment exists with matching replicas
				existingDeployment := &v1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testWorkspace",
						Namespace: "kaito",
					},
					Spec: v1.DeploymentSpec{
						Replicas: &[]int32{2}[0], // Current replicas match target (2)
					},
				}
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Run(func(args mock.Arguments) {
					dep := args[2].(*v1.Deployment)
					*dep = *existingDeployment
				}).Return(nil)

				// Mock Create call - should return AlreadyExists since deployment exists
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(apierrors.NewAlreadyExists(schema.GroupResource{Group: "apps", Resource: "deployments"}, "testWorkspace"))
			},
			expectedError: nil,
		},
		"Deployment doesn't exist, successfully creates new deployment": {
			callMocks: func(c *test.MockClient) {
				// Mock Get call for ScaleDeploymentIfNeeded - deployment doesn't exist
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "testWorkspace"))

				// Mock Create call for new deployment
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
		"Deployment doesn't exist, fails to create new deployment": {
			callMocks: func(c *test.MockClient) {
				// Mock Get call for ScaleDeploymentIfNeeded - deployment doesn't exist
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1.Deployment{}), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "testWorkspace"))

				// Mock Create call that fails
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(errors.New("Failed to create resource"))
			},
			expectedError: errors.New("Failed to create resource"),
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			// Create a workspace with inference status for testing
			workspace := test.MockWorkspaceWithInferenceTemplate.DeepCopy()
			workspace.Status.Inference = &kaitov1beta1.InferenceStatus{
				TargetNodeCount: 2, // Set target replicas to 2 for testing
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
		})
	}
}
