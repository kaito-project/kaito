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

package workspace

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestUpdateStatusConditionIfNotMatch(t *testing.T) {
	t.Run("Should skip update when condition matches current values", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesReady",
						Message: "All resources are ready",
					},
				},
			},
		}

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, workspace,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		// No client calls should be made since condition matches
		mockClient.AssertExpectations(t)
	})

	t.Run("Should update when condition status differs", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionFalse,
						Reason:  "ResourcesNotReady",
						Message: "Resources are not ready",
					},
				},
			},
		}

		// Mock the Get call for UpdateWorkspaceStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.Workspace)
			// Verify the condition was updated
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, workspace,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition reason differs", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionTrue,
						Reason:  "OldReason",
						Message: "All resources are ready",
					},
				},
			},
		}

		// Mock the Get call for UpdateWorkspaceStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, workspace,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "NewReason", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition message differs", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesReady",
						Message: "Old message",
					},
				},
			},
		}

		// Mock the Get call for UpdateWorkspaceStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, workspace,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "New message")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should add new condition when condition type doesn't exist", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{}, // Empty conditions
			},
		}

		// Mock the Get call for UpdateWorkspaceStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.Workspace)
			// Verify the condition was added
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, workspace,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should propagate error from UpdateWorkspaceStatus", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionFalse,
						Reason:  "OldReason",
						Message: "Old message",
					},
				},
			},
		}

		// Mock the Get call to return an error
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(fmt.Errorf("get error"))

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, workspace,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "NewReason", "New message")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "get error")
		mockClient.AssertExpectations(t)
	})
}

func TestUpdateWorkspaceStatus(t *testing.T) {
	t.Run("Should successfully update workspace status", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-workspace",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{},
			},
		}

		condition := &metav1.Condition{
			Type:               string(kaitov1beta1.ConditionTypeResourceStatus),
			Status:             metav1.ConditionTrue,
			Reason:             "ResourcesReady",
			Message:            "All resources are ready",
			LastTransitionTime: metav1.Now(),
		}

		// Mock the Get call
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.Workspace)
			// Verify the condition was set
			foundCondition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, foundCondition)
			assert.Equal(t, condition.Type, foundCondition.Type)
			assert.Equal(t, condition.Status, foundCondition.Status)
			assert.Equal(t, condition.Reason, foundCondition.Reason)
			assert.Equal(t, condition.Message, foundCondition.Message)
		}).Return(nil)

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
		err := UpdateWorkspaceStatus(ctx, mockClient, key, func(status *kaitov1beta1.WorkspaceStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should handle workspace not found gracefully", func(t *testing.T) {
		mockClient := test.NewClient()

		condition := &metav1.Condition{
			Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourcesReady",
			Message: "All resources are ready",
		}

		// Mock the Get call to return NotFound error
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(
			apierrors.NewNotFound(schema.GroupResource{Group: "kaito.sh", Resource: "workspaces"}, "test-workspace"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
		err := UpdateWorkspaceStatus(ctx, mockClient, key, func(status *kaitov1beta1.WorkspaceStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.NoError(t, err) // Should not return error for NotFound
		mockClient.AssertExpectations(t)
	})

	t.Run("Should return error for other Get failures", func(t *testing.T) {
		mockClient := test.NewClient()

		condition := &metav1.Condition{
			Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourcesReady",
			Message: "All resources are ready",
		}

		// Mock the Get call to return a generic error
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(fmt.Errorf("network error"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
		err := UpdateWorkspaceStatus(ctx, mockClient, key, func(status *kaitov1beta1.WorkspaceStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network error")
		mockClient.AssertExpectations(t)
	})

	t.Run("Should handle nil condition", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{},
			},
		}

		// Mock the Get call
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil)

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
		err := UpdateWorkspaceStatus(ctx, mockClient, key, nil)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should retry on retryable errors", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{},
			},
		}

		condition := &metav1.Condition{
			Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourcesReady",
			Message: "All resources are ready",
		}

		// Mock the Get call (multiple times due to retry)
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call to fail first with a retryable error, then succeed
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(
			apierrors.NewConflict(schema.GroupResource{Group: "kaito.sh", Resource: "workspaces"}, "test-workspace", fmt.Errorf("conflict"))).Once()

		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(nil).Once()

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
		err := UpdateWorkspaceStatus(ctx, mockClient, key, func(status *kaitov1beta1.WorkspaceStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should return error for non-retryable status update failures", func(t *testing.T) {
		mockClient := test.NewClient()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Status: kaitov1beta1.WorkspaceStatus{
				Conditions: []metav1.Condition{},
			},
		}

		condition := &metav1.Condition{
			Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourcesReady",
			Message: "All resources are ready",
		}

		// Mock the Get call
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-workspace", Namespace: "default"},
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.Workspace)
			*ws = *workspace
		}).Return(nil)

		// Mock the Status().Update call to fail with a non-retryable error
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.Workspace{}), mock.Anything).Return(fmt.Errorf("permanent error"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-workspace", Namespace: "default"}
		err := UpdateWorkspaceStatus(ctx, mockClient, key, func(status *kaitov1beta1.WorkspaceStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permanent error")
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})
}
