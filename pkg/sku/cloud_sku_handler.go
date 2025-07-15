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
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type CloudSKUHandler interface {
	GetSupportedSKUs() []string
	GetGPUConfigBySKU(sku string) *GPUConfig
}

type GPUConfig struct {
	SKU             string
	GPUCount        int
	GPUMemGB        int
	GPUModel        string
	NVMeDiskEnabled bool
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
