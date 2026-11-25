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

// Package accelerator renders the Kubernetes objects that differ by
// accelerator vendor. Each vendor implements Driver in a subpackage.
package accelerator

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/accelerator/cpu"
	"github.com/kaito-project/kaito/internal/accelerator/nvidia"
	"github.com/kaito-project/kaito/pkg/accelerator"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

// Driver renders the vendor-specific parts of a workload.
type Driver interface {
	Type() accelerator.Type
	// ResourceRequirements requests count devices. A non-empty profile
	// requests partitions of that profile instead of whole devices.
	ResourceRequirements(count int, profile string) corev1.ResourceRequirements
	// Tolerations tolerates the taints on this accelerator's nodes.
	Tolerations() []corev1.Toleration
	// PartitionTolerations tolerates the taint on nodes that expose profile.
	PartitionTolerations(profile string) []corev1.Toleration
	// SharedDeviceEnv exposes the node's devices to a container that has no
	// device request of its own.
	SharedDeviceEnv() []corev1.EnvVar
	// InferenceBaseImage returns the inference base image for runtime, or an
	// error when this accelerator does not support the runtime.
	InferenceBaseImage(runtime pkgmodel.RuntimeName) (pkgmodel.Metadata, error)
	// ExclusiveNodeLabels labels pods that must not share a node with each
	// other. It is nil when device requests already keep them apart.
	ExclusiveNodeLabels() map[string]string
}

var drivers = map[accelerator.Type]Driver{
	accelerator.TypeNVIDIA: nvidia.Driver{},
	accelerator.TypeCPU:    cpu.Driver{},
	// AMD and Neuron have no driver yet. They keep the NVIDIA driver, which is
	// how every GPU SKU was rendered before drivers existed.
	accelerator.TypeAMD:    nvidia.Driver{},
	accelerator.TypeNeuron: nvidia.Driver{},
}

// ForType returns the driver for t. An unset type means NVIDIA, the
// accelerator of every GPUConfig built before the field existed.
func ForType(t accelerator.Type) Driver {
	if d, ok := drivers[t]; ok {
		return d
	}
	return drivers[accelerator.TypeNVIDIA]
}

// For returns the driver for an inference target. A nil config means CPU.
func For(cfg *sku.GPUConfig) Driver {
	if cfg == nil {
		return ForType(accelerator.TypeCPU)
	}
	return ForType(cfg.Accelerator)
}

// ForWorkspace returns the driver for the target ws declares, without reading
// nodes. A GPU target whose instance type is not a known SKU uses NVIDIA.
func ForWorkspace(ctx context.Context, ws *v1beta1.Workspace) Driver {
	if v1beta1.IsCPUInferenceWorkspace(ctx, ws) {
		return ForType(accelerator.TypeCPU)
	}
	if ws == nil {
		return ForType(accelerator.TypeNVIDIA)
	}
	if cfg, err := sku.GetGPUConfigBySKU(ws.Resource.InstanceType); err == nil {
		return For(cfg)
	}
	return ForType(accelerator.TypeNVIDIA)
}
