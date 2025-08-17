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

package nodeclaim

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
)

// MockEventHandler implements handler.TypedEventHandler
type MockEventHandler struct {
	mock.Mock
}

func (m *MockEventHandler) Create(ctx context.Context, evt event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	m.Called(ctx, evt, q)
}

func (m *MockEventHandler) Delete(ctx context.Context, evt event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	m.Called(ctx, evt, q)
}

func (m *MockEventHandler) Update(ctx context.Context, evt event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	m.Called(ctx, evt, q)
}

func (m *MockEventHandler) Generic(ctx context.Context, evt event.TypedGenericEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	m.Called(ctx, evt, q)
}

// createTestWorkQueue creates a real workqueue for testing
func createTestWorkQueue() workqueue.TypedRateLimitingInterface[reconcile.Request] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
}

func TestGetControllerKeyForNodeClaim(t *testing.T) {
	t.Run("Should return controller key when both labels are present", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		key := getControllerKeyForNodeClaim(nc)

		assert.NotNil(t, key)
		assert.Equal(t, "test-namespace", key.Namespace)
		assert.Equal(t, "test-workspace", key.Name)
	})

	t.Run("Should return nil when workspace name label is missing", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		key := getControllerKeyForNodeClaim(nc)

		assert.Nil(t, key)
	})

	t.Run("Should return nil when workspace namespace label is missing", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: "test-workspace",
				},
			},
		}

		key := getControllerKeyForNodeClaim(nc)

		assert.Nil(t, key)
	})

	t.Run("Should return nil when both labels are missing", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-node-claim",
				Labels: map[string]string{},
			},
		}

		key := getControllerKeyForNodeClaim(nc)

		assert.Nil(t, key)
	})

	t.Run("Should return nil when labels map is nil", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-node-claim",
				Labels: nil,
			},
		}

		key := getControllerKeyForNodeClaim(nc)

		assert.Nil(t, key)
	})
}

func TestNodeClaimEventHandlerCreate(t *testing.T) {
	t.Run("Should handle normal create event", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		ctx := context.Background()
		evt := event.TypedCreateEvent[client.Object]{Object: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		mockHandler.On("Create", ctx, evt, queue).Return()

		handler.Create(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})

	t.Run("Should handle create event with deletion timestamp as delete", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		now := metav1.Now()
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-node-claim",
				DeletionTimestamp: &now,
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		ctx := context.Background()
		evt := event.TypedCreateEvent[client.Object]{Object: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		mockHandler.On("Delete", ctx, event.TypedDeleteEvent[client.Object]{Object: nc}, queue).Return()

		handler.Create(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})

	t.Run("Should not process when controller key is nil", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-node-claim",
				Labels: map[string]string{}, // Missing required labels
			},
		}

		ctx := context.Background()
		evt := event.TypedCreateEvent[client.Object]{Object: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		// No expectations should be called
		handler.Create(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})
}

func TestNodeClaimEventHandlerDelete(t *testing.T) {
	t.Run("Should handle delete event with valid controller key", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		ctx := context.Background()
		evt := event.TypedDeleteEvent[client.Object]{Object: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		mockHandler.On("Delete", ctx, evt, queue).Return()

		handler.Delete(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})

	t.Run("Should handle delete event without valid controller key", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-node-claim",
				Labels: map[string]string{}, // Missing required labels
			},
		}

		ctx := context.Background()
		evt := event.TypedDeleteEvent[client.Object]{Object: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		// Only the enqueue handler should be called, no expectations
		mockHandler.On("Delete", ctx, evt, queue).Return()

		handler.Delete(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})
}

func TestNodeClaimEventHandlerUpdate(t *testing.T) {
	t.Run("Should delegate to enqueue handler", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
			},
		}

		ctx := context.Background()
		evt := event.TypedUpdateEvent[client.Object]{ObjectOld: nc, ObjectNew: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		mockHandler.On("Update", ctx, evt, queue).Return()

		handler.Update(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})
}

func TestNodeClaimEventHandlerGeneric(t *testing.T) {
	t.Run("Should do nothing", func(t *testing.T) {
		mockHandler := &MockEventHandler{}
		expectations := utils.NewControllerExpectations()
		logger := klog.NewKlogr()

		handler := &nodeClaimEventHandler{
			enqueueHandler: mockHandler,
			expectations:   expectations,
			logger:         logger,
		}

		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
			},
		}

		ctx := context.Background()
		evt := event.TypedGenericEvent[client.Object]{Object: nc}
		queue := createTestWorkQueue()
		defer queue.ShutDown()

		// Should not call any methods
		handler.Generic(ctx, evt, queue)

		mockHandler.AssertExpectations(t)
	})
}

func TestEnqueueWorkspaceForNodeClaimFunction(t *testing.T) {
	// Test the underlying function directly by extracting it from the handler
	testFunc := func(ctx context.Context, o client.Object) []reconcile.Request {
		nodeClaimObj := o.(*karpenterv1.NodeClaim)
		key := getControllerKeyForNodeClaim(nodeClaimObj)
		if key == nil {
			return nil
		}
		return []reconcile.Request{
			{
				NamespacedName: *key,
			},
		}
	}

	t.Run("Should return reconcile request for valid NodeClaim", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName:      "test-workspace",
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		ctx := context.Background()
		requests := testFunc(ctx, nc)

		assert.Len(t, requests, 1)
		assert.Equal(t, "test-namespace", requests[0].Namespace)
		assert.Equal(t, "test-workspace", requests[0].Name)
	})

	t.Run("Should return empty slice for NodeClaim without valid labels", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-node-claim",
				Labels: map[string]string{}, // Missing required labels
			},
		}

		ctx := context.Background()
		requests := testFunc(ctx, nc)

		assert.Empty(t, requests)
	})

	t.Run("Should return empty slice for NodeClaim with only workspace name label", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: "test-workspace",
				},
			},
		}

		ctx := context.Background()
		requests := testFunc(ctx, nc)

		assert.Empty(t, requests)
	})

	t.Run("Should return empty slice for NodeClaim with only workspace namespace label", func(t *testing.T) {
		nc := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-claim",
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceNamespace: "test-namespace",
				},
			},
		}

		ctx := context.Background()
		requests := testFunc(ctx, nc)

		assert.Empty(t, requests)
	})
}
