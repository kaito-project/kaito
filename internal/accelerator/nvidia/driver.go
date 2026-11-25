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

// Package nvidia implements the accelerator driver for NVIDIA GPUs.
package nvidia

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/accelerator"
	pkgnvidia "github.com/kaito-project/kaito/pkg/accelerator/nvidia"
	"github.com/kaito-project/kaito/pkg/catalog"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

// Driver renders NVIDIA device requests, tolerations and images.
type Driver struct{}

func (Driver) Type() accelerator.Type { return accelerator.TypeNVIDIA }

// ResourceRequirements requests nvidia.com/gpu, or nvidia.com/mig-<profile>
// under the "mixed" MIG strategy, where each profile is its own resource.
func (Driver) ResourceRequirements(count int, profile string) corev1.ResourceRequirements {
	name := corev1.ResourceName(pkgnvidia.ResourceGPU)
	if profile != "" {
		name = corev1.ResourceName(pkgnvidia.MIGResourceName(profile))
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{name: *resource.NewQuantity(int64(count), resource.DecimalSI)},
		Limits:   corev1.ResourceList{name: *resource.NewQuantity(int64(count), resource.DecimalSI)},
	}
}

func (Driver) Tolerations() []corev1.Toleration {
	return []corev1.Toleration{
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpExists,
			Key:      pkgnvidia.ResourceGPU,
		},
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Value:    v1beta1.SKUGPU,
			Key:      v1beta1.LabelSKU,
			Operator: corev1.TolerationOpEqual,
		},
	}
}

// PartitionTolerations tolerates a per-profile MIG taint, if the cluster
// operator sets one (the NVIDIA device plugin does not by default).
func (Driver) PartitionTolerations(profile string) []corev1.Toleration {
	return []corev1.Toleration{{
		Effect:   corev1.TaintEffectNoSchedule,
		Operator: corev1.TolerationOpExists,
		Key:      pkgnvidia.MIGResourceName(profile),
	}}
}

// SharedDeviceEnv exposes every GPU on the node; compute enables CUDA and
// utility provides the driver/NVML discovery APIs.
func (Driver) SharedDeviceEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"},
		{Name: "NVIDIA_DRIVER_CAPABILITIES", Value: "compute,utility"},
	}
}

func (Driver) InferenceBaseImage(pkgmodel.RuntimeName) (pkgmodel.Metadata, error) {
	return catalog.MustGet("base"), nil
}

func (Driver) ExclusiveNodeLabels() map[string]string { return nil }
