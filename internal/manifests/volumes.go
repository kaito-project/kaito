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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

const shmMountPath = "/dev/shm"

// DefaultConfigMapMountPath is where workload ConfigMaps are mounted.
const DefaultConfigMapMountPath = "/mnt/config"

// DefaultModelWeightsVolume is the shared emptyDir that holds model weights.
var DefaultModelWeightsVolume = corev1.Volume{
	Name: "model-weights-volume",
	VolumeSource: corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	},
}

// DefaultModelWeightsVolumeMount mounts DefaultModelWeightsVolume at the weights path.
var DefaultModelWeightsVolumeMount = corev1.VolumeMount{
	Name:      "model-weights-volume",
	MountPath: pkgmodel.DefaultWeightsVolumePath,
}

// ConfigImagePullSecretVolume returns a docker-config volume projected from imagePullSecrets.
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

// ConfigSHMVolume returns an in-memory /dev/shm volume for shared memory.
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
		MountPath: shmMountPath,
	}

	return volume, volumeMount
}
