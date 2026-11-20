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

package manifests

import (
	"testing"
)

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

func TestConfigSHMVolume(t *testing.T) {
	vol, mount := ConfigSHMVolume()
	if vol.Name != "dshm" {
		t.Errorf("unexpected volume name %q", vol.Name)
	}
	if vol.VolumeSource.EmptyDir == nil || vol.VolumeSource.EmptyDir.Medium != "Memory" {
		t.Error("expected EmptyDir with Memory medium")
	}
	if mount.MountPath != shmMountPath {
		t.Errorf("unexpected mount path %q", mount.MountPath)
	}
}
