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
	"testing"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func isolateProviderRegistry(t *testing.T) {
	t.Helper()

	mu.RLock()
	original := make(map[kaitov1beta1.CacheProvider]Provider, len(providers))
	for name, provider := range providers {
		original[name] = provider
	}
	mu.RUnlock()

	t.Cleanup(func() {
		mu.Lock()
		providers = original
		mu.Unlock()
	})
}

type statefulTestProvider struct {
	name      string
	available bool
	ready     bool
	reason    string
	availErr  error
	readyErr  error
}

func (p *statefulTestProvider) Name() string { return p.name }

func (p *statefulTestProvider) IsAvailable(_ context.Context, _ string) (bool, error) {
	return p.available, p.availErr
}

func (p *statefulTestProvider) IsReady(_ context.Context, _ string) (bool, string, error) {
	return p.ready, p.reason, p.readyErr
}

func (p *statefulTestProvider) PodMutations(_ context.Context, _ CacheConcern, _ *kaitov1beta1.Workspace, _, _, _ string) (*PodMutations, error) {
	return &PodMutations{}, nil
}

func (p *statefulTestProvider) Cleanup(_ context.Context, _ *kaitov1beta1.Workspace, _ string) error {
	return nil
}

func TestControllerReconcileDiscoversProviders(t *testing.T) {
	isolateProviderRegistry(t)

	Register(&noopTestProvider{})
	Register(&statefulTestProvider{name: "stateful-test", available: true, ready: false, reason: "warming"})

	controller := NewController(nil, record.NewFakeRecorder(4))
	result, err := controller.Reconcile(context.Background(), reconcile.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != ReadinessCheckInterval {
		t.Fatalf("expected requeue after %s, got %s", ReadinessCheckInterval, result.RequeueAfter)
	}

	noopStatus := controller.GetProviderStatus("noop-test")
	if noopStatus == nil || !noopStatus.Available || !noopStatus.Ready {
		t.Fatalf("expected noop provider status to be ready, got %#v", noopStatus)
	}

	mockStatus := controller.GetProviderStatus("stateful-test")
	if mockStatus == nil {
		t.Fatal("expected stateful provider status to be recorded")
	}
	if !mockStatus.Available || mockStatus.Ready || mockStatus.Reason != "warming" {
		t.Fatalf("unexpected stateful provider status: %#v", mockStatus)
	}
}

func TestControllerReconcileProviderStateTransition(t *testing.T) {
	isolateProviderRegistry(t)

	provider := &statefulTestProvider{name: "stateful-test", available: false}
	Register(provider)

	controller := NewController(nil, record.NewFakeRecorder(4))
	if _, err := controller.Reconcile(context.Background(), reconcile.Request{}); err != nil {
		t.Fatalf("unexpected error on first reconcile: %v", err)
	}

	status := controller.GetProviderStatus("stateful-test")
	if status == nil || status.Available || status.Ready {
		t.Fatalf("expected unavailable provider status, got %#v", status)
	}

	provider.available = true
	provider.ready = true
	provider.reason = "ready"

	if _, err := controller.Reconcile(context.Background(), reconcile.Request{}); err != nil {
		t.Fatalf("unexpected error on second reconcile: %v", err)
	}

	status = controller.GetProviderStatus("stateful-test")
	if status == nil {
		t.Fatal("expected provider status after transition")
	}
	if !status.Available || !status.Ready || status.Reason != "ready" {
		t.Fatalf("expected provider to transition to ready, got %#v", status)
	}
}
