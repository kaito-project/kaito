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

package sku

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/apis"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// DefaultSKUHandler is the package-level CloudSKUHandler used by callers that
// do not want to resolve a handler from the environment on every call. It is
// expected to be initialized once at process startup via GetSKUHandler.
var DefaultSKUHandler CloudSKUHandler = nil

// UnknownSKU identifies GPU configuration that was not resolved from a SKU table.
const UnknownSKU = "unknown"

// GetSKUHandler returns the CloudSKUHandler for the current cloud provider
// as configured via the CLOUD_PROVIDER environment variable.
func GetSKUHandler() (CloudSKUHandler, error) {
	provider := os.Getenv("CLOUD_PROVIDER")

	if provider == "" {
		return nil, apis.ErrMissingField("CLOUD_PROVIDER environment variable must be set")
	}
	skuHandler := GetCloudSKUHandler(provider)
	if skuHandler == nil {
		return nil, apis.ErrInvalidValue(fmt.Sprintf("Unsupported cloud provider %s", provider), "CLOUD_PROVIDER")
	}

	return skuHandler, nil
}

// IsAzureCloudProvider reports whether the current cloud provider is Azure.
func IsAzureCloudProvider() bool {
	return os.Getenv("CLOUD_PROVIDER") == consts.AzureCloudName
}

// GetGPUConfigBySKU returns the GPUConfig for the given instance type using
// the cloud provider configured via the CLOUD_PROVIDER environment variable.
func GetGPUConfigBySKU(instanceType string) (*GPUConfig, error) {
	handler := DefaultSKUHandler
	if handler == nil {
		h, err := GetSKUHandler()
		if err != nil {
			return nil, apis.ErrInvalidValue(fmt.Sprintf("Failed to get SKU handler: %v", err), "sku")
		}
		handler = h
	}

	config := handler.GetGPUConfigBySKU(instanceType)
	if config == nil {
		return nil, apis.ErrInvalidValue(fmt.Sprintf("Unsupported SKU '%s' for cloud provider", instanceType), "sku")
	}

	return config, nil
}

// GetGPUConfigFromNode resolves GPU configuration from the node's instance type
// when it is a known SKU, then falls back to GPU Feature Discovery labels.
// Nodes with MIG configured always use their live labels because the SKU catalog
// describes full physical GPUs rather than the partition topology.
func GetGPUConfigFromNode(node *corev1.Node) (*GPUConfig, error) {
	if !hasMIGConfig(node) {
		if instanceType := node.Labels[corev1.LabelInstanceTypeStable]; instanceType != "" {
			if config, _ := GetGPUConfigBySKU(instanceType); config != nil {
				return config, nil
			}
		}
	}

	return getGPUConfigFromNodeLabels(node)
}

// CanonicalGPUModel maps GPU Feature Discovery product labels to the model names
// used by SKU tables and hardware policy maps.
func CanonicalGPUModel(gpuModel string) string {
	for token := range strings.FieldsFuncSeq(gpuModel, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		switch strings.ToUpper(token) {
		case "H100":
			return "NVIDIA H100"
		case "A100":
			return "NVIDIA A100"
		case "A10":
			return "NVIDIA A10"
		}
	}
	return gpuModel
}

// getGPUConfigFromNodeLabels extracts GPU configuration from nvidia.com labels on a node.
func getGPUConfigFromNodeLabels(node *corev1.Node) (*GPUConfig, error) {
	gpuProduct, hasGPUProduct := node.Labels[consts.NvidiaGPUProduct]
	gpuCountStr, hasGPUCount := node.Labels[consts.NvidiaGPUCount]
	gpuMemoryStr, hasGPUMemory := node.Labels[consts.NvidiaGPUMemory]

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

	// Parse CUDA compute capability from nvidia.com/cuda.compute.major and nvidia.com/cuda.compute.minor labels.
	// These are set by the NVIDIA GPU Feature Discovery (GFD) DaemonSet.
	var cudaComputeCap float64
	if majorStr, ok := node.Labels[consts.NvidiaCUDAComputeCapMajor]; ok {
		if major, err := strconv.Atoi(majorStr); err == nil {
			cudaComputeCap = float64(major)
			if minorStr, ok := node.Labels[consts.NvidiaCUDAComputeCapMinor]; ok {
				if minor, err := strconv.Atoi(minorStr); err == nil {
					cudaComputeCap += float64(minor) / 10.0
				}
			}
		}
	}

	return &GPUConfig{
		SKU:                   UnknownSKU,
		GPUCount:              gpuCount,
		GPUModel:              gpuProduct,
		GPUMem:                *resource.NewQuantity(gpuMemGiB*consts.GiBToBytes, resource.BinarySI),
		CUDAComputeCapability: cudaComputeCap,
		IsMIG:                 isMIGNode(node),
	}, nil
}

// ScaleGPUConfigToCount returns a copy of nodeCfg scaled to count whole GPUs, preserving
// the per-GPU memory (GPUMem/GPUCount); it errors when count exceeds the node's GPUs.
func ScaleGPUConfigToCount(nodeCfg *GPUConfig, count int) (*GPUConfig, error) {
	if count > nodeCfg.GPUCount {
		return nil, fmt.Errorf("partition.count (%d) exceeds GPUs available on node (%d)", count, nodeCfg.GPUCount)
	}
	perGPUMem := nodeCfg.GPUMem.Value() / int64(nodeCfg.GPUCount)
	scaled := *nodeCfg
	scaled.GPUCount = count
	scaled.GPUMem = *resource.NewQuantity(perGPUMem*int64(count), resource.BinarySI)
	return &scaled, nil
}

// isMIGNode reports whether the node's GPUs are partitioned into MIG slices,
// based on the labels the NVIDIA GPU Operator's mig-manager applies. A
// nvidia.com/mig.config other than "all-disabled" that reached
// nvidia.com/mig.config.state="success" means MIG is active.
//
// Note: only IsMIG is derived here (used to size against a slice and disable
// CPU KV-cache offload). MIGProfile is intentionally left empty — the per-slice
// nvidia.com/mig-<profile> extended resource used by the "mixed" strategy is
// populated only via the spec-driven path (Workspace.Resource.Partition), so a
// node-detected MIG under the "single" strategy keeps requesting nvidia.com/gpu.
func isMIGNode(node *corev1.Node) bool {
	return hasMIGConfig(node) &&
		node.Labels[consts.NvidiaMIGConfigState] == consts.NvidiaMIGConfigStateSuccess
}

// hasMIGConfig reports whether MIG is requested, including while it is pending
// or failed, so a SKU's full-GPU topology is not used during reconfiguration.
func hasMIGConfig(node *corev1.Node) bool {
	migConfig := node.Labels[consts.NvidiaMIGConfig]
	return migConfig != "" && migConfig != consts.NvidiaMIGConfigDisabled
}
