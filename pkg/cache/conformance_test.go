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

package cache_test

// The blank import of internal/cachetest/noop below self-registers the fixture's
// conformance Expectations via init(). Adding a new provider package to the
// import list is all that is required for the registry-driven suite to discover it.
import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/cachetest"
	_ "github.com/kaito-project/kaito/internal/cachetest/dacs"
	_ "github.com/kaito-project/kaito/internal/cachetest/noop"
	"github.com/kaito-project/kaito/pkg/cache"
)

// TestProviderConformance_AutoDiscovered iterates every registered provider's
// Expectations and verifies the provider satisfies the shared PodMutations contract
// plus its own declared, provider-specific expectations. Because it is driven by the
// registry, any newly added provider is automatically included — and a provider that
// registers without a usable conformance factory fails the suite closed.
func TestProviderConformance_AutoDiscovered(t *testing.T) {
	exps := cachetest.ListExpectations()
	if len(exps) == 0 {
		t.Fatal("no cache provider expectations registered; expected at least noop")
	}
	t.Logf("discovered %d cache provider(s) for conformance", len(exps))

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Name: "conformance-ws", Namespace: "default"},
	}

	for _, e := range exps {
		e := e
		t.Run(string(e.Provider), func(t *testing.T) {
			// Fail closed: a discovered provider must supply a conformance factory.
			if e.NewForConformance == nil {
				t.Fatalf("provider %q registered Expectations without NewForConformance", e.Provider)
			}

			p := e.NewForConformance()
			if p == nil {
				t.Fatalf("provider %q NewForConformance returned nil", e.Provider)
			}
			if got := cache.ProviderName(p.Name()); got != e.Provider {
				t.Fatalf("provider name mismatch: instance %q, expectations %q", got, e.Provider)
			}

			concerns := []cache.CacheConcern{
				cache.CacheConcernModelWeights,
				cache.CacheConcernKVCache,
			}
			for _, concern := range concerns {
				exp := e.ForConcern(concern)
				if !exp.Supported {
					continue
				}
				t.Run(string(concern), func(t *testing.T) {
					m, err := p.PodMutations(context.Background(), concern, ws, "test-model", "main", nil)
					if err != nil {
						t.Fatalf("PodMutations(%s) returned error: %v", concern, err)
					}
					if m == nil {
						t.Fatalf("PodMutations(%s) returned nil mutations", concern)
					}
					for _, assertErr := range cachetest.AssertMutations(m, exp) {
						t.Errorf("[%s/%s] %v", e.Provider, concern, assertErr)
					}
				})
			}
		})
	}
}

// TestAllProviderDirectoriesRegisterExpectations enforces that every subdirectory
// under pkg/cache/ (which represents a cache provider) has registered its
// conformance Expectations. This prevents a new provider from being added without
// declaring its E2E contract — the common E2E scenarios automatically run against
// all registered providers, so missing registration means silent E2E coverage gaps.
//
// To satisfy this test, a new provider must:
//  1. Create a package under pkg/cache/<provider-name>/
//  2. Add test support under internal/cachetest/<provider-name>/ that calls
//     cachetest.RegisterExpectations(...) in its init()
//  3. Blank-import that test-support package in this file
func TestAllProviderDirectoriesRegisterExpectations(t *testing.T) {
	// Go test runs from the package directory, so subdirs are direct children.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read pkg/cache directory: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := entry.Name()

		// A provider directory must contain at least one .go file.
		goFiles, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		if len(goFiles) == 0 {
			continue
		}

		provider := cache.ProviderName(dir)
		exp, ok := cachetest.GetExpectations(provider)
		if !ok {
			t.Errorf("provider directory %q exists under pkg/cache/ but did not register "+
				"Expectations via cachetest.RegisterExpectations(). Every provider must declare "+
				"its conformance contract so the common E2E scenarios are exercised.", dir)
			continue
		}
		if exp.NewForConformance == nil {
			t.Errorf("provider %q registered Expectations but NewForConformance is nil; "+
				"a conformance factory is required for offline mutation testing", dir)
		}
	}
}

// TestAssertPodSpec exercises the AssertPodSpec conformance helper directly.
// AssertPodSpec validates a deployed pod (template labels + pod spec) against a
// MutationExpectation and is consumed by provider e2e suites; this unit test
// pins its contract — required labels, env vars, volumes, and volume mounts are
// detected on the first (model) container, and violations are reported.
func TestAssertPodSpec(t *testing.T) {
	exp := cachetest.MutationExpectation{
		Supported:            true,
		RequiredLabels:       map[string]string{"cache": "on"},
		RequiredEnvVars:      []string{"CACHE_ENV"},
		RequiredVolumes:      []string{"cache-vol"},
		RequiredVolumeMounts: []string{"cache-mount"},
	}

	compliant := &corev1.PodSpec{
		Containers: []corev1.Container{{
			Name:         "model",
			Env:          []corev1.EnvVar{{Name: "CACHE_ENV", Value: "1"}},
			VolumeMounts: []corev1.VolumeMount{{Name: "cache-mount", MountPath: "/cache"}},
		}},
		Volumes: []corev1.Volume{{Name: "cache-vol"}},
	}

	if errs := cachetest.AssertPodSpec(map[string]string{"cache": "on"}, compliant, exp); len(errs) != 0 {
		t.Fatalf("expected compliant pod spec to pass, got errors: %v", errs)
	}

	// A pod missing every requirement must surface one error per missing item
	// (label, env var, volume, volume mount).
	empty := &corev1.PodSpec{Containers: []corev1.Container{{Name: "model"}}}
	errs := cachetest.AssertPodSpec(map[string]string{}, empty, exp)
	if len(errs) != 4 {
		t.Fatalf("expected 4 violations for empty pod spec, got %d: %v", len(errs), errs)
	}
}
