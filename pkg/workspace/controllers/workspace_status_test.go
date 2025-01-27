// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestUpdateWorkspaceStatus(t *testing.T) {
	t.Run("Should update workspace status successfully", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &WorkspaceReconciler{
			Client: mockClient,
			Scheme: test.NewTestScheme(),
		}
		ctx := context.Background()
		workspace := test.MockWorkspaceDistributedModel
		condition := metav1.Condition{
			Type:    "TestCondition",
			Status:  metav1.ConditionStatus("True"),
			Reason:  "TestReason",
			Message: "TestMessage",
		}
		workerNodes := []string{"node1", "node2"}

		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(nil)
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(nil)

		err := reconciler.updateWorkspaceStatus(ctx, &client.ObjectKey{Name: workspace.Name, Namespace: workspace.Namespace}, &condition, workerNodes)
		assert.Nil(t, err)
	})

	t.Run("Should return error when Get operation fails", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &WorkspaceReconciler{
			Client: mockClient,
			Scheme: test.NewTestScheme(),
		}
		ctx := context.Background()
		workspace := test.MockWorkspaceDistributedModel
		condition := metav1.Condition{
			Type:    "TestCondition",
			Status:  metav1.ConditionStatus("True"),
			Reason:  "TestReason",
			Message: "TestMessage",
		}
		workerNodes := []string{"node1", "node2"}

		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(errors.New("Get operation failed"))

		err := reconciler.updateWorkspaceStatus(ctx, &client.ObjectKey{Name: workspace.Name, Namespace: workspace.Namespace}, &condition, workerNodes)
		assert.NotNil(t, err)
	})

	t.Run("Should return nil when workspace is not found", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &WorkspaceReconciler{
			Client: mockClient,
			Scheme: test.NewTestScheme(),
		}
		ctx := context.Background()
		workspace := test.MockWorkspaceDistributedModel
		condition := metav1.Condition{
			Type:    "TestCondition",
			Status:  metav1.ConditionStatus("True"),
			Reason:  "TestReason",
			Message: "TestMessage",
		}
		workerNodes := []string{"node1", "node2"}

		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(apierrors.NewNotFound(schema.GroupResource{}, "workspace"))

		err := reconciler.updateWorkspaceStatus(ctx, &client.ObjectKey{Name: workspace.Name, Namespace: workspace.Namespace}, &condition, workerNodes)
		assert.Nil(t, err)
	})
}

func TestUpdateStatusConditionIfNotMatch(t *testing.T) {
	t.Run("Should not update when condition matches", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &WorkspaceReconciler{
			Client: mockClient,
			Scheme: test.NewTestScheme(),
		}
		ctx := context.Background()
		workspace := test.MockWorkspaceDistributedModel
		conditionType := kaitov1alpha1.ConditionType("TestCondition")
		conditionStatus := metav1.ConditionStatus("True")
		conditionReason := "TestReason"
		conditionMessage := "TestMessage"

		workspace.Status.Conditions = []metav1.Condition{
			{
				Type:    string(conditionType),
				Status:  conditionStatus,
				Reason:  conditionReason,
				Message: conditionMessage,
			},
		}

		err := reconciler.updateStatusConditionIfNotMatch(ctx, workspace, conditionType, conditionStatus, conditionReason, conditionMessage)
		assert.Nil(t, err)
	})

	t.Run("Should update when condition does not match", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &WorkspaceReconciler{
			Client: mockClient,
			Scheme: test.NewTestScheme(),
		}
		ctx := context.Background()
		workspace := test.MockWorkspaceDistributedModel
		conditionType := kaitov1alpha1.ConditionType("TestCondition")
		conditionStatus := metav1.ConditionStatus("True")
		conditionReason := "TestReason"
		conditionMessage := "TestMessage"

		workspace.Status.Conditions = []metav1.Condition{
			{
				Type:    string(conditionType),
				Status:  conditionStatus,
				Reason:  conditionReason,
				Message: "DifferentMessage",
			},
		}
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(nil)
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(nil)

		err := reconciler.updateStatusConditionIfNotMatch(ctx, workspace, conditionType, conditionStatus, conditionReason, conditionMessage)
		assert.Nil(t, err)
	})

	t.Run("Should update when condition is not found", func(t *testing.T) {
		mockClient := test.NewClient()
		reconciler := &WorkspaceReconciler{
			Client: mockClient,
			Scheme: test.NewTestScheme(),
		}
		ctx := context.Background()
		workspace := test.MockWorkspaceDistributedModel
		conditionType := kaitov1alpha1.ConditionType("TestCondition")
		conditionStatus := metav1.ConditionStatus("True")
		conditionReason := "TestReason"
		conditionMessage := "TestMessage"

		workspace.Status.Conditions = []metav1.Condition{
			{
				Type:    "DifferentCondition",
				Status:  conditionStatus,
				Reason:  conditionReason,
				Message: conditionMessage,
			},
		}
		mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(nil)
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()), mock.IsType(&kaitov1alpha1.Workspace{}), mock.Anything).Return(nil)

		err := reconciler.updateStatusConditionIfNotMatch(ctx, workspace, conditionType, conditionStatus, conditionReason, conditionMessage)
		assert.Nil(t, err)
	})
}
