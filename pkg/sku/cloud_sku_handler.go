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
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type CloudSKUHandler interface {
	GetSupportedSKUs() []string
	GetGPUConfigBySKU(sku string) *GPUConfig
}

type GPUConfig struct {
	SKU                   string
	GPUCount              int
	GPUMem                resource.Quantity
	GPUModel              string
	NVMeDiskEnabled       bool
	CUDAComputeCapability float64 // CUDA compute capability version (e.g., 7.5 for Turing, 8.0 for Ampere)
}

func (cfg *GPUConfig) String() string {
	return fmt.Sprintf("SKU: %s, GPUCount: %d, GPUMem: %s, GPUModel: %s, NVMeDiskEnabled: %t, CUDAComputeCapability: %.1f",
		cfg.SKU, cfg.GPUCount, cfg.GPUMem.String(), cfg.GPUModel, cfg.NVMeDiskEnabled, cfg.CUDAComputeCapability)
}

// SupportsBFloat16 returns true if the GPU supports bfloat16 (requires CUDA compute capability >= 8.0).
func (cfg *GPUConfig) SupportsBFloat16() bool {
	return cfg.CUDAComputeCapability >= 8.0
}

func GetCloudSKUHandler(cloud string) CloudSKUHandler {
	switch cloud {
	case consts.AzureCloudName:
		return NewAzureSKUHandler()
	case consts.AWSCloudName:
		return NewAwsSKUHandler()
	case consts.ArcCloudName:
		return NewArcSKUHandler()
	default:
		return nil
	}
}

type generalSKUHandler struct {
	supportedSKUs map[string]GPUConfig
}

func NewGeneralSKUHandler(supportedSKUs []GPUConfig) CloudSKUHandler {
	skuMap := make(map[string]GPUConfig)
	for _, sku := range supportedSKUs {
		skuMap[sku.SKU] = sku
	}
	return &generalSKUHandler{supportedSKUs: skuMap}
}

func (b *generalSKUHandler) GetSupportedSKUs() []string {
	keys := make([]string, 0, len(b.supportedSKUs))
	for k := range b.supportedSKUs {
		keys = append(keys, k)
	}
	return keys
}

func (b *generalSKUHandler) GetGPUConfigBySKU(sku string) *GPUConfig {
	if config, ok := b.supportedSKUs[sku]; ok {
		return &config
	}
	return nil
}

// HasSKUNamePrefix checks if the given SKU name has one of the specified prefixes,
// using case-insensitive comparison. This is useful because Azure VM SKU names are
// case-insensitive (e.g., "standard_d2s_v6" and "Standard_D2s_v6" refer to the same SKU).
func HasSKUNamePrefix(skuName string, prefixes ...string) bool {
	lowerSKU := strings.ToLower(skuName)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lowerSKU, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}
