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
	"strings"
	"testing"
	"time"

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

func TestGenerateRandomString(t *testing.T) {
	t.Run("Should return empty string for zero length", func(t *testing.T) {
		result := generateRandomString(0)
		assert.Equal(t, "", result)
	})

	t.Run("Should return empty string for negative length", func(t *testing.T) {
		result := generateRandomString(-5)
		assert.Equal(t, "", result)
	})

	t.Run("Should generate string of correct length", func(t *testing.T) {
		lengths := []int{1, 5, 10, 20, 50}
		for _, length := range lengths {
			result := generateRandomString(length)
			assert.Len(t, result, length)
		}
	})

	t.Run("Should only contain lowercase letters and digits", func(t *testing.T) {
		result := generateRandomString(100)
		for _, char := range result {
			assert.True(t, (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'),
				"Character '%c' is not a lowercase letter or digit", char)
		}
	})

	t.Run("Should generate different strings on consecutive calls", func(t *testing.T) {
		// Generate multiple strings and check they're not all the same
		// Note: There's a very small chance this could fail due to randomness
		strings := make([]string, 10)
		for i := range strings {
			strings[i] = generateRandomString(10)
			time.Sleep(time.Nanosecond) // Ensure different timestamps
		}

		// Check that not all strings are identical
		firstString := strings[0]
		allSame := true
		for _, s := range strings[1:] {
			if s != firstString {
				allSame = false
				break
			}
		}
		assert.False(t, allSame, "All generated strings are identical, which is highly unlikely")
	})

	t.Run("Should handle large length values", func(t *testing.T) {
		result := generateRandomString(1000)
		assert.Len(t, result, 1000)

		// Verify all characters are valid
		for _, char := range result {
			assert.True(t, (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'))
		}
	})

	t.Run("Should generate string with expected character set distribution", func(t *testing.T) {
		// Generate a long string to check character distribution
		result := generateRandomString(10000)

		hasLetter := false
		hasDigit := false

		for _, char := range result {
			if char >= 'a' && char <= 'z' {
				hasLetter = true
			}
			if char >= '0' && char <= '9' {
				hasDigit = true
			}
			if hasLetter && hasDigit {
				break
			}
		}

		// With 10000 characters, it's virtually certain to have both letters and digits
		assert.True(t, hasLetter, "Generated string should contain at least one letter")
		assert.True(t, hasDigit, "Generated string should contain at least one digit")
	})
}

func TestGetWorkspaceNameWithRandomSuffix(t *testing.T) {
	t.Run("Should generate workspace name with correct format", func(t *testing.T) {
		baseName := "test-workspace"
		result := GetWorkspaceNameWithRandomSuffix(baseName)

		// Check that the result starts with the base name
		assert.True(t, strings.HasPrefix(result, baseName+"-"),
			"Result should start with base name followed by hyphen")

		// Check total length
		expectedLength := len(baseName) + 1 + WorkspaceNameSuffixLength // +1 for hyphen
		assert.Len(t, result, expectedLength)

		// Extract and validate the suffix
		suffix := result[len(baseName)+1:]
		assert.Len(t, suffix, WorkspaceNameSuffixLength)

		// Verify suffix contains only lowercase letters and digits
		for _, char := range suffix {
			assert.True(t, (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'),
				"Suffix character '%c' is not a lowercase letter or digit", char)
		}
	})

	t.Run("Should handle empty base name", func(t *testing.T) {
		baseName := ""
		result := GetWorkspaceNameWithRandomSuffix(baseName)

		// Should be just hyphen plus random suffix
		assert.True(t, strings.HasPrefix(result, "-"))
		assert.Len(t, result, 1+WorkspaceNameSuffixLength)

		// Verify the suffix part
		suffix := result[1:]
		assert.Len(t, suffix, WorkspaceNameSuffixLength)
		for _, char := range suffix {
			assert.True(t, (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'))
		}
	})

	t.Run("Should handle base name with special characters", func(t *testing.T) {
		baseNames := []string{
			"test-workspace-123",
			"test.workspace",
			"test_workspace",
			"test/workspace",
			"test@workspace",
		}

		for _, baseName := range baseNames {
			result := GetWorkspaceNameWithRandomSuffix(baseName)
			assert.True(t, strings.HasPrefix(result, baseName+"-"))
			assert.Len(t, result, len(baseName)+1+WorkspaceNameSuffixLength)
		}
	})

	t.Run("Should generate different suffixes for same base name", func(t *testing.T) {
		baseName := "test-workspace"
		results := make([]string, 10)

		for i := range results {
			results[i] = GetWorkspaceNameWithRandomSuffix(baseName)
			time.Sleep(time.Nanosecond) // Ensure different timestamps
		}

		// Check that not all results are identical
		firstResult := results[0]
		allSame := true
		for _, r := range results[1:] {
			if r != firstResult {
				allSame = false
				break
			}
		}
		assert.False(t, allSame, "Should generate different suffixes for the same base name")

		// All should have the same prefix though
		for _, r := range results {
			assert.True(t, strings.HasPrefix(r, baseName+"-"))
		}
	})

	t.Run("Should handle very long base name", func(t *testing.T) {
		baseName := strings.Repeat("a", 1000)
		result := GetWorkspaceNameWithRandomSuffix(baseName)

		assert.True(t, strings.HasPrefix(result, baseName+"-"))
		assert.Len(t, result, len(baseName)+1+WorkspaceNameSuffixLength)
	})

	t.Run("Should handle Unicode characters in base name", func(t *testing.T) {
		baseName := "..-workspace-.."
		result := GetWorkspaceNameWithRandomSuffix(baseName)

		assert.True(t, strings.HasPrefix(result, baseName+"-"))
		// Note: len() counts bytes, not runes for Unicode
		expectedSuffixStart := len(baseName) + 1
		suffix := result[expectedSuffixStart:]

		// The suffix length should still be WorkspaceNameSuffixLength
		assert.Len(t, suffix, WorkspaceNameSuffixLength)

		// Verify suffix contains only ASCII lowercase letters and digits
		for _, char := range suffix {
			assert.True(t, (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9'))
		}
	})

	t.Run("Should maintain consistent suffix length", func(t *testing.T) {
		baseNames := []string{"short", "medium-length", "very-long-base-name-for-testing"}

		for _, baseName := range baseNames {
			result := GetWorkspaceNameWithRandomSuffix(baseName)
			suffix := result[len(baseName)+1:]
			assert.Len(t, suffix, WorkspaceNameSuffixLength,
				"Suffix length should always be %d regardless of base name", WorkspaceNameSuffixLength)
		}
	})
}
