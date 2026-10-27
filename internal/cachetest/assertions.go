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

package cachetest

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/pkg/cache"
)

// AssertMutations validates raw pod mutations against an expectation.
func AssertMutations(m *cache.PodMutations, exp MutationExpectation) []error {
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
		return errs
	}

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

	envNames := make(map[string]struct{}, len(m.EnvVars))
	for _, e := range m.EnvVars {
		envNames[e.Name] = struct{}{}
	}
	for _, name := range exp.RequiredEnvVars {
		if _, ok := envNames[name]; !ok {
			errs = append(errs, fmt.Errorf("missing required env var %q", name))
		}
	}

	volumeNames := make(map[string]struct{}, len(m.Volumes))
	for _, volume := range m.Volumes {
		volumeNames[volume.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumes {
		if _, ok := volumeNames[name]; !ok {
			errs = append(errs, fmt.Errorf("missing required volume %q", name))
		}
	}

	mountNames := make(map[string]struct{}, len(m.VolumeMounts))
	for _, mount := range m.VolumeMounts {
		mountNames[mount.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumeMounts {
		if _, ok := mountNames[name]; !ok {
			errs = append(errs, fmt.Errorf("missing required volume mount %q", name))
		}
	}

	if exp.Validate != nil {
		errs = append(errs, exp.Validate(m)...)
	}

	return errs
}

// AssertPodSpec validates deployed pod mutations against an expectation.
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

	volumeNames := make(map[string]struct{}, len(spec.Volumes))
	for _, volume := range spec.Volumes {
		volumeNames[volume.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumes {
		if _, ok := volumeNames[name]; !ok {
			errs = append(errs, fmt.Errorf("pod spec missing required volume %q", name))
		}
	}

	mountNames := make(map[string]struct{}, len(spec.Containers[0].VolumeMounts))
	for _, mount := range spec.Containers[0].VolumeMounts {
		mountNames[mount.Name] = struct{}{}
	}
	for _, name := range exp.RequiredVolumeMounts {
		if _, ok := mountNames[name]; !ok {
			errs = append(errs, fmt.Errorf("model container missing required volume mount %q", name))
		}
	}

	return errs
}
