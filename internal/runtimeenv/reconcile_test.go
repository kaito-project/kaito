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

package runtimeenv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/kaito-project/kaito/api/env"
)

// The controllers inject the Env through the manager BaseContext; this checks
// that controller-runtime passes it through to Reconcile.
func TestBaseContextReachesReconcile(t *testing.T) {
	e := &env.Env{CloudProvider: "azure"}
	mgr, err := ctrl.NewManager(&rest.Config{Host: "http://127.0.0.1:1"}, ctrl.Options{
		Metrics:     metricsserver.Options{BindAddress: "0"},
		BaseContext: e.BaseContext,
	})
	require.NoError(t, err)

	seen := make(chan *env.Env, 1)
	c, err := controller.New("env-probe", mgr, controller.Options{
		SkipNameValidation: ptr.To(true),
		Reconciler: reconcile.Func(func(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
			seen <- env.FromContext(ctx)
			return reconcile.Result{}, nil
		}),
	})
	require.NoError(t, err)
	events := make(chan event.GenericEvent, 1)
	require.NoError(t, c.Watch(source.Channel(events, &handler.EnqueueRequestForObject{})))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = mgr.Start(ctx) }()
	events <- event.GenericEvent{Object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "probe"}}}

	select {
	case got := <-seen:
		assert.Same(t, e, got)
	case <-time.After(10 * time.Second):
		t.Fatal("Reconcile was not called")
	}
}
