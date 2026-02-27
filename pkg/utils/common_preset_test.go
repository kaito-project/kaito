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

package utils

import (
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGetPresetImageName(t *testing.T) {
	type args struct {
		registry string
		name     string
		tag      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "all parameters provided",
			args: args{
				registry: "myregistry.com",
				name:     "phi-3",
				tag:      "v1.0",
			},
			want: "myregistry.com/kaito-phi-3:v1.0",
		},
		{
			name: "empty registry uses environment variable",
			args: args{
				registry: "",
				name:     "llama-2",
				tag:      "latest",
			},
			want: "test-registry.io/kaito-llama-2:latest",
		},
		{
			name: "registry with different format",
			args: args{
				registry: "docker.io/kaito",
				name:     "falcon",
				tag:      "7b",
			},
			want: "docker.io/kaito/kaito-falcon:7b",
		},
		{
			name: "complex model name with hyphens",
			args: args{
				registry: "azurecr.io",
				name:     "phi-3-mini-128k-instruct",
				tag:      "v2.1.3",
			},
			want: "azurecr.io/kaito-phi-3-mini-128k-instruct:v2.1.3",
		},
		{
			name: "minimal case",
			args: args{
				registry: "reg",
				name:     "m",
				tag:      "t",
			},
			want: "reg/kaito-m:t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variable for the test case that needs it
			if tt.name == "empty registry uses environment variable" {
				originalValue := os.Getenv("PRESET_REGISTRY_NAME")
				os.Setenv("PRESET_REGISTRY_NAME", "test-registry.io")
				defer func() {
					if originalValue == "" {
						os.Unsetenv("PRESET_REGISTRY_NAME")
					} else {
						os.Setenv("PRESET_REGISTRY_NAME", originalValue)
					}
				}()
			}

			if got := GetPresetImageName(tt.args.registry, tt.args.name, tt.args.tag); got != tt.want {
				t.Errorf("GetPresetImageName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigResultsVolume(t *testing.T) {
	t.Run("nil output volume uses EmptyDir", func(t *testing.T) {
		vol, mount := ConfigResultsVolume("/results", nil)
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
		vol, mount := ConfigResultsVolume("/results", &corev1.VolumeSource{
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
		mount := FindResultsVolumeMount(spec)
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
		if got := FindResultsVolumeMount(spec); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
}

func TestConfigImagePullSecretVolume(t *testing.T) {
	secrets := []string{"secret-a", "secret-b"}
	vol, mount := ConfigImagePullSecretVolume("suffix", secrets)

	if vol.Name != "docker-config-suffix" {
		t.Errorf("unexpected volume name %q", vol.Name)
	}
	if vol.VolumeSource.Projected == nil {
		t.Fatal("expected Projected volume source")
	}
	if len(vol.VolumeSource.Projected.Sources) != 2 {
		t.Errorf("expected 2 projected sources, got %d", len(vol.VolumeSource.Projected.Sources))
	}
	if mount.MountPath != "/root/.docker/config.d/suffix" {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
}

func TestConfigImagePushSecretVolume(t *testing.T) {
	vol, mount := ConfigImagePushSecretVolume("my-secret")
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

func TestConfigSHMVolume(t *testing.T) {
	vol, mount := ConfigSHMVolume()
	if vol.Name != "dshm" {
		t.Errorf("unexpected volume name %q", vol.Name)
	}
	if vol.VolumeSource.EmptyDir == nil || vol.VolumeSource.EmptyDir.Medium != "Memory" {
		t.Error("expected EmptyDir with Memory medium")
	}
	if mount.MountPath != DefaultVolumeMountPath {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
}

func TestConfigCMVolume(t *testing.T) {
	vol, mount := ConfigCMVolume("my-configmap")
	if vol.VolumeSource.ConfigMap == nil {
		t.Fatal("expected ConfigMap volume source")
	}
	if vol.VolumeSource.ConfigMap.Name != "my-configmap" {
		t.Errorf("unexpected configmap name %q", vol.VolumeSource.ConfigMap.Name)
	}
	if mount.MountPath != DefaultConfigMapMountPath {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
}

func TestConfigDataVolume(t *testing.T) {
	t.Run("nil input uses EmptyDir", func(t *testing.T) {
		vol, mount := ConfigDataVolume(nil)
		if vol.VolumeSource.EmptyDir == nil {
			t.Error("expected EmptyDir volume source")
		}
		if mount.MountPath != DefaultDataVolumePath {
			t.Errorf("unexpected mount path %q", mount.MountPath)
		}
	})

	t.Run("custom volume source is used", func(t *testing.T) {
		hostPath := "/data"
		vol, mount := ConfigDataVolume(&corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: hostPath},
		})
		if vol.VolumeSource.HostPath == nil || vol.VolumeSource.HostPath.Path != hostPath {
			t.Errorf("expected HostPath volume with path %q", hostPath)
		}
		if mount.MountPath != DefaultDataVolumePath {
			t.Errorf("expected mount path %q, got %q", DefaultDataVolumePath, mount.MountPath)
		}
	})
}

func TestConfigAdapterVolume(t *testing.T) {
	vol, mount := ConfigAdapterVolume()
	if vol.Name != "adapter-volume" {
		t.Errorf("unexpected volume name %q", vol.Name)
	}
	if vol.VolumeSource.EmptyDir == nil {
		t.Error("expected EmptyDir volume source")
	}
	if mount.MountPath != DefaultAdapterVolumePath {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
}
