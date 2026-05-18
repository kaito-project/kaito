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
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMergeMutations_DeduplicatesEnvVars(t *testing.T) {
	dst := &PodMutations{
		EnvVars: []corev1.EnvVar{
			{Name: "A", Value: "1"},
			{Name: "B", Value: "2"},
		},
	}
	src := &PodMutations{
		EnvVars: []corev1.EnvVar{
			{Name: "B", Value: "overridden"}, // duplicate, should be skipped
			{Name: "C", Value: "3"},
		},
	}

	mergeMutations(dst, src)

	if len(dst.EnvVars) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(dst.EnvVars))
	}
	// B should keep original value (first wins)
	for _, e := range dst.EnvVars {
		if e.Name == "B" && e.Value != "2" {
			t.Errorf("expected B=2 (first wins), got B=%s", e.Value)
		}
	}
}

func TestMergeMutations_NilSrc(t *testing.T) {
	dst := &PodMutations{
		EnvVars: []corev1.EnvVar{{Name: "A", Value: "1"}},
	}
	mergeMutations(dst, nil)

	if len(dst.EnvVars) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(dst.EnvVars))
	}
}

func TestApplyMutations(t *testing.T) {
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "model",
				Env:  []corev1.EnvVar{{Name: "EXISTING", Value: "yes"}},
			},
		},
	}

	mutations := &PodMutations{
		EnvVars: []corev1.EnvVar{
			{Name: "CACHE_ENABLED", Value: "true"},
		},
		Volumes: []corev1.Volume{
			{Name: "cache-vol"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "cache-vol", MountPath: "/cache"},
		},
		InitContainers: []corev1.Container{
			{Name: "cache-init"},
		},
	}

	applyMutations(spec, mutations)

	if len(spec.Containers[0].Env) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(spec.Containers[0].Env))
	}
	if len(spec.Containers[0].VolumeMounts) != 1 {
		t.Errorf("expected 1 volume mount, got %d", len(spec.Containers[0].VolumeMounts))
	}
	if len(spec.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(spec.Volumes))
	}
	if len(spec.InitContainers) != 1 {
		t.Errorf("expected 1 init container, got %d", len(spec.InitContainers))
	}
}

func TestApplyMutations_EmptyContainers(t *testing.T) {
	spec := &corev1.PodSpec{}
	mutations := &PodMutations{
		EnvVars: []corev1.EnvVar{{Name: "A", Value: "1"}},
	}

	// Should not panic with empty containers
	applyMutations(spec, mutations)

	if len(spec.Containers) != 0 {
		t.Errorf("expected no containers modified")
	}
}
