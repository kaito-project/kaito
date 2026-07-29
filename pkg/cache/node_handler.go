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

package cache

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const cacheControllerTriggerKey = "cache-controller"

// nodeEventHandler enqueues a single reconcile request when Node events occur,
// allowing the Cache Controller to re-evaluate topology.
type nodeEventHandler struct{}

func (h *nodeEventHandler) Create(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if isNodeRelevant(e.Object) {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: cacheControllerTriggerKey}})
	}
}

func (h *nodeEventHandler) Update(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if isNodeRelevant(e.ObjectNew) {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: cacheControllerTriggerKey}})
	}
}

func (h *nodeEventHandler) Delete(_ context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if isNodeRelevant(e.Object) {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: cacheControllerTriggerKey}})
	}
}

func (h *nodeEventHandler) Generic(_ context.Context, _ event.GenericEvent, _ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
}

// isNodeRelevant filters Node events to only those that might affect cache topology.
func isNodeRelevant(obj client.Object) bool {
	_, ok := obj.(*corev1.Node)
	if !ok {
		return false
	}
	// Any node change (addition, removal, readiness transition) may affect
	// cache server scheduling, so always trigger a reconcile.
	return true
}
