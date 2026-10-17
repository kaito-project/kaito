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

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type observedNodeClaim struct {
	oldName string
	name    string
	deleted bool
}

type fakeNodeClaimObserver struct {
	observed []observedNodeClaim
}

func (f *fakeNodeClaimObserver) ObserveNodeClaim(oldNC, newNC *karpenterv1.NodeClaim, deleted bool) {
	o := observedNodeClaim{name: newNC.Name, deleted: deleted}
	if oldNC != nil {
		o.oldName = oldNC.Name
	}
	f.observed = append(f.observed, o)
}

func TestNodeClaimEventHandlerNotifiesObserver(t *testing.T) {
	observer := &fakeNodeClaimObserver{}
	h := &nodeClaimEventHandler{
		enqueueHandler: enqueueWorkspaceForNodeClaim,
		expectations:   utils.NewControllerExpectations(),
		logger:         klog.Background(),
		observer:       observer,
	}
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	newNC := func(name string) *karpenterv1.NodeClaim {
		return &karpenterv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			consts.KarpenterWorkspaceNameKey:      "ws1",
			consts.KarpenterWorkspaceNamespaceKey: "default",
		}}}
	}
	deleting := newNC("nc-deleting")
	deleting.DeletionTimestamp = &metav1.Time{}

	ctx := context.Background()
	h.Create(ctx, event.TypedCreateEvent[client.Object]{Object: newNC("nc-1")}, q)
	h.Update(ctx, event.TypedUpdateEvent[client.Object]{ObjectOld: newNC("nc-1-old"), ObjectNew: newNC("nc-1")}, q)
	h.Delete(ctx, event.TypedDeleteEvent[client.Object]{Object: newNC("nc-1")}, q)
	h.Create(ctx, event.TypedCreateEvent[client.Object]{Object: deleting}, q)

	assert.Equal(t, []observedNodeClaim{
		{name: "nc-1", deleted: false},
		{oldName: "nc-1-old", name: "nc-1", deleted: false},
		{name: "nc-1", deleted: true},
		{name: "nc-deleting", deleted: true},
	}, observer.observed)
	assert.Equal(t, 1, q.Len(), "events for the same workspace are coalesced into one request")
}

func TestNodeClaimEventHandlerWithoutObserver(t *testing.T) {
	h := &nodeClaimEventHandler{
		enqueueHandler: enqueueWorkspaceForNodeClaim,
		expectations:   utils.NewControllerExpectations(),
		logger:         klog.Background(),
	}
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()

	nc := &karpenterv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "nc-1"}}
	assert.NotPanics(t, func() {
		h.Delete(context.Background(), event.TypedDeleteEvent[client.Object]{Object: nc}, q)
	})
}
