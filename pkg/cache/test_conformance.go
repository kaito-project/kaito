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
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// This file holds the provider-agnostic assertion helpers used by both the
// unit-level conformance suite (against raw PodMutations) and the e2e conformance
// suite (against a deployed pod spec). Keeping the assertions here — rather than in
// a _test.go file — lets the e2e (ginkgo) suite reuse exactly the same contract
// checks the unit tests use, so a provider is verified identically offline and online.

// AssertMutations validates a raw PodMutations result against a MutationExpectation.
// It returns a slice of errors describing every unmet expectation (empty when the
// mutations conform).
func AssertMutations(m *PodMutations, exp MutationExpectation) []error {
	var errs []error

	if m == nil {
		if exp.ExpectEmpty {
			return nil
		}
		return []error{fmt.Errorf("expected non-nil PodMutations")}
	}

	if exp.ExpectEmpty {
		if len(m.Labels) != 0 {
			errs = append(errs, fmt.Errorf("expected no labels, got %v", m.Labels))
		}
		if len(m.EnvVars) != 0 {
			errs = append(errs, fmt.Errorf("expected no env vars, got %d", len(m.EnvVars)))
		}
		if len(m.Volumes) != 0 {
			errs = append(errs, fmt.Errorf("expected no volumes, got %d", len(m.Volumes)))
		}
		if len(m.VolumeMounts) != 0 {
			errs = append(errs, fmt.Errorf("expected no volume mounts, got %d", len(m.VolumeMounts)))
		}
		if len(m.InitContainers) != 0 {
			errs = append(errs, fmt.Errorf("expected no init containers, got %d", len(m.InitContainers)))
		}
		return errs
	}

	// Required labels.
	for k, v := range exp.RequiredLabels {
		got, ok := m.Labels[k]
		if !ok {
			errs = append(errs, fmt.Errorf("missing required label %q", k))
			continue
		}
		if got != v {
			errs = append(errs, fmt.Errorf("label %q = %q, want %q", k, got, v))
		}
	}

	// Required env vars.
	envNames := make(map[string]struct{}, len(m.EnvVars))
	for _, e := range m.EnvVars {
		envNames[e.Name] = struct{}{}
	}
	for _, name := range exp.RequiredEnvVars {
		if _, ok := envNames[name]; !ok {
			errs = append(errs, fmt.Errorf("missing required env var %q", name))
		}
	}

	// Required volumes.
	volNames := make(map[string]struct{}, len(m.Volumes))
	for _, v := range m.Volumes {
		volNames[v.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumes {
		if _, ok := volNames[name]; !ok {
			errs = append(errs, fmt.Errorf("missing required volume %q", name))
		}
	}

	// Required volume mounts.
	mountNames := make(map[string]struct{}, len(m.VolumeMounts))
	for _, vm := range m.VolumeMounts {
		mountNames[vm.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumeMounts {
		if _, ok := mountNames[name]; !ok {
			errs = append(errs, fmt.Errorf("missing required volume mount %q", name))
		}
	}

	// Provider-specific deep validation.
	if exp.Validate != nil {
		errs = append(errs, exp.Validate(m)...)
	}

	return errs
}

// AssertPodSpec validates a deployed pod (template labels + pod spec) against a
// MutationExpectation. Env vars and volume mounts are checked on the first (model)
// container. This is used by the e2e conformance suite against a StatefulSet.
//
// ExpectEmpty is not enforced here because a deployed pod legitimately contains
// unrelated labels/env/volumes; emptiness is asserted on raw PodMutations instead.
func AssertPodSpec(templateLabels map[string]string, spec *corev1.PodSpec, exp MutationExpectation) []error {
	var errs []error

	for k, v := range exp.RequiredLabels {
		got, ok := templateLabels[k]
		if !ok {
			errs = append(errs, fmt.Errorf("pod template missing required label %q", k))
			continue
		}
		if got != v {
			errs = append(errs, fmt.Errorf("pod template label %q = %q, want %q", k, got, v))
		}
	}

	if spec == nil || len(spec.Containers) == 0 {
		if len(exp.RequiredEnvVars) > 0 || len(exp.RequiredVolumeMounts) > 0 {
			errs = append(errs, fmt.Errorf("pod spec has no containers to validate env/mounts"))
		}
		return errs
	}

	envNames := make(map[string]struct{}, len(spec.Containers[0].Env))
	for _, e := range spec.Containers[0].Env {
		envNames[e.Name] = struct{}{}
	}
	for _, name := range exp.RequiredEnvVars {
		if _, ok := envNames[name]; !ok {
			errs = append(errs, fmt.Errorf("model container missing required env var %q", name))
		}
	}

	volNames := make(map[string]struct{}, len(spec.Volumes))
	for _, v := range spec.Volumes {
		volNames[v.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumes {
		if _, ok := volNames[name]; !ok {
			errs = append(errs, fmt.Errorf("pod spec missing required volume %q", name))
		}
	}

	mountNames := make(map[string]struct{}, len(spec.Containers[0].VolumeMounts))
	for _, vm := range spec.Containers[0].VolumeMounts {
		mountNames[vm.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumeMounts {
		if _, ok := mountNames[name]; !ok {
			errs = append(errs, fmt.Errorf("model container missing required volume mount %q", name))
		}
	}

	return errs
}

// EnvValue returns the value of the named env var in the mutations, and whether
// it was present. Useful for provider Validate functions.
func (m *PodMutations) EnvValue(name string) (string, bool) {
	for _, e := range m.EnvVars {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}
