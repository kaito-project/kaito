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

	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
)

type nodeClaimEventHandler struct {
	enqueueHandler handler.TypedEventHandler[client.Object, reconcile.Request]
	expectations   *utils.ControllerExpectations
	logger         klog.Logger
}

var _ handler.TypedEventHandler[client.Object, reconcile.Request] = (*nodeClaimEventHandler)(nil)

func getControllerKeyForNodeClaim(nc *karpenterv1.NodeClaim) *client.ObjectKey {
	name, ok := nc.Labels[kaitov1beta1.LabelWorkspaceName]
	if !ok {
		return nil
	}
	namespace, ok := nc.Labels[kaitov1beta1.LabelWorkspaceNamespace]
	if !ok {
		return nil
	}
	return &client.ObjectKey{Namespace: namespace, Name: name}
}

func (n *nodeClaimEventHandler) Create(ctx context.Context, evt event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	nc := evt.Object.(*karpenterv1.NodeClaim)
	if nc.DeletionTimestamp != nil {
		n.Delete(ctx, event.TypedDeleteEvent[client.Object]{Object: nc}, q)
		return
	}
	if key := getControllerKeyForNodeClaim(nc); key != nil {
		n.expectations.CreationObserved(n.logger, key.String())
		n.enqueueHandler.Create(ctx, evt, q)
	}
}

func (n *nodeClaimEventHandler) Delete(ctx context.Context, evt event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	n.enqueueHandler.Delete(ctx, evt, q)
}

func (n *nodeClaimEventHandler) Generic(ctx context.Context, evt event.TypedGenericEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
}

func (n *nodeClaimEventHandler) Update(ctx context.Context, evt event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	n.enqueueHandler.Update(ctx, evt, q)
}

var enqueueWorkspaceForNodeClaim = handler.EnqueueRequestsFromMapFunc(
	func(ctx context.Context, o client.Object) []reconcile.Request {
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
	})
