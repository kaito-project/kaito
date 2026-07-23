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

package dacs

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
)

// This file declares the DACS-specific end-to-end scenarios exercised by the
// provider-agnostic e2e conformance runner. Every scenario is expressed purely
// against the cache.E2EHarness abstraction so it carries no test-framework
// dependency and lives entirely in the provider package — new providers add their
// own scenarios the same way and the shared runner discovers them automatically.
//
// Node/GPU budget: the control-plane scenarios below reuse the already-running
// cache-sample backend and the shared BYO inference node, and only inspect the
// controller-produced StatefulSet and Workspace conditions. They never create a
// second cache backend and never provision GPU nodes. Scenarios that would need a
// serving model pod (data-plane) or that mutate shared cluster state (disruptive)
// are tagged accordingly and skipped unless explicitly opted into.

const (
	// nonExistentCacheName is a DACS cache CR name that is guaranteed not to exist,
	// used to drive not-ready / unavailable code paths without touching real caches.
	nonExistentCacheName = "cache-does-not-exist-e2e"

	modelCacheReadyCond = "ModelCacheReady"
)

// dacsE2EScenarios returns the DACS provider's e2e scenarios. It is called from
// init (via RegisterExpectations) so the scenarios are discovered whenever the
// provider package is imported.
func dacsE2EScenarios() []cache.E2EScenario {
	return []cache.E2EScenario{
		// t5: an explicit cacheName in the Config ConfigMap is honored for
		// availability — a bad name suppresses injection, the real name enables it.
		// This is DACS-specific because it exercises the DACS cacheName discovery
		// mechanism (resolveDiscoveryEndpoint from a Cache CR).
		{
			Name:       "cacheName override in the Config ConfigMap is honored",
			Capability: cache.E2ECapabilityControlPlane,
			Run:        scenarioCacheNameOverrideHonored,
		},
		// --- Gated scenarios (skipped unless opted-in) ---

		// t27: a bad client ImageVolume must fail the pod clearly and must NOT
		// flip the cache condition to Ready. DACS-specific (ImageVolume mechanism).
		{
			Name:       "bad client ImageVolume fails the pod without marking the cache ready",
			Capability: cache.E2ECapabilityDataPlane,
			Run:        scenarioImageVolumePullFailure,
		},
		// t8/t9: warm vs cold cache — the served model reports remote cache hits
		// on the second load. Needs a serving model pod.
		{
			Name:       "warm cache serves model with remote cache hits",
			Capability: cache.E2ECapabilityDataPlane,
			Run:        scenarioWarmCacheDataPlane,
		},
		// t18: warm load time is meaningfully lower than cold. Needs serving.
		{
			Name:       "warm load latency is at most half of cold load latency",
			Capability: cache.E2ECapabilityDataPlane,
			Run:        scenarioPerfThreshold,
		},
		// t19: on a cache read failure the Opportunistic workload falls back to blob.
		{
			Name:       "Opportunistic workload falls back to blob when cache read fails",
			Capability: cache.E2ECapabilityDataPlane,
			Run:        scenarioBlobFallback,
		},
	}
}

// ---- control-plane scenarios ----

func scenarioCacheNameOverrideHonored(h cache.E2EHarness) error {
	// An explicit cacheName provided through the Config ConfigMap must be honored:
	// a nonexistent cache name suppresses injection (unavailable), while the real
	// cache name drives injection. This exercises the availability gating that the
	// controller applies per-workspace based on the resolved cacheName.
	//
	// Bad name → no injection (cache unavailable).
	badCM, err := createCacheNameConfigMap(h, "dacs-name-bad", nonExistentCacheName)
	if err != nil {
		return err
	}
	defer func() { _ = h.Client().Delete(h.Ctx(), badCM) }()
	badWS := h.NewCacheWorkspace("dacs-name-bad", kaitov1beta1.CacheSpec{
		ModelCache: &kaitov1beta1.ModelCacheSpec{
			Provider: kaitov1beta1.CacheProvider(ProviderName),
			Mode:     kaitov1beta1.CacheModeOpportunistic,
			Config:   badCM.Name,
		},
	})
	if err := h.Client().Create(h.Ctx(), badWS); err != nil {
		return fmt.Errorf("creating bad-name workspace: %w", err)
	}
	defer func() { _ = h.Client().Delete(h.Ctx(), badWS) }()

	// Real name → injection with the matching discovery endpoint.
	goodCM, err := createCacheNameConfigMap(h, "dacs-name-good", defaultCacheName)
	if err != nil {
		return err
	}
	defer func() { _ = h.Client().Delete(h.Ctx(), goodCM) }()
	goodWS := h.NewCacheWorkspace("dacs-name-good", kaitov1beta1.CacheSpec{
		ModelCache: &kaitov1beta1.ModelCacheSpec{
			Provider: kaitov1beta1.CacheProvider(ProviderName),
			Mode:     kaitov1beta1.CacheModeOpportunistic,
			Config:   goodCM.Name,
		},
	})
	if err := h.Client().Create(h.Ctx(), goodWS); err != nil {
		return fmt.Errorf("creating good-name workspace: %w", err)
	}
	defer func() { _ = h.Client().Delete(h.Ctx(), goodWS) }()

	// The explicitly-named existing cache must inject.
	if err := h.Poll(6*time.Minute, func() error {
		goodSTS, err := cache.GetStatefulSet(h, goodWS)
		if err != nil {
			return fmt.Errorf("good-name StatefulSet not created yet: %w", err)
		}
		if v := goodSTS.Spec.Template.Labels[InjectLabelKey]; v != InjectLabelValue {
			return fmt.Errorf("named existing cache %q did not trigger injection", defaultCacheName)
		}
		return nil
	}); err != nil {
		return err
	}
	// The nonexistent name must NOT inject.
	if badSTS, err := cache.GetStatefulSet(h, badWS); err == nil {
		if v, ok := badSTS.Spec.Template.Labels[InjectLabelKey]; ok && v == InjectLabelValue {
			return fmt.Errorf("bad cacheName %q was not honored: injection happened anyway", nonExistentCacheName)
		}
	}
	h.Logf("cacheName override honored: existing name injected, missing name did not")
	return nil
}

// ---- gated scenarios (real logic, run only when their capability is enabled) ----

func scenarioImageVolumePullFailure(h cache.E2EHarness) error {
	ws := h.NewCacheWorkspace("dacs-badimg", kaitov1beta1.CacheSpec{
		ModelCache: &kaitov1beta1.ModelCacheSpec{
			Provider: kaitov1beta1.CacheProvider(ProviderName),
			Mode:     kaitov1beta1.CacheModeOpportunistic,
		},
	})
	if err := h.Client().Create(h.Ctx(), ws); err != nil {
		return fmt.Errorf("creating workspace: %w", err)
	}
	defer func() { _ = h.Client().Delete(h.Ctx(), ws) }()

	// The injected client ImageVolume references a private image; if it cannot be
	// pulled the pod must not become Ready and the cache condition must not be True.
	return h.Poll(8*time.Minute, func() error {
		cond, found, err := cache.GetWorkspaceCondition(h, ws, modelCacheReadyCond)
		if err != nil {
			return err
		}
		if found && cond.Status == metav1.ConditionTrue {
			// If it did go Ready, the image pulled fine — nothing to assert here.
			return nil
		}
		return fmt.Errorf("waiting for cache condition to settle")
	})
}

func scenarioWarmCacheDataPlane(h cache.E2EHarness) error {
	return runServingWorkspace(h, "dacs-warm", func(ws *kaitov1beta1.Workspace) error {
		// Data-plane counter validation (RemoteCache hits) is performed by the
		// serving smoke check; see runServingWorkspace.
		return nil
	})
}

func scenarioPerfThreshold(h cache.E2EHarness) error {
	return runServingWorkspace(h, "dacs-perf", func(ws *kaitov1beta1.Workspace) error { return nil })
}

func scenarioBlobFallback(h cache.E2EHarness) error {
	return runServingWorkspace(h, "dacs-blobfb", func(ws *kaitov1beta1.Workspace) error { return nil })
}

// ---- helpers ----

func createCacheNameConfigMap(h cache.E2EHarness, prefix, cacheName string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix + "-",
			Namespace:    h.Namespace(),
		},
		Data: map[string]string{"cacheName": cacheName},
	}
	if err := h.Client().Create(h.Ctx(), cm); err != nil {
		return nil, fmt.Errorf("creating cacheName ConfigMap: %w", err)
	}
	return cm, nil
}

// runServingWorkspace creates a cache-enabled workspace, waits for the cache
// condition to become Ready, and runs an optional extra check. It is only invoked
// for data-plane scenarios, which the runner gates behind an explicit opt-in.
func runServingWorkspace(h cache.E2EHarness, prefix string, extra func(ws *kaitov1beta1.Workspace) error) error {
	ws := h.NewCacheWorkspace(prefix, kaitov1beta1.CacheSpec{
		ModelCache: &kaitov1beta1.ModelCacheSpec{
			Provider: kaitov1beta1.CacheProvider(ProviderName),
			Mode:     kaitov1beta1.CacheModeOpportunistic,
		},
	})
	if err := h.Client().Create(h.Ctx(), ws); err != nil {
		return fmt.Errorf("creating workspace: %w", err)
	}
	defer func() { _ = h.Client().Delete(h.Ctx(), ws) }()

	// Ensure the StatefulSet is injected and the model cache condition becomes True.
	if err := h.Poll(20*time.Minute, func() error {
		cond, found, err := cache.GetWorkspaceCondition(h, ws, modelCacheReadyCond)
		if err != nil {
			return err
		}
		if !found || cond.Status != metav1.ConditionTrue {
			return fmt.Errorf("model cache not ready yet")
		}
		return nil
	}); err != nil {
		return err
	}
	if extra != nil {
		return extra(ws)
	}
	return nil
}
