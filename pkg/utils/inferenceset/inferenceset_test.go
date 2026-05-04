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

package inferenceset

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestUpdateStatusConditionIfNotMatch(t *testing.T) {
	t.Run("Should skip update when condition matches current values", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		// No client calls should be made since condition matches
		mockClient.AssertExpectations(t)
	})

	t.Run("Should update when condition status differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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

		// Mock the Get call for UpdateInferenceSetStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1alpha1.InferenceSet)
			// Verify the condition was updated
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition reason differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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

		// Mock the Get call for UpdateInferenceSetStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "NewReason", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition message differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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

		// Mock the Get call for UpdateInferenceSetStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "New message")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should add new condition when condition type doesn't exist", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
				Conditions: []metav1.Condition{}, // Empty conditions
			},
		}

		// Mock the Get call for UpdateInferenceSetStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1alpha1.InferenceSet)
			// Verify the condition was added
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should propagate error from UpdateInferenceSetStatus", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(fmt.Errorf("get error"))

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1alpha1.ConditionTypeResourceStatus, metav1.ConditionTrue, "NewReason", "New message")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "get error")
		mockClient.AssertExpectations(t)
	})
}

func TestUpdateInferenceSetStatus(t *testing.T) {
	t.Run("Should successfully update inferenceset status", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1alpha1.InferenceSet)
			// Verify the condition was set
			foundCondition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, foundCondition)
			assert.Equal(t, condition.Type, foundCondition.Type)
			assert.Equal(t, condition.Status, foundCondition.Status)
			assert.Equal(t, condition.Reason, foundCondition.Reason)
			assert.Equal(t, condition.Message, foundCondition.Message)
		}).Return(nil)

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1alpha1.InferenceSetStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should handle inferenceset not found gracefully", func(t *testing.T) {
		mockClient := test.NewClient()

		condition := &metav1.Condition{
			Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
			Status:  metav1.ConditionTrue,
			Reason:  "ResourcesReady",
			Message: "All resources are ready",
		}

		// Mock the Get call to return NotFound error
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(
			apierrors.NewNotFound(schema.GroupResource{Group: "kaito.sh", Resource: "inferencesets"}, "test-inferenceset"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1alpha1.InferenceSetStatus) error {
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
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(fmt.Errorf("network error"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1alpha1.InferenceSetStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network error")
		mockClient.AssertExpectations(t)
	})

	t.Run("Should handle nil condition", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Status: kaitov1alpha1.InferenceSetStatus{
				Conditions: []metav1.Condition{},
			},
		}

		// Mock the Get call
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil)

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, nil)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should retry on retryable errors", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call to fail first with a retryable error, then succeed
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(
			apierrors.NewConflict(schema.GroupResource{Group: "kaito.sh", Resource: "inferencesets"}, "test-inferenceset", fmt.Errorf("conflict"))).Once()

		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(nil).Once()

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1alpha1.InferenceSetStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should return error for non-retryable status update failures", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Status: kaitov1alpha1.InferenceSetStatus{
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
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1alpha1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call to fail with a non-retryable error
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1alpha1.InferenceSet{}), mock.Anything).Return(fmt.Errorf("permanent error"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1alpha1.InferenceSetStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permanent error")
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})
}

func TestComputeInferenceSetHash(t *testing.T) {
	t.Run("Should generate consistent hash for same InferenceSet", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
				Labels: map[string]string{
					"app": "test",
				},
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset)
		hash2 := ComputeInferenceSetHash(inferenceset)

		assert.Equal(t, hash1, hash2)
		assert.NotEmpty(t, hash1)
		// SHA256 produces 64 character hex string
		assert.Len(t, hash1, 64)
	})

	t.Run("Should generate same hashes for different ObjectMeta", func(t *testing.T) {
		inferenceset1 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset-1",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		inferenceset2 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset-2",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("Should generate different hashes for different Spec", func(t *testing.T) {
		inferenceset1 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		inferenceset2 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 5,
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("Should ignore Status field changes", func(t *testing.T) {
		inferenceset1 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
				Conditions: []metav1.Condition{},
			},
		}

		inferenceset2 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionTrue,
						Reason:  "Ready",
						Message: "Resources are ready",
					},
				},
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("Should handle nil InferenceSet gracefully", func(t *testing.T) {
		// This test verifies that the function doesn't panic with nil input
		assert.NotPanics(t, func() {
			var inferenceset *kaitov1alpha1.InferenceSet
			hash := ComputeInferenceSetHash(inferenceset)
			assert.Empty(t, hash)
		})
	})

	t.Run("Should generate same hashes for different label values", func(t *testing.T) {
		inferenceset1 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
				Labels: map[string]string{
					"version": "v1",
				},
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		inferenceset2 := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
				Labels: map[string]string{
					"version": "v2",
				},
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("Should handle empty InferenceSet", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{}

		hash := ComputeInferenceSetHash(inferenceset)

		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 64)
	})
}
func TestMarshalInferenceSetFields(t *testing.T) {
	t.Run("Should marshal InferenceSet fields successfully", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		jsonData, err := MarshalInferenceSetFields(inferenceset)

		assert.NoError(t, err)
		assert.NotNil(t, jsonData)

		// Unmarshal to verify the content
		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		assert.NoError(t, err)

		// Verify Spec content
		spec, ok := result["Spec"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, float64(3), spec["replicas"]) // JSON unmarshals numbers as float64
	})

	t.Run("Should return error for nil InferenceSet", func(t *testing.T) {
		jsonData, err := MarshalInferenceSetFields(nil)

		assert.Error(t, err)
		assert.Nil(t, jsonData)
		assert.Equal(t, "InferenceSet object is nil", err.Error())
	})

	t.Run("Should marshal empty InferenceSet", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{}

		jsonData, err := MarshalInferenceSetFields(inferenceset)

		assert.NoError(t, err)
		assert.NotNil(t, jsonData)

		// Unmarshal to verify the content
		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		assert.NoError(t, err)

		// Verify the structure
		assert.Contains(t, result, "Spec")
		assert.Len(t, result, 1) // Only Spec field should be present
	})

	t.Run("Should exclude Status field", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
			Status: kaitov1alpha1.InferenceSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:  metav1.ConditionTrue,
						Reason:  "Ready",
						Message: "Resources are ready",
					},
				},
			},
		}

		jsonData, err := MarshalInferenceSetFields(inferenceset)

		assert.NoError(t, err)
		assert.NotNil(t, jsonData)

		// Unmarshal to verify the content
		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		assert.NoError(t, err)

		// Verify Status is not included
		assert.NotContains(t, result, "Status")
		assert.Len(t, result, 1) // Only Spec
	})

	t.Run("Should marshal complex InferenceSet correctly", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "complex-inferenceset",
				Namespace: "production",
				Labels: map[string]string{
					"app":     "test",
					"version": "v1",
					"env":     "prod",
				},
				Annotations: map[string]string{
					"description": "Test InferenceSet",
					"owner":       "team-a",
				},
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 5,
			},
		}

		jsonData, err := MarshalInferenceSetFields(inferenceset)

		assert.NoError(t, err)
		assert.NotNil(t, jsonData)

		// Unmarshal to verify the content
		var result map[string]interface{}
		err = json.Unmarshal(jsonData, &result)
		assert.NoError(t, err)
	})

	t.Run("Should produce valid JSON output", func(t *testing.T) {
		inferenceset := &kaitov1alpha1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1alpha1.InferenceSetSpec{
				Replicas: 3,
			},
		}

		jsonData, err := MarshalInferenceSetFields(inferenceset)

		assert.NoError(t, err)
		assert.NotNil(t, jsonData)

		// Verify it's valid JSON
		assert.True(t, json.Valid(jsonData))
	})
}

func TestListWorkspaces(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, kaitov1beta1.AddToScheme(scheme))

	mkWS := func(name, ns, createdBy string) *kaitov1beta1.Workspace {
		ws := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		}
		if createdBy != "" {
			ws.Labels = map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: createdBy,
			}
		}
		return ws
	}

	listErr := fmt.Errorf("simulated list failure")

	tests := []struct {
		name            string
		inferenceSet    *kaitov1alpha1.InferenceSet
		existingObjects []client.Object
		// listInterceptor, when non-nil, replaces the fake client's List
		// implementation. Used to simulate a List error from the API server.
		listInterceptor func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error
		expectErr       bool
		expectedErrMsg  string
		// expectedNames is the set of Workspace names the call should
		// return. nil means do not validate; an empty slice means expect
		// zero items.
		expectedNames []string
	}{
		{
			name:           "Should return error for nil InferenceSet",
			inferenceSet:   nil,
			expectErr:      true,
			expectedErrMsg: "InferenceSet object is nil",
		},
		{
			name: "Should return empty list when no Workspaces exist",
			inferenceSet: &kaitov1alpha1.InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "no-match-infset", Namespace: "empty-ns"},
			},
			expectedNames: []string{},
		},
		{
			name: "Should list Workspaces associated with InferenceSet in same namespace",
			inferenceSet: &kaitov1alpha1.InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-inferenceset", Namespace: "default"},
			},
			existingObjects: []client.Object{
				mkWS("workspace-1", "default", "test-inferenceset"),
				mkWS("workspace-2", "default", "test-inferenceset"),
			},
			expectedNames: []string{"workspace-1", "workspace-2"},
		},
		{
			name: "Should only return Workspaces matching namespace and label out of many",
			inferenceSet: &kaitov1alpha1.InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "target-infset", Namespace: "tenant-a"},
			},
			existingObjects: []client.Object{
				// 2 in target ns with matching created-by label -> SHOULD be returned
				mkWS("match-1", "tenant-a", "target-infset"),
				mkWS("match-2", "tenant-a", "target-infset"),
				// in target ns with a DIFFERENT created-by label -> should NOT be returned
				mkWS("other-label", "tenant-a", "other-infset"),
				// in target ns with NO created-by label -> should NOT be returned
				mkWS("no-label", "tenant-a", ""),
				// in another namespace with the MATCHING label -> should NOT be returned
				// (this verifies the namespace-scoping fix)
				mkWS("cross-ns-match", "tenant-b", "target-infset"),
			},
			expectedNames: []string{"match-1", "match-2"},
		},
		{
			name: "Should propagate List error from client",
			inferenceSet: &kaitov1alpha1.InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-inferenceset", Namespace: "default"},
			},
			listInterceptor: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				return listErr
			},
			expectErr:      true,
			expectedErrMsg: listErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if len(tt.existingObjects) > 0 {
				builder = builder.WithObjects(tt.existingObjects...)
			}
			if tt.listInterceptor != nil {
				builder = builder.WithInterceptorFuncs(interceptor.Funcs{
					List: tt.listInterceptor,
				})
			}
			cl := builder.Build()

			got, err := ListWorkspaces(context.Background(), tt.inferenceSet, cl)

			if tt.expectErr {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				// nil-InferenceSet path returns a nil list; the List-error
				// path still returns a non-nil (possibly empty) list pointer.
				if tt.inferenceSet == nil {
					assert.Nil(t, got)
				} else {
					assert.NotNil(t, got)
				}
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, got)

			if tt.expectedNames != nil {
				gotNames := make([]string, 0, len(got.Items))
				for _, w := range got.Items {
					gotNames = append(gotNames, w.Name)
					// Sanity: every returned item must satisfy both filters.
					assert.Equal(t, tt.inferenceSet.Namespace, w.Namespace,
						"returned Workspace has wrong namespace")
					assert.Equal(t, tt.inferenceSet.Name,
						w.Labels[consts.WorkspaceCreatedByInferenceSetLabel],
						"returned Workspace has wrong created-by label")
				}
				assert.ElementsMatch(t, tt.expectedNames, gotNames,
					"returned Workspace set does not match expectation")
			}
		})
	}
}
