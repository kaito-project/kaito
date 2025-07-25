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
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultVolumeMountPath    = "/dev/shm"
	DefaultConfigMapMountPath = "/mnt/config"
	DefaultDataVolumePath     = "/mnt/data"
	DefaultAdapterVolumePath  = "/mnt/adapter"
	DefaultWeightsVolumePath  = "/workspace/weights"

	DefaultORASToolImage = "mcr.microsoft.com/oss/v2/oras-project/oras:v1.2.3"
)

var DefaultModelWeightsVolume = corev1.Volume{
	Name: "model-weights-volume",
	VolumeSource: corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	},
}
var DefaultModelWeightsVolumeMount = corev1.VolumeMount{
	Name:      "model-weights-volume",
	MountPath: DefaultWeightsVolumePath,
}

func ConfigResultsVolume(outputPath string, outputVolume *corev1.VolumeSource) (corev1.Volume, corev1.VolumeMount) {
	sharedWorkspaceVolume := corev1.Volume{
		Name: "results-volume",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	if outputVolume != nil {
		sharedWorkspaceVolume.VolumeSource = *outputVolume
	}
	sharedVolumeMount := corev1.VolumeMount{
		Name:      "results-volume",
		MountPath: outputPath,
	}
	return sharedWorkspaceVolume, sharedVolumeMount
}

func FindResultsVolumeMount(spec *corev1.PodSpec) *corev1.VolumeMount {
	for _, container := range spec.Containers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == "results-volume" {
				return &volumeMount
			}
		}
	}
	return nil
}

func ConfigImagePullSecretVolume(nameSuffix string, imagePullSecrets []string) (corev1.Volume, corev1.VolumeMount) {
	name := fmt.Sprintf("docker-config-%s", nameSuffix)

	sources := make([]corev1.VolumeProjection, 0, len(imagePullSecrets))
	for _, imagePullSecret := range imagePullSecrets {
		volumeProjection := corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: imagePullSecret,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  ".dockerconfigjson",
						Path: fmt.Sprintf("%s.json", imagePullSecret),
					},
				},
			},
		}

		sources = append(sources, volumeProjection)
	}

	volume := corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: sources,
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      name,
		MountPath: fmt.Sprintf("/root/.docker/config.d/%s", nameSuffix),
	}

	return volume, volumeMount
}

func ConfigImagePushSecretVolume(imagePushSecret string) (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: "docker-config",
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: imagePushSecret,
							},
							Items: []corev1.KeyToPath{
								{
									Key:  ".dockerconfigjson",
									Path: "config.json",
								},
							},
						},
					},
				},
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      "docker-config",
		MountPath: "/root/.docker",
	}

	return volume, volumeMount
}

func ConfigSHMVolume() (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: "dshm",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: "Memory",
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      volume.Name,
		MountPath: DefaultVolumeMountPath,
	}

	return volume, volumeMount
}

func ConfigCMVolume(cmName string) (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{
		Name: "config-volume",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cmName,
				},
			},
		},
	}

	volumeMount := corev1.VolumeMount{
		Name:      volume.Name,
		MountPath: DefaultConfigMapMountPath,
	}
	return volume, volumeMount
}

func ConfigDataVolume(inputVolumeSource *corev1.VolumeSource) (corev1.Volume, corev1.VolumeMount) {
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
		Name:         "data-volume",
		VolumeSource: volumeSource,
	}

	volumeMount = corev1.VolumeMount{
		Name:      "data-volume",
		MountPath: DefaultDataVolumePath,
	}
	return volume, volumeMount
}

func ConfigAdapterVolume() (corev1.Volume, corev1.VolumeMount) {
	var volume corev1.Volume
	var volumeMount corev1.VolumeMount

	volumeSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}

	volume = corev1.Volume{
		Name:         "adapter-volume",
		VolumeSource: volumeSource,
	}

	volumeMount = corev1.VolumeMount{
		Name:      "adapter-volume",
		MountPath: DefaultAdapterVolumePath,
	}
	return volume, volumeMount
}

func GetPresetImageName(name, tag string) string {
	return fmt.Sprintf("%s/kaito-%s:%s",
		os.Getenv("PRESET_REGISTRY_NAME"),
		name, tag)
}
