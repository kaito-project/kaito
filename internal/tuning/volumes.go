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
	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/internal/manifests"
)

const defaultDataVolumePath = "/mnt/data"

const defaultORASToolImage = "mcr.microsoft.com/oss/v2/oras-project/oras:v1.3.5-2"

func configResultsVolume(outputPath string, outputVolume *corev1.VolumeSource) (corev1.Volume, corev1.VolumeMount) {
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

func findResultsVolumeMount(spec *corev1.PodSpec) *corev1.VolumeMount {
	for _, container := range spec.Containers {
		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == "results-volume" {
				return &volumeMount
			}
		}
	}
	return nil
}

func configImagePushSecretVolume(imagePushSecret string) (corev1.Volume, corev1.VolumeMount) {
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

// configCMVolume mounts the whole tuning ConfigMap at
// manifests.DefaultConfigMapMountPath so every key appears in the pod.
func configCMVolume(cmName string) (corev1.Volume, corev1.VolumeMount) {
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
		MountPath: manifests.DefaultConfigMapMountPath,
	}
	return volume, volumeMount
}

func configDataVolume(inputVolumeSource *corev1.VolumeSource) (corev1.Volume, corev1.VolumeMount) {
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
		MountPath: defaultDataVolumePath,
	}
	return volume, volumeMount
}
