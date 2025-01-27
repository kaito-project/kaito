// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package machine

import (
	"context"
	"errors"
	"testing"

	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"

	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/stretchr/testify/mock"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreateMachine(t *testing.T) {
	testcases := map[string]struct {
		callMocks         func(c *test.MockClient)
		machineConditions apis.Conditions
		expectedError     error
	}{
		"Machine creation fails": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(errors.New("failed to create machine"))
			},
			expectedError: errors.New("failed to create machine"),
		},
		"Machine creation fails because SKU is not available": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
			},
			machineConditions: apis.Conditions{
				{
					Type:    v1alpha5.MachineLaunched,
					Status:  corev1.ConditionFalse,
					Message: consts.ErrorInstanceTypesUnavailable,
				},
			},
			expectedError: errors.New(consts.ErrorInstanceTypesUnavailable),
		},
		"A machine is successfully created": {
			callMocks: func(c *test.MockClient) {
				c.On("Create", mock.IsType(context.Background()), mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
			},
			machineConditions: apis.Conditions{
				{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionTrue,
				},
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockMachine := &test.MockMachine
			mockMachine.Status.Conditions = tc.machineConditions

			err := CreateMachine(context.Background(), mockMachine, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestWaitForPendingMachines(t *testing.T) {
	testcases := map[string]struct {
		callMocks         func(c *test.MockClient)
		machineConditions apis.Conditions
		expectedError     error
	}{
		"Fail to list machines because associated machines cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(errors.New("failed to retrieve machines"))
			},
			expectedError: errors.New("failed to retrieve machines"),
		},
		"Fail to list machines because machine status cannot be retrieved": {
			callMocks: func(c *test.MockClient) {
				machineList := test.MockMachineList
				relevantMap := c.CreateMapWithType(machineList)
				c.CreateOrUpdateObjectInMap(&test.MockMachine)

				//insert machine objects into the map
				for _, obj := range test.MockMachineList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(errors.New("fail to get machine"))
			},
			machineConditions: apis.Conditions{
				{
					Type:   v1alpha5.MachineInitialized,
					Status: corev1.ConditionFalse,
				},
			},
			expectedError: errors.New("fail to get machine"),
		},
		"Successfully waits for all pending machines": {
			callMocks: func(c *test.MockClient) {
				machineList := test.MockMachineList
				relevantMap := c.CreateMapWithType(machineList)
				c.CreateOrUpdateObjectInMap(&test.MockMachine)

				//insert machine objects into the map
				for _, obj := range test.MockMachineList.Items {
					m := obj
					objKey := client.ObjectKeyFromObject(&m)

					relevantMap[objKey] = &m
				}

				c.On("List", mock.IsType(context.Background()), mock.IsType(&v1alpha5.MachineList{}), mock.Anything).Return(nil)
				c.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&v1alpha5.Machine{}), mock.Anything).Return(nil)
			},
			machineConditions: apis.Conditions{
				{
					Type:   apis.ConditionReady,
					Status: corev1.ConditionTrue,
				},
			},
			expectedError: nil,
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			mockClient := test.NewClient()
			tc.callMocks(mockClient)

			mockMachine := &v1alpha5.Machine{}

			mockClient.UpdateCb = func(key types.NamespacedName) {
				mockClient.GetObjectFromMap(mockMachine, key)
				mockMachine.Status.Conditions = tc.machineConditions
				mockClient.CreateOrUpdateObjectInMap(mockMachine)
			}

			err := WaitForPendingMachines(context.Background(), test.MockWorkspaceWithPreset, mockClient)
			if tc.expectedError == nil {
				assert.Check(t, err == nil, "Not expected to return error")
			} else {
				assert.Equal(t, tc.expectedError.Error(), err.Error())
			}
		})
	}
}

func TestGenerateMachineManifest(t *testing.T) {
	t.Run("Should generate a machine object from the given workspace", func(t *testing.T) {
		mockWorkspace := test.MockWorkspaceWithPreset

		machine := GenerateMachineManifest(context.Background(), "0", mockWorkspace)

		assert.Check(t, machine != nil, "Machine must not be nil")
		assert.Equal(t, machine.Namespace, mockWorkspace.Namespace, "Machine must have same namespace as workspace")
	})
}
