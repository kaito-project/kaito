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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/accelerator"
	"github.com/kaito-project/kaito/pkg/accelerator/nvidia"
	"github.com/kaito-project/kaito/pkg/apis"
	"github.com/kaito-project/kaito/pkg/consts"
)

// DefaultSKUHandler is the package-level CloudSKUHandler used by callers that
// do not want to resolve a handler from the environment on every call. It is
// expected to be initialized once at process startup via GetSKUHandler.
var DefaultSKUHandler CloudSKUHandler = nil

// UnknownSKU identifies GPU configuration that was not resolved from a SKU table.
const UnknownSKU = "unknown"

// GiB is the number of bytes in a gibibyte.
const GiB = 1024 * 1024 * 1024

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
	handler := DefaultSKUHandler
	if handler == nil {
		handler, _ = GetSKUHandler()
	}
	return GetGPUConfigFromNodeWithHandler(handler, node)
}

// GetGPUConfigFromNodeWithHandler is GetGPUConfigFromNode with an explicit
// SKU handler. A nil handler resolves from GPU Feature Discovery labels only.
func GetGPUConfigFromNodeWithHandler(handler CloudSKUHandler, node *corev1.Node) (*GPUConfig, error) {
	if handler != nil && !nvidia.HasMIGConfig(node.Labels) {
		if instanceType := node.Labels[corev1.LabelInstanceTypeStable]; instanceType != "" {
			if config := handler.GetGPUConfigBySKU(instanceType); config != nil {
				return config, nil
			}
		}
	}

	return gpuConfigFromDevice(vendorFor(accelerator.TypeNVIDIA), node)
}

// vendors reads devices for each accelerator type that publishes node labels.
var vendors = map[accelerator.Type]accelerator.Vendor{
	accelerator.TypeNVIDIA: nvidia.Vendor{},
}

// vendorFor returns the vendor for t. Types without a vendor use NVIDIA, which
// is how every GPU was handled before vendors existed.
func vendorFor(t accelerator.Type) accelerator.Vendor {
	if v, ok := vendors[t]; ok {
		return v
	}
	return vendors[accelerator.TypeNVIDIA]
}

// gpuConfigFromDevice reads the node's devices through vendor.
func gpuConfigFromDevice(vendor accelerator.Vendor, node *corev1.Node) (*GPUConfig, error) {
	device, err := vendor.DeviceFromNode(node)
	if err != nil {
		return nil, err
	}
	return &GPUConfig{
		SKU:                   UnknownSKU,
		Accelerator:           vendor.Type(),
		GPUCount:              device.Count,
		GPUModel:              device.Model,
		GPUMem:                device.Memory,
		CUDAComputeCapability: device.ComputeCapability,
		IsMIG:                 device.Partitioned,
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
