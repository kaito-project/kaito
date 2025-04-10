// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package sku

import (
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type CloudSKUHandler interface {
	GetSupportedSKUs() []string
	GetGPUConfigs() map[string]GPUConfig
	GetGPUConfigBySKU(sku string) *GPUConfig
}

type GPUConfig struct {
	SKU      string
	GPUCount int
	GPUMemGB int
	GPUModel string
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

func (b *generalSKUHandler) GetSupportedSKUs() []string {
	keys := make([]string, 0, len(b.supportedSKUs))
	for k := range b.supportedSKUs {
		keys = append(keys, k)
	}
	return keys
}

func (b *generalSKUHandler) GetGPUConfigs() map[string]GPUConfig {
	return b.supportedSKUs
}

func (b *generalSKUHandler) GetGPUConfigBySKU(sku string) *GPUConfig {
	if config, ok := b.supportedSKUs[sku]; ok {
		return &config
	}
	return nil
}
