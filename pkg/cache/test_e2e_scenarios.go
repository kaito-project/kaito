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
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

// Provider-specific e2e scenarios keep the conformance suite provider-agnostic while
// still allowing a provider to declare behaviour that only makes sense for it (e.g.
// cacheName discovery, backend-CR deletion, data-plane counters). A provider
// registers scenarios on its Expectations; the e2e suite discovers every provider via
// ListExpectations and runs each registered scenario, so adding a provider (and its
// scenarios) requires no edits to the shared e2e code.
//
// Scenarios must not import the e2e test utilities. They operate purely against the
// E2EHarness abstraction (a controller-runtime client, a namespace, a workspace
// factory and a poller), so they can live in the provider package with no test-only
// dependencies and no import cycle.

// E2ECapability classifies what a scenario needs from the environment so the runner
// can skip scenarios whose prerequisites are not opted into.
type E2ECapability string

const (
	// E2ECapabilityControlPlane scenarios only inspect controller output (injected
	// StatefulSet, conditions, events). They need no running model pod and are safe
	// to run in any cache-enabled cluster.
	E2ECapabilityControlPlane E2ECapability = "controlplane"

	// E2ECapabilityDataPlane scenarios require a model pod that actually serves
	// (real weights or CPU serving) so cache hit/miss counters can be observed.
	// Runs on any node type — KAITO's provisioner handles CPU/GPU placement.
	E2ECapabilityDataPlane E2ECapability = "dataplane"

	// E2ECapabilityDisruptive scenarios mutate shared cluster state (redeploy the
	// controller, upgrade/remove the cache backend, scale nodes, delete a shared CR).
	// These should clean up after themselves.
	E2ECapabilityDisruptive E2ECapability = "disruptive"
)

// E2EHarness is the minimal surface a provider e2e scenario needs. The e2e test
// package implements it; provider packages depend only on this interface.
type E2EHarness interface {
	// Ctx returns the suite context.
	Ctx() context.Context

	// Client returns a controller-runtime client bound to the test cluster.
	Client() client.Client

	// Namespace returns the namespace scenarios should create resources in.
	Namespace() string

	// Provider returns the cache provider name this harness is configured for.
	Provider() kaitov1beta1.CacheProvider

	// NewCacheWorkspace builds (but does not create) a standard cache-enabled
	// Workspace with a unique name derived from idPrefix. Scenarios mutate the
	// returned object and create it via Client().
	NewCacheWorkspace(idPrefix string, spec kaitov1beta1.CacheSpec) *kaitov1beta1.Workspace

	// Poll retries fn until it returns nil or the timeout elapses, returning the
	// last error. Used to wait for eventual controller reconciliation.
	Poll(timeout time.Duration, fn func() error) error

	// PodLogs returns the current log output for a pod in the test namespace.
	PodLogs(podName string) (string, error)

	// Logf records progress in the test output.
	Logf(format string, args ...any)
}

// E2EScenario is a single provider-declared e2e test case.
type E2EScenario struct {
	// Name is a short, unique (within the provider) scenario description used in
	// the generated ginkgo spec name.
	Name string

	// Capability declares what the scenario needs; the runner skips it unless the
	// capability is enabled.
	Capability E2ECapability

	// Run executes the scenario and returns nil on success or a descriptive error.
	// Implementations should create their own resources in h.Namespace() and clean
	// them up (a Cleanup closure may be returned instead; see Run semantics below).
	Run func(h E2EHarness) error
}
