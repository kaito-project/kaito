// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package utils

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultVolumeMountPath    = "/dev/shm"
	DefaultConfigMapMountPath = "/mnt/config"
	DefaultDataVolumePath     = "/mnt/data"
	DefaultAdapterVolumePath  = "/mnt/adapter"
)

func ConfigResultsVolume(outputPath string) (corev1.Volume, corev1.VolumeMount) {
	sharedWorkspaceVolume := corev1.Volume{
		Name: "results-volume",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	sharedVolumeMount := corev1.VolumeMount{
		Name:      "results-volume",
		MountPath: outputPath,
	}
	return sharedWorkspaceVolume, sharedVolumeMount
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
		MountPath: "/tmp/.docker/config",
	}

	return volume, volumeMount
}

func ConfigSHMVolume(instanceCount int) (corev1.Volume, corev1.VolumeMount) {
	volume := corev1.Volume{}
	volumeMount := corev1.VolumeMount{}

	// Signifies multinode inference requirement
	if instanceCount > 1 {
		// Append share memory volume to any existing volumes
		volume = corev1.Volume{
			Name: "dshm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: "Memory",
				},
			},
		}

		volumeMount = corev1.VolumeMount{
			Name:      volume.Name,
			MountPath: DefaultVolumeMountPath,
		}
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

func ConfigDataVolume(hostPath *string) (corev1.Volume, corev1.VolumeMount) {
	var volume corev1.Volume
	var volumeMount corev1.VolumeMount
	var volumeSource corev1.VolumeSource
	if hostPath != nil {
		volumeSource = corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: *hostPath,
			},
		}
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
