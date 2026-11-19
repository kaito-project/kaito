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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kaito-project/kaito/internal/manifests"
)

const defaultAdapterVolumePath = "/mnt/adapter"

// inferenceConfigKey is the ConfigMap entry holding vLLM runtime overrides.
const inferenceConfigKey = "inference_config.yaml"

// hostPathVolume returns a read-only Volume/VolumeMount pair that mounts a
// directory from the host node into the container at the same path. It is used
// to expose data baked into a custom GPU node image (e.g. model weights or a
// CUDA toolkit) to the inference container.
func hostPathVolume(name, hostPath string) (corev1.Volume, corev1.VolumeMount) {
	dirType := corev1.HostPathDirectory
	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: hostPath,
				Type: &dirType,
			},
		},
	}
	volumeMount := corev1.VolumeMount{
		Name:      name,
		MountPath: hostPath,
		ReadOnly:  true,
	}
	return volume, volumeMount
}

// inferenceConfigCMVolume mounts only the runtime configuration
// (inference_config.yaml) from the inference ConfigMap. A bring-your-own model
// shares this ConfigMap with its config.json, which is consumed by the operator
// for sizing and must not appear beside the authoritative config.json that
// arrives with the model bundle. The key is projected optionally because the
// model configuration alone is a valid ConfigMap for a custom model.
func inferenceConfigCMVolume(cmName string) (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: "config-volume",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cmName,
				},
				Items: []corev1.KeyToPath{
					{Key: inferenceConfigKey, Path: inferenceConfigKey},
				},
				Optional: ptr.To(true),
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      volume.Name,
		MountPath: manifests.DefaultConfigMapMountPath,
	}
	return volume, volumeMount
}

func configAdapterVolume(inputVolumeSource *corev1.VolumeSource) (corev1.Volume, corev1.VolumeMount) {
	var volume corev1.Volume
	var volumeMount corev1.VolumeMount
	var volumeSource corev1.VolumeSource
	if inputVolumeSource != nil {
		volumeSource = *inputVolumeSource
	} else {
		volumeSource = corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}

	volume = corev1.Volume{
		Name:         "adapter-volume",
		VolumeSource: volumeSource,
	}

	volumeMount = corev1.VolumeMount{
		Name:      "adapter-volume",
		MountPath: defaultAdapterVolumePath,
	}
	return volume, volumeMount
}
