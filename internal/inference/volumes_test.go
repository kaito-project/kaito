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

package inference

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// A bring-your-own model shares the inference ConfigMap with its config.json,
// which the operator reads for sizing. Projecting the whole ConfigMap would drop
// that file into the mount path alongside the authoritative config.json shipped
// with the model bundle, where it could silently override the real one.
func TestInferenceConfigCMVolumeProjectsOnlyRuntimeConfig(t *testing.T) {
	vol, _ := inferenceConfigCMVolume("my-configmap")
	src := vol.VolumeSource.ConfigMap

	if len(src.Items) != 1 {
		t.Fatalf("expected exactly one projected key, got %d", len(src.Items))
	}
	if src.Items[0].Key != inferenceConfigKey || src.Items[0].Path != inferenceConfigKey {
		t.Errorf("expected only %q to be projected, got key %q at path %q",
			inferenceConfigKey, src.Items[0].Key, src.Items[0].Path)
	}
	// A custom model may supply only config.json, leaving no runtime settings
	// to project; that must not block the pod from starting.
	if src.Optional == nil || !*src.Optional {
		t.Error("expected the runtime configuration to be optional")
	}
}

func TestConfigAdapterVolume(t *testing.T) {
	t.Run("nil volume source uses EmptyDir", func(t *testing.T) {
		vol, mount := configAdapterVolume(nil)
		if vol.Name != "adapter-volume" {
			t.Errorf("unexpected volume name %q", vol.Name)
		}
		if vol.VolumeSource.EmptyDir == nil {
			t.Error("expected EmptyDir volume source")
		}
		if mount.MountPath != defaultAdapterVolumePath {
			t.Errorf("unexpected mount path %q", mount.MountPath)
		}
	})

	t.Run("provided volume source is used", func(t *testing.T) {
		pvcSource := &corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "test-pvc",
			},
		}
		vol, mount := configAdapterVolume(pvcSource)
		if vol.Name != "adapter-volume" {
			t.Errorf("unexpected volume name %q", vol.Name)
		}
		if vol.VolumeSource.PersistentVolumeClaim == nil {
			t.Error("expected PersistentVolumeClaim volume source")
		}
		if vol.VolumeSource.PersistentVolumeClaim.ClaimName != "test-pvc" {
			t.Errorf("unexpected claim name %q", vol.VolumeSource.PersistentVolumeClaim.ClaimName)
		}
		if vol.VolumeSource.EmptyDir != nil {
			t.Error("did not expect EmptyDir volume source")
		}
		if mount.MountPath != defaultAdapterVolumePath {
			t.Errorf("unexpected mount path %q", mount.MountPath)
		}
	})
}
