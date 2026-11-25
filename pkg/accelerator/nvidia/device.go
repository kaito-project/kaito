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

package nvidia

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/accelerator"
)

const gib = 1024 * 1024 * 1024

// Vendor reads NVIDIA devices from GPU Feature Discovery labels.
type Vendor struct{}

var _ accelerator.Vendor = Vendor{}

func (Vendor) Type() accelerator.Type { return accelerator.TypeNVIDIA }

func (Vendor) CanonicalModel(model string) string { return CanonicalGPUModel(model) }

// DeviceFromNode extracts the GPU configuration from nvidia.com labels on a node.
func (Vendor) DeviceFromNode(node *corev1.Node) (*accelerator.Device, error) {
	gpuProduct, hasGPUProduct := node.Labels[LabelGPUProduct]
	gpuCountStr, hasGPUCount := node.Labels[LabelGPUCount]
	gpuMemoryStr, hasGPUMemory := node.Labels[LabelGPUMemory]

	if !hasGPUProduct || !hasGPUCount || !hasGPUMemory {
		return nil, fmt.Errorf("missing required nvidia.com labels on node %s", node.Name)
	}

	gpuCount, err := strconv.Atoi(gpuCountStr)
	if err != nil {
		return nil, fmt.Errorf("invalid nvidia.com/gpu.count value on node %s: %s", node.Name, gpuCountStr)
	}

	// nvidia.com/gpu.memory is per-GPU memory in MiB.
	gpuMemoryMiB, err := strconv.Atoi(gpuMemoryStr)
	if err != nil {
		return nil, fmt.Errorf("invalid nvidia.com/gpu.memory value on node %s: %s", node.Name, gpuMemoryStr)
	}

	gpuMemGiB := int64((float64(gpuMemoryMiB)/1024)+0.5) * int64(gpuCount)

	var cudaComputeCap float64
	if majorStr, ok := node.Labels[LabelCUDAComputeMajor]; ok {
		if major, err := strconv.Atoi(majorStr); err == nil {
			cudaComputeCap = float64(major)
			if minorStr, ok := node.Labels[LabelCUDAComputeMinor]; ok {
				if minor, err := strconv.Atoi(minorStr); err == nil {
					cudaComputeCap += float64(minor) / 10.0
				}
			}
		}
	}

	return &accelerator.Device{
		Model:             gpuProduct,
		Count:             gpuCount,
		Memory:            *resource.NewQuantity(gpuMemGiB*gib, resource.BinarySI),
		ComputeCapability: cudaComputeCap,
		Partitioned:       isMIGNode(node.Labels),
	}, nil
}

// isMIGNode reports whether the node's GPUs are partitioned into MIG slices:
// a mig.config other than "all-disabled" that reached mig.config.state
// "success".
//
// Only Partitioned is derived here. The MIG profile is left to the spec-driven
// path (Workspace.Resource.Partition), so a node-detected MIG under the
// "single" strategy keeps requesting nvidia.com/gpu.
func isMIGNode(labels map[string]string) bool {
	return HasMIGConfig(labels) && labels[LabelMIGConfigState] == MIGConfigStateSuccess
}

// HasMIGConfig reports whether MIG is requested, including while it is pending
// or failed, so a SKU's full-GPU topology is not used during reconfiguration.
func HasMIGConfig(labels map[string]string) bool {
	migConfig := labels[LabelMIGConfig]
	return migConfig != "" && migConfig != MIGConfigDisabled
}
