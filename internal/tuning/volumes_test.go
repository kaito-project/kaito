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

package tuning

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/internal/manifests"
)

func TestConfigResultsVolume(t *testing.T) {
	t.Run("nil output volume uses EmptyDir", func(t *testing.T) {
		vol, mount := configResultsVolume("/results", nil)
		if vol.Name != "results-volume" {
			t.Errorf("expected volume name 'results-volume', got %q", vol.Name)
		}
		if vol.VolumeSource.EmptyDir == nil {
			t.Error("expected EmptyDir volume source")
		}
		if mount.MountPath != "/results" {
			t.Errorf("expected mount path '/results', got %q", mount.MountPath)
		}
	})

	t.Run("non-nil output volume is used", func(t *testing.T) {
		hostPath := "/host/path"
		vol, mount := configResultsVolume("/results", &corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: hostPath},
		})
		if vol.VolumeSource.HostPath == nil || vol.VolumeSource.HostPath.Path != hostPath {
			t.Errorf("expected HostPath volume source with path %q", hostPath)
		}
		if mount.MountPath != "/results" {
			t.Errorf("expected mount path '/results', got %q", mount.MountPath)
		}
	})
}

func TestFindResultsVolumeMount(t *testing.T) {
	t.Run("found in first container", func(t *testing.T) {
		spec := &corev1.PodSpec{
			Containers: []corev1.Container{
				{
					VolumeMounts: []corev1.VolumeMount{
						{Name: "other-volume", MountPath: "/other"},
						{Name: "results-volume", MountPath: "/results"},
					},
				},
			},
		}
		mount := findResultsVolumeMount(spec)
		if mount == nil {
			t.Fatal("expected to find results-volume mount")
		}
		if mount.MountPath != "/results" {
			t.Errorf("expected mount path '/results', got %q", mount.MountPath)
		}
	})

	t.Run("not found returns nil", func(t *testing.T) {
		spec := &corev1.PodSpec{
			Containers: []corev1.Container{
				{VolumeMounts: []corev1.VolumeMount{{Name: "other-vol", MountPath: "/x"}}},
			},
		}
		if got := findResultsVolumeMount(spec); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
}

func TestConfigImagePushSecretVolume(t *testing.T) {
	vol, mount := configImagePushSecretVolume("my-secret")
	if vol.Name != "docker-config" {
		t.Errorf("unexpected volume name %q", vol.Name)
	}
	if vol.VolumeSource.Projected == nil {
		t.Fatal("expected Projected volume source")
	}
	if mount.MountPath != "/root/.docker" {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
}

func TestConfigCMVolume(t *testing.T) {
	vol, mount := configCMVolume("my-configmap")
	if vol.VolumeSource.ConfigMap == nil {
		t.Fatal("expected ConfigMap volume source")
	}
	if vol.VolumeSource.ConfigMap.Name != "my-configmap" {
		t.Errorf("unexpected configmap name %q", vol.VolumeSource.ConfigMap.Name)
	}
	if mount.MountPath != manifests.DefaultConfigMapMountPath {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
	// The whole ConfigMap must be projected: tuning relies on training_config.yaml
	// being mounted, so no key filtering may be applied here.
	if vol.VolumeSource.ConfigMap.Items != nil {
		t.Errorf("expected the whole ConfigMap to be projected, got items %v", vol.VolumeSource.ConfigMap.Items)
	}
}

func TestConfigDataVolume(t *testing.T) {
	t.Run("nil input uses EmptyDir", func(t *testing.T) {
		vol, mount := configDataVolume(nil)
		if vol.VolumeSource.EmptyDir == nil {
			t.Error("expected EmptyDir volume source")
		}
		if mount.MountPath != defaultDataVolumePath {
			t.Errorf("unexpected mount path %q", mount.MountPath)
		}
	})

	t.Run("custom volume source is used", func(t *testing.T) {
		hostPath := "/data"
		vol, mount := configDataVolume(&corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: hostPath},
		})
		if vol.VolumeSource.HostPath == nil || vol.VolumeSource.HostPath.Path != hostPath {
			t.Errorf("expected HostPath volume with path %q", hostPath)
		}
		if mount.MountPath != defaultDataVolumePath {
			t.Errorf("expected mount path %q, got %q", defaultDataVolumePath, mount.MountPath)
		}
	})
}
