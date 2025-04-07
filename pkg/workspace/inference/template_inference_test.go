// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	v1 "k8s.io/api/apps/v1"

	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestCreateTemplateInference(t *testing.T) {
	testcases := map[string]struct {
		callMocks     func(c *test.MockClient)
		expectedError error
	}{
		"Fail to create template inference because deployment creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(errors.New("Failed to create resource"))
			},
			expectedError: errors.New("Failed to create resource"),
		},
		"Successfully creates template inference because deployment already exists": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(test.IsAlreadyExistsError())
			},
			expectedError: nil,
		},
		"Successfully creates template inference by creating a new deployment": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1.Deployment{}), mock.Anything).Return(nil)
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			obj, err := CreateTemplateInference(context.Background(), test.MockWorkspaceWithInferenceTemplate, mockClient)
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
