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
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestUpdateStatusConditionIfNotMatch(t *testing.T) {
	t.Run("Should skip update when condition matches current values", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:             metav1.ConditionTrue,
						Reason:             "ResourcesReady",
						Message:            "All resources are ready",
						ObservedGeneration: 1,
					},
				},
			},
		}

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		// No client calls should be made since condition matches
		mockClient.AssertExpectations(t)
	})

	t.Run("Should update when observedGeneration differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 2,
			},
			Status: kaitov1beta1.InferenceSetStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(kaitov1beta1.ConditionTypeResourceStatus),
						Status:             metav1.ConditionTrue,
						Reason:             "ResourcesReady",
						Message:            "All resources are ready",
						ObservedGeneration: 1,
					},
				},
			},
		}

		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.InferenceSet)
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, int64(2), condition.ObservedGeneration)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition status differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.InferenceSet)
			// Verify the condition was updated
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition reason differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "NewReason", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should update when condition message differs", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "New message")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should add new condition when condition type doesn't exist", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
				Conditions: []metav1.Condition{}, // Empty conditions
			},
		}

		// Mock the Get call for UpdateInferenceSetStatus
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.InferenceSet)
			// Verify the condition was added
			condition := meta.FindStatusCondition(ws.Status.Conditions, string(kaitov1beta1.ConditionTypeResourceStatus))
			assert.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			assert.Equal(t, "ResourcesReady", condition.Reason)
			assert.Equal(t, "All resources are ready", condition.Message)
		}).Return(nil)

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "ResourcesReady", "All resources are ready")

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should propagate error from UpdateInferenceSetStatus", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(fmt.Errorf("get error"))

		ctx := context.Background()
		err := UpdateStatusConditionIfNotMatch(ctx, mockClient, inferenceset,
			kaitov1beta1.ConditionTypeResourceStatus, metav1.ConditionTrue, "NewReason", "New message")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "get error")
		mockClient.AssertExpectations(t)
	})
}

func TestUpdateInferenceSetStatus(t *testing.T) {
	t.Run("Should successfully update inferenceset status", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-inferenceset",
				Namespace:  "default",
				Generation: 1,
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(1).(*kaitov1beta1.InferenceSet)
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
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1beta1.InferenceSetStatus) error {
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(
			apierrors.NewNotFound(schema.GroupResource{Group: "kaito.sh", Resource: "inferencesets"}, "test-inferenceset"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1beta1.InferenceSetStatus) error {
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(fmt.Errorf("network error"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1beta1.InferenceSetStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network error")
		mockClient.AssertExpectations(t)
	})

	t.Run("Should handle nil condition", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Status: kaitov1beta1.InferenceSetStatus{
				Conditions: []metav1.Condition{},
			},
		}

		// Mock the Get call
		mockClient.On("Get", mock.IsType(context.Background()),
			client.ObjectKey{Name: "test-inferenceset", Namespace: "default"},
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(nil)

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, nil)

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should retry on retryable errors", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call to fail first with a retryable error, then succeed
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(
			apierrors.NewConflict(schema.GroupResource{Group: "kaito.sh", Resource: "inferencesets"}, "test-inferenceset", fmt.Errorf("conflict"))).Once()

		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(nil).Once()

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1beta1.InferenceSetStatus) error {
			meta.SetStatusCondition(&status.Conditions, *condition)
			return nil
		})

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
		mockClient.StatusMock.AssertExpectations(t)
	})

	t.Run("Should return error for non-retryable status update failures", func(t *testing.T) {
		mockClient := test.NewClient()

		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Run(func(args mock.Arguments) {
			ws := args.Get(2).(*kaitov1beta1.InferenceSet)
			*ws = *inferenceset
		}).Return(nil)

		// Mock the Status().Update call to fail with a non-retryable error
		mockClient.StatusMock.On("Update", mock.IsType(context.Background()),
			mock.IsType(&kaitov1beta1.InferenceSet{}), mock.Anything).Return(fmt.Errorf("permanent error"))

		ctx := context.Background()
		key := &client.ObjectKey{Name: "test-inferenceset", Namespace: "default"}
		err := UpdateInferenceSetStatus(ctx, mockClient, key, func(status *kaitov1beta1.InferenceSetStatus) error {
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
		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
				Labels: map[string]string{
					"app": "test",
				},
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
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
		inferenceset1 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset-1",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		inferenceset2 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset-2",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("Should generate different hashes for different Spec", func(t *testing.T) {
		inferenceset1 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		inferenceset2 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(5)),
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("Should ignore Status field changes", func(t *testing.T) {
		inferenceset1 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
			Status: kaitov1beta1.InferenceSetStatus{
				Conditions: []metav1.Condition{},
			},
		}

		inferenceset2 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
			var inferenceset *kaitov1beta1.InferenceSet
			hash := ComputeInferenceSetHash(inferenceset)
			assert.Empty(t, hash)
		})
	})

	t.Run("Should generate same hashes for different label values", func(t *testing.T) {
		inferenceset1 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
				Labels: map[string]string{
					"version": "v1",
				},
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		inferenceset2 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
				Labels: map[string]string{
					"version": "v2",
				},
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("Should change hash when runtime annotation changes", func(t *testing.T) {
		inferenceset1 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{kaitov1beta1.AnnotationWorkspaceRuntime: "vllm"},
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		inferenceset2 := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{kaitov1beta1.AnnotationWorkspaceRuntime: "transformers"},
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
		}

		hash1 := ComputeInferenceSetHash(inferenceset1)
		hash2 := ComputeInferenceSetHash(inferenceset2)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("Should handle empty InferenceSet", func(t *testing.T) {
		inferenceset := &kaitov1beta1.InferenceSet{}

		hash := ComputeInferenceSetHash(inferenceset)

		assert.NotEmpty(t, hash)
		assert.Len(t, hash, 64)
	})
}
func TestMarshalInferenceSetFields(t *testing.T) {
	t.Run("Should marshal InferenceSet fields successfully", func(t *testing.T) {
		inferenceset := &kaitov1beta1.InferenceSet{
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
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
		inferenceset := &kaitov1beta1.InferenceSet{}

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
		inferenceset := &kaitov1beta1.InferenceSet{
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
			},
			Status: kaitov1beta1.InferenceSetStatus{
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
		inferenceset := &kaitov1beta1.InferenceSet{
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
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(5)),
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
		inferenceset := &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-inferenceset",
				Namespace: "default",
			},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(3)),
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
		inferenceSet    *kaitov1beta1.InferenceSet
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
			inferenceSet: &kaitov1beta1.InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "no-match-infset", Namespace: "empty-ns"},
			},
			expectedNames: []string{},
		},
		{
			name: "Should list Workspaces associated with InferenceSet in same namespace",
			inferenceSet: &kaitov1beta1.InferenceSet{
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
			inferenceSet: &kaitov1beta1.InferenceSet{
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
			inferenceSet: &kaitov1beta1.InferenceSet{
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

func TestNewWorkspaceForInferenceSetPropagatesCapacityType(t *testing.T) {
	is := &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
		Spec: kaitov1beta1.InferenceSetSpec{
			Template: kaitov1beta1.InferenceSetTemplate{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					kaitov1beta1.AnnotationCapacityType: consts.KarpenterCapacityTypeSpot,
				}},
			},
		},
	}

	ws := NewWorkspaceForInferenceSet(is)
	assert.Equal(t, consts.KarpenterCapacityTypeSpot, ws.Annotations[kaitov1beta1.AnnotationCapacityType])
}

// The API server appends 5 random characters to GenerateName, so the prefix
// must leave the generated Workspace name within MaxWorkspaceNameLength.
func TestNewWorkspaceForInferenceSetGenerateName(t *testing.T) {
	tests := []struct {
		name             string
		inferenceSetName string
		want             string
	}{
		{name: "short name is kept", inferenceSetName: "phi-4-mini", want: "phi-4-mini-"},
		{name: "longest name kept as is", inferenceSetName: strings.Repeat("a", 46), want: strings.Repeat("a", 46) + "-"},
		{name: "long name is truncated", inferenceSetName: strings.Repeat("b", 63), want: strings.Repeat("b", 46) + "-"},
		{
			name:             "trailing dashes left by truncation are trimmed",
			inferenceSetName: strings.Repeat("c", 44) + "--" + strings.Repeat("d", 17),
			want:             strings.Repeat("c", 44) + "-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := &kaitov1beta1.InferenceSet{ObjectMeta: metav1.ObjectMeta{Name: tt.inferenceSetName, Namespace: "default"}}
			ws := NewWorkspaceForInferenceSet(is)
			assert.Equal(t, tt.want, ws.GenerateName)
			assert.LessOrEqual(t, len(ws.GenerateName)+5, kaitov1beta1.MaxWorkspaceNameLength)
			assert.Equal(t, tt.inferenceSetName, ws.Labels[consts.WorkspaceCreatedByInferenceSetLabel])
		})
	}
}

func TestNewWorkspaceForInferenceSetSelectors(t *testing.T) {
	orig := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = orig })

	specSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}}
	nodeSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "gpu"}}

	tests := []struct {
		name             string
		byo              bool
		resourceSelector *metav1.LabelSelector
		want             *metav1.LabelSelector
	}{
		{name: "NAP inherits spec.labelSelector when unset", want: specSelector},
		{name: "NAP inherits spec.labelSelector when empty", resourceSelector: &metav1.LabelSelector{}, want: specSelector},
		{name: "NAP uses template.resource.labelSelector when set", resourceSelector: nodeSelector, want: nodeSelector},
		{name: "BYO does not inherit spec.labelSelector", byo: true, want: nil},
		{name: "BYO uses template.resource.labelSelector when set", byo: true, resourceSelector: nodeSelector, want: nodeSelector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tt.byo
			is := &kaitov1beta1.InferenceSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
				Spec: kaitov1beta1.InferenceSetSpec{
					Selector: specSelector,
					Template: kaitov1beta1.InferenceSetTemplate{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"team": "ml"}},
						Resource:   kaitov1beta1.InferenceSetResourceSpec{LabelSelector: tt.resourceSelector},
					},
				},
			}

			ws := NewWorkspaceForInferenceSet(is)
			assert.Equal(t, tt.want, ws.Resource.LabelSelector)
			assert.Equal(t, map[string]string{
				"app":  "llm",
				"team": "ml",
				consts.WorkspaceCreatedByInferenceSetLabel: "test-is",
			}, ws.Labels)
		})
	}
}

func TestNewWorkspaceForInferenceSetPropagatesRuntimeAnnotation(t *testing.T) {
	is := &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-is",
			Namespace: "default",
			Annotations: map[string]string{
				kaitov1beta1.AnnotationWorkspaceRuntime: "transformers",
			},
		},
		Spec: kaitov1beta1.InferenceSetSpec{},
	}

	ws := NewWorkspaceForInferenceSet(is)
	assert.Equal(t, "transformers", ws.Annotations[kaitov1beta1.AnnotationWorkspaceRuntime])
}

func TestValidateWorkspaceForInferenceSet(t *testing.T) {
	// BYO mode so an empty instanceType is valid and node-listing is skipped.
	orig := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = orig
	})

	newIS := func(inf kaitov1beta1.InferenceSpec) *kaitov1beta1.InferenceSet {
		return &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
			Spec: kaitov1beta1.InferenceSetSpec{
				Template: kaitov1beta1.InferenceSetTemplate{Inference: inf},
			},
		}
	}

	tests := []struct {
		name       string
		inference  kaitov1beta1.InferenceSpec
		wantErr    bool
		errContent string
	}{
		{
			name: "invalid preset name is rejected",
			inference: kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "invalid-preset"}},
			},
			wantErr:    true,
			errContent: "Unsupported inference preset name invalid-preset",
		},
		{
			name:       "neither preset nor template is rejected",
			inference:  kaitov1beta1.InferenceSpec{},
			wantErr:    true,
			errContent: "Preset or Template must be specified",
		},
		{
			name: "both preset and template is rejected",
			inference: kaitov1beta1.InferenceSpec{
				Preset:   &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "invalid-preset"}},
				Template: &corev1.PodTemplateSpec{},
			},
			wantErr:    true,
			errContent: "Preset and Template cannot be set at the same time",
		},
		{
			name:      "custom template on BYO nodes is accepted",
			inference: kaitov1beta1.InferenceSpec{Template: &corev1.PodTemplateSpec{}},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkspaceForInferenceSet(context.Background(), newIS(tt.inference))
			if tt.wantErr {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.errContent)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

// TestValidateWorkspaceForInferenceSet_InstanceTypeProjectedOnBYO verifies
// that a template-level instanceType is projected onto the child Workspace in
// BYO mode, where Workspace accepts it to filter BYO nodes by SKU.
func TestValidateWorkspaceForInferenceSet_InstanceTypeProjectedOnBYO(t *testing.T) {
	orig := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	origProvisioner := consts.ActiveNodeProvisioner
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	consts.ActiveNodeProvisioner = consts.NodeProvisionerBYO
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = orig
		consts.ActiveNodeProvisioner = origProvisioner
	})

	is := &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
		Spec: kaitov1beta1.InferenceSetSpec{
			Template: kaitov1beta1.InferenceSetTemplate{
				Resource:  kaitov1beta1.InferenceSetResourceSpec{InstanceType: "Standard_NC6s_v3"},
				Inference: kaitov1beta1.InferenceSpec{Template: &corev1.PodTemplateSpec{}},
			},
		},
	}

	assert.Equal(t, "Standard_NC6s_v3", NewWorkspaceForInferenceSet(is).Resource.InstanceType)
	assert.Nil(t, ValidateWorkspaceForInferenceSet(context.Background(), is))
}

// useFakeGlobalClient installs an empty fake client for the BYO create-time
// checks of preset inference, which list nodes and read ConfigMaps.
func useFakeGlobalClient(t *testing.T) {
	orig := k8sclient.GetGlobalClient()
	k8sclient.SetGlobalClient(fake.NewClientBuilder().Build())
	t.Cleanup(func() { k8sclient.SetGlobalClient(orig) })
}

func TestValidateWorkspaceForInferenceSet_Update(t *testing.T) {
	origNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	origStreaming := featuregates.FeatureGates[consts.FeatureFlagModelStreaming]
	origProvisioner := consts.ActiveNodeProvisioner
	useFakeGlobalClient(t)
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAP
		featuregates.FeatureGates[consts.FeatureFlagModelStreaming] = origStreaming
		consts.ActiveNodeProvisioner = origProvisioner
	})

	base := func() *kaitov1beta1.InferenceSet {
		return &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
			Spec: kaitov1beta1.InferenceSetSpec{
				Replicas: lo.ToPtr(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "llm"}},
				Template: kaitov1beta1.InferenceSetTemplate{
					ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
						kaitov1beta1.AnnotationCapacityType: consts.KarpenterCapacityTypeOnDemand,
					}},
					Resource:  kaitov1beta1.InferenceSetResourceSpec{InstanceType: "Standard_NC6s_v3"},
					Inference: kaitov1beta1.InferenceSpec{Template: &corev1.PodTemplateSpec{}},
				},
			},
		}
	}
	withPreset := func(name string) func(*kaitov1beta1.InferenceSet) {
		return func(is *kaitov1beta1.InferenceSet) {
			is.Spec.Template.Inference = kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: kaitov1beta1.ModelName(name)}},
			}
		}
	}
	withCustomConfig := func(config string) func(*kaitov1beta1.InferenceSet) {
		return func(is *kaitov1beta1.InferenceSet) {
			withPreset(plugin.PresetNameCustom)(is)
			is.Spec.Template.Inference.Config = config
		}
	}
	setAnnotation := func(key, value string) func(*kaitov1beta1.InferenceSet) {
		return func(is *kaitov1beta1.InferenceSet) { is.Spec.Template.Annotations[key] = value }
	}

	tests := []struct {
		name        string
		provisioner string
		streaming   bool
		mutateOld   func(*kaitov1beta1.InferenceSet)
		mutateNew   func(*kaitov1beta1.InferenceSet)
		wantErr     string
		wantErrOnce string
	}{
		{
			name:        "instanceType change is rejected with node auto-provisioning",
			provisioner: consts.NodeProvisionerKarpenter,
			mutateNew:   func(is *kaitov1beta1.InferenceSet) { is.Spec.Template.Resource.InstanceType = "Standard_NC12s_v3" },
			wantErr:     "instanceType cannot be changed once set",
		},
		{
			name:        "instanceType change is rejected on BYO nodes",
			provisioner: consts.NodeProvisionerBYO,
			mutateNew:   func(is *kaitov1beta1.InferenceSet) { is.Spec.Template.Resource.InstanceType = "Standard_NC12s_v3" },
			wantErr:     "instanceType cannot be changed once set",
		},
		{
			name:        "partition change is rejected",
			provisioner: consts.NodeProvisionerBYO,
			mutateNew: func(is *kaitov1beta1.InferenceSet) {
				is.Spec.Template.Resource.Partition = &kaitov1beta1.PartitionSpec{Mode: kaitov1beta1.PartitionModeAccelerator, Count: lo.ToPtr(2)}
			},
			wantErr: "field is immutable: spec.template.resource.partition",
		},
		{
			name:        "labelSelector change is rejected at spec.labelSelector",
			provisioner: consts.NodeProvisionerBYO,
			mutateNew:   func(is *kaitov1beta1.InferenceSet) { is.Spec.Selector.MatchLabels["app"] = "other" },
			wantErrOnce: "field is immutable: spec.labelSelector",
		},
		{
			name:        "template.resource.labelSelector change is rejected",
			provisioner: consts.NodeProvisionerBYO,
			mutateNew: func(is *kaitov1beta1.InferenceSet) {
				is.Spec.Template.Resource.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "gpu"}}
			},
			wantErr: "field is immutable: spec.template.resource.labelSelector",
		},
		{
			name:        "inherited labelSelector change is reported only at spec.labelSelector",
			provisioner: consts.NodeProvisionerKarpenter,
			mutateNew:   func(is *kaitov1beta1.InferenceSet) { is.Spec.Selector.MatchLabels["app"] = "other" },
			wantErrOnce: "labelSelector",
		},
		{
			name:        "preset change is rejected",
			provisioner: consts.NodeProvisionerBYO,
			mutateOld:   withPreset("old-preset"),
			mutateNew:   withPreset("new-preset"),
			wantErr:     "field is immutable: spec.template.inference.preset\n",
		},
		{
			name:        "custom preset config change is rejected",
			provisioner: consts.NodeProvisionerBYO,
			mutateOld:   withCustomConfig("model-a"),
			mutateNew:   withCustomConfig("model-b"),
			wantErr:     "field is immutable for preset \"custom\"",
		},
		{
			name:        "setting inference.template is rejected",
			provisioner: consts.NodeProvisionerBYO,
			mutateOld:   withPreset("some-preset"),
			mutateNew:   func(is *kaitov1beta1.InferenceSet) { is.Spec.Template.Inference.Template = &corev1.PodTemplateSpec{} },
			wantErr:     "field cannot be unset/set if it was set/unset: spec.template.inference.template",
		},
		{
			name:        "unsetting inference.template is rejected",
			provisioner: consts.NodeProvisionerBYO,
			mutateNew:   withPreset("some-preset"),
			wantErr:     "field cannot be unset/set if it was set/unset: spec.template.inference.template",
		},
		{
			name:        "capacity type change is rejected",
			provisioner: consts.NodeProvisionerKarpenter,
			mutateNew:   setAnnotation(kaitov1beta1.AnnotationCapacityType, consts.KarpenterCapacityTypeSpot),
			wantErr:     "annotation kaito.sh/capacity-type is immutable after creation",
		},
		{
			name:        "model-streaming annotation change is rejected when ModelStreaming is enabled",
			provisioner: consts.NodeProvisionerBYO,
			streaming:   true,
			mutateNew:   setAnnotation(mmconsts.AnnotationModelStreaming, "true"),
			wantErr:     "annotation kaito.sh/model-streaming is immutable after creation",
		},
		{
			name:        "invalid speculative-decoding value on template change is reported once",
			provisioner: consts.NodeProvisionerBYO,
			mutateNew:   setAnnotation(kaitov1beta1.AnnotationEnableSpeculativeDecoding, "yes"),
			wantErrOnce: "spec.template.metadata.annotations[kaito.sh/enable-speculative-decoding]",
		},
		{
			name:        "replicas-only change skips create-time checks",
			provisioner: consts.NodeProvisionerBYO,
			mutateOld:   withPreset("removed-preset"),
			mutateNew: func(is *kaitov1beta1.InferenceSet) {
				withPreset("removed-preset")(is)
				is.Spec.Replicas = lo.ToPtr(int32(3))
			},
		},
		{
			name:        "template change re-runs create-time checks",
			provisioner: consts.NodeProvisionerBYO,
			mutateOld:   withPreset("removed-preset"),
			mutateNew: func(is *kaitov1beta1.InferenceSet) {
				withPreset("removed-preset")(is)
				setAnnotation(kaitov1beta1.AnnotationPerformanceMode, kaitov1beta1.PerformanceModeThroughput)(is)
			},
			wantErr: "Unsupported inference preset name removed-preset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consts.ActiveNodeProvisioner = tt.provisioner
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tt.provisioner == consts.NodeProvisionerBYO
			featuregates.FeatureGates[consts.FeatureFlagModelStreaming] = tt.streaming

			old, updated := base(), base()
			if tt.mutateOld != nil {
				tt.mutateOld(old)
			}
			if tt.mutateNew != nil {
				tt.mutateNew(updated)
			}

			err := ValidateWorkspaceForInferenceSet(apis.WithinUpdate(context.Background(), old), updated)
			switch {
			case tt.wantErr != "":
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			case tt.wantErrOnce != "":
				assert.NotNil(t, err)
				assert.Equal(t, 1, strings.Count(err.Error(), tt.wantErrOnce), err.Error())
			default:
				assert.Nil(t, err)
			}
		})
	}
}

func TestValidateWorkspaceForInferenceSet_CreateAnnotations(t *testing.T) {
	origNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	origVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	origProvisioner := consts.ActiveNodeProvisioner
	useFakeGlobalClient(t)
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAP
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = origVLLM
		consts.ActiveNodeProvisioner = origProvisioner
	})

	newIS := func(annotations map[string]string, inference kaitov1beta1.InferenceSpec) *kaitov1beta1.InferenceSet {
		return &kaitov1beta1.InferenceSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
			Spec: kaitov1beta1.InferenceSetSpec{
				Template: kaitov1beta1.InferenceSetTemplate{
					ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
					Inference:  inference,
				},
			},
		}
	}
	podTemplate := kaitov1beta1.InferenceSpec{Template: &corev1.PodTemplateSpec{}}
	specDecodingField := "spec.template.metadata.annotations[kaito.sh/enable-speculative-decoding]"

	tests := []struct {
		name        string
		provisioner string
		vllm        bool
		is          *kaitov1beta1.InferenceSet
		wantErr     string
	}{
		{
			name:        "unsupported capacity type is rejected with Karpenter",
			provisioner: consts.NodeProvisionerKarpenter,
			vllm:        true,
			is:          newIS(map[string]string{kaitov1beta1.AnnotationCapacityType: "reserved"}, podTemplate),
			wantErr:     "\"reserved\" is not a supported capacity type",
		},
		{
			name:        "invalid speculative-decoding value is rejected",
			provisioner: consts.NodeProvisionerBYO,
			vllm:        true,
			is:          newIS(map[string]string{kaitov1beta1.AnnotationEnableSpeculativeDecoding: "yes"}, podTemplate),
			wantErr:     specDecodingField,
		},
		{
			name:        "speculative decoding without a preset is rejected",
			provisioner: consts.NodeProvisionerBYO,
			vllm:        true,
			is:          newIS(map[string]string{kaitov1beta1.AnnotationEnableSpeculativeDecoding: "true"}, podTemplate),
			wantErr:     "requires a preset inference",
		},
		{
			name:        "speculative decoding with the vLLM feature gate disabled is rejected",
			provisioner: consts.NodeProvisionerBYO,
			vllm:        false,
			is: newIS(map[string]string{kaitov1beta1.AnnotationEnableSpeculativeDecoding: "true"}, kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: "deepseek-r1-0528"}},
			}),
			wantErr: "requires the vLLM runtime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consts.ActiveNodeProvisioner = tt.provisioner
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = tt.provisioner == consts.NodeProvisionerBYO
			featuregates.FeatureGates[consts.FeatureFlagVLLM] = tt.vllm

			err := ValidateWorkspaceForInferenceSet(context.Background(), tt.is)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestValidateWorkspaceForInferenceSet_NAPDoesNotPanicOnNilCount reproduces the
// regression where the projected Workspace had no Resource.Count: the NAP code
// path in validateCreateWithInference dereferenced r.Count, which panicked the
// InferenceSet admission webhook. Validation now guards the nil deref.
func TestValidateWorkspaceForInferenceSet_NAPDoesNotPanicOnNilCount(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	origNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	origProvisioner := consts.ActiveNodeProvisioner
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	consts.ActiveNodeProvisioner = consts.NodeProvisionerAzureGPU
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAP
		consts.ActiveNodeProvisioner = origProvisioner
	})

	is := &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-is", Namespace: "default"},
		Spec: kaitov1beta1.InferenceSetSpec{
			Template: kaitov1beta1.InferenceSetTemplate{
				Resource:  kaitov1beta1.InferenceSetResourceSpec{InstanceType: "Standard_NC6s_v3"},
				Inference: kaitov1beta1.InferenceSpec{Template: &corev1.PodTemplateSpec{}},
			},
		},
	}

	assert.NotPanics(t, func() {
		_ = ValidateWorkspaceForInferenceSet(context.Background(), is)
	})
}

// CPU detection must inspect the same nodes as the generated Workspace, not
// similarly labeled nodes selected by the InferenceSet's Workspace selector.
func TestNewWorkspaceForInferenceSetCPUSelectorConsistency(t *testing.T) {
	test.RegisterTestModel()
	oldCPU := featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference]
	oldBYO := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = oldCPU
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldBYO
	})
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true

	scheme := runtime.NewScheme()
	assert.NoError(t, corev1.AddToScheme(scheme))
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cpu", Labels: map[string]string{"pool": "cpu", corev1.LabelArchStable: "amd64"}},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "gpu-decoy", Labels: map[string]string{"app": "workspaces", consts.NvidiaGPUCount: "1"}},
			Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
		},
	).Build()

	for _, tt := range []struct {
		name         string
		cpuEnabled   bool
		nodeSelector *metav1.LabelSelector
		wantCPU      bool
	}{
		{name: "explicit CPU pool wins over GPU Workspace selector", cpuEnabled: true, nodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}, wantCPU: true},
		{name: "GPU pool stays GPU", cpuEnabled: true, nodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "workspaces"}}},
		{name: "disabled CPU feature stays disabled", nodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "cpu"}}},
		{name: "BYO nil selector does not inherit Workspace labels", cpuEnabled: true},
		{name: "BYO empty selector does not inherit Workspace labels", cpuEnabled: true, nodeSelector: &metav1.LabelSelector{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			featuregates.FeatureGates[consts.FeatureFlagEnableCPUInference] = tt.cpuEnabled
			iObj := test.MockInferenceSetWithPresetVLLM.DeepCopy()
			iObj.Spec.Template.Resource.InstanceType = ""
			iObj.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "workspaces"}}
			iObj.Spec.Template.Resource.LabelSelector = tt.nodeSelector
			before := iObj.DeepCopy()
			ws := NewWorkspaceForInferenceSet(iObj)
			assert.Equal(t, tt.nodeSelector, ws.Resource.LabelSelector)
			assert.Equal(t, "workspaces", ws.Labels["app"])
			got, err := kaitov1beta1.ResolveCPUInferenceSetTarget(context.Background(), iObj, kubeClient)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantCPU, got)
			childCPU, err := kaitov1beta1.ResolveCPUInferenceWorkspace(context.Background(), ws, kubeClient)
			assert.NoError(t, err)
			assert.Equal(t, got, childCPU)
			assert.Equal(t, before, iObj, "construction and CPU resolution must not mutate the parent")
		})
	}
}
