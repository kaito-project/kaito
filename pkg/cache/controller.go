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
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

const (
	// ReadinessCheckInterval is the default interval between provider readiness polls.
	ReadinessCheckInterval = 30 * time.Second
)

// TODO(distributed-cache): Phase 5 — Observability. Define a standard metrics interface
// for providers to expose cache performance (hit/miss rate, latency, eviction counts).
// Surface cache performance in Workspace status annotations or events.

// ProviderStatus holds the last-known status of a cache provider.
type ProviderStatus struct {
	Available bool
	Ready     bool
	Reason    string
	LastCheck time.Time
}

// Controller is the Cache Controller that manages provider lifecycle,
// monitors readiness, and watches node topology for cache infrastructure.
type Controller struct {
	client.Client
	Recorder record.EventRecorder

	mu       sync.RWMutex
	statuses map[kaitov1beta1.CacheProvider]*ProviderStatus
}

// NewController creates a new Cache Controller instance.
func NewController(c client.Client, recorder record.EventRecorder) *Controller {
	return &Controller{
		Client:   c,
		Recorder: recorder,
		statuses: make(map[kaitov1beta1.CacheProvider]*ProviderStatus),
	}
}

// Reconcile is triggered by Node events and periodic requeue. It checks
// all registered providers for availability and readiness, emitting events
// on state transitions.
func (c *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	klog.V(4).InfoS("Cache controller reconcile", "trigger", req.NamespacedName)

	providers := List()
	for _, p := range providers {
		providerName := kaitov1beta1.CacheProvider(p.Name())
		oldStatus := c.getStatus(providerName)

		available, err := p.IsAvailable(ctx, "")
		if err != nil {
			klog.V(2).InfoS("Cache provider availability check error",
				"provider", p.Name(), "error", err)
			c.setStatus(providerName, &ProviderStatus{
				Available: false,
				Ready:     false,
				Reason:    err.Error(),
				LastCheck: time.Now(),
			})
			if oldStatus != nil && oldStatus.Available {
				c.emitEvent(p.Name(), "CacheProviderUnavailable",
					"Provider %s became unavailable: %v", p.Name(), err)
			}
			continue
		}

		if !available {
			c.setStatus(providerName, &ProviderStatus{
				Available: false,
				Ready:     false,
				Reason:    "cache infrastructure not installed",
				LastCheck: time.Now(),
			})
			if oldStatus == nil || oldStatus.Available {
				klog.V(2).InfoS("Cache provider not available", "provider", p.Name())
				c.emitEvent(p.Name(), "CacheProviderUnavailable",
					"Provider %s: cache infrastructure not installed", p.Name())
			}
			continue
		}

		ready, reason, err := p.IsReady(ctx, "")
		if err != nil {
			klog.V(2).InfoS("Cache provider readiness check error",
				"provider", p.Name(), "error", err)
			c.setStatus(providerName, &ProviderStatus{
				Available: true,
				Ready:     false,
				Reason:    err.Error(),
				LastCheck: time.Now(),
			})
			continue
		}

		newStatus := &ProviderStatus{
			Available: true,
			Ready:     ready,
			Reason:    reason,
			LastCheck: time.Now(),
		}
		c.setStatus(providerName, newStatus)

		// Emit events on state transitions.
		if oldStatus != nil && !oldStatus.Ready && ready {
			c.emitEvent(p.Name(), "CacheProviderReady",
				"Provider %s is ready: %s", p.Name(), reason)
		} else if oldStatus != nil && oldStatus.Ready && !ready {
			c.emitEvent(p.Name(), "CacheProviderNotReady",
				"Provider %s is no longer ready: %s", p.Name(), reason)
		} else if oldStatus == nil && available {
			klog.InfoS("Cache provider discovered",
				"provider", p.Name(), "available", available, "ready", ready)
		}
	}

	// Requeue periodically to monitor readiness.
	return reconcile.Result{RequeueAfter: ReadinessCheckInterval}, nil
}

// GetProviderStatus returns the cached status for a provider.
func (c *Controller) GetProviderStatus(name kaitov1beta1.CacheProvider) *ProviderStatus {
	return c.getStatus(name)
}

// SetupWithManager registers the Cache Controller with the manager.
// It watches Nodes to detect topology changes that affect cache scheduling.
func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("cache").
		Watches(&corev1.Node{}, &nodeEventHandler{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(c)
}

func (c *Controller) getStatus(name kaitov1beta1.CacheProvider) *ProviderStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.statuses[name]
}

func (c *Controller) setStatus(name kaitov1beta1.CacheProvider, status *ProviderStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statuses[name] = status
}

func (c *Controller) emitEvent(provider, reason, messageFmt string, args ...interface{}) {
	msg := fmt.Sprintf(messageFmt, args...)
	klog.InfoS("Cache controller event", "provider", provider, "reason", reason, "message", msg)

	// Try to emit event on the Cache CR if one exists for this provider.
	// This makes events visible via `kubectl describe cache`.
	p, err := Get(kaitov1beta1.CacheProvider(provider))
	if err != nil {
		return
	}
	if ep, ok := p.(EventTarget); ok {
		if obj := ep.EventObject(); obj != nil {
			c.Recorder.Event(obj, corev1.EventTypeNormal, reason, msg)
		}
	}
}
