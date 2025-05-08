// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package sku

var _ CloudSKUHandler = &ArcSKUHandler{}

type ArcSKUHandler struct {
	supportedSKUs map[string]GPUConfig
}

func NewArcSKUHandler() *ArcSKUHandler {
	return &ArcSKUHandler{
		// The SKU including Azure Local official sku: https://learn.microsoft.com/en-us/azure/aks/aksarc/scale-requirements
		supportedSKUs: map[string]GPUConfig{
			"Standard_NK6":      {SKU: "Standard_NK6", GPUCount: 1, GPUMem: 8, GPUModel: "NVIDIA T4"},
			"Standard_NK12":     {SKU: "Standard_NK12", GPUCount: 2, GPUMem: 16, GPUModel: "NVIDIA T4"},
			"Standard_NC4_A2":   {SKU: "Standard_NC4_A2", GPUCount: 1, GPUMem: 16, GPUModel: "NVIDIA A2"},
			"Standard_NC8_A2":   {SKU: "Standard_NC8_A2", GPUCount: 1, GPUMem: 16, GPUModel: "NVIDIA A2"},
			"Standard_NC16_A2":  {SKU: "Standard_NC16_A2", GPUCount: 2, GPUMem: 32, GPUModel: "NVIDIA A2"},
			"Standard_NC32_A2":  {SKU: "Standard_NC32_A2", GPUCount: 2, GPUMem: 32, GPUModel: "NVIDIA A2"},
			"Standard_NC4_A16":  {SKU: "Standard_NC4_A16", GPUCount: 1, GPUMem: 16, GPUModel: "NVIDIA A16"},
			"Standard_NC8_A16":  {SKU: "Standard_NC8_A16", GPUCount: 1, GPUMem: 16, GPUModel: "NVIDIA A16"},
			"Standard_NC16_A16": {SKU: "Standard_NC16_A16", GPUCount: 2, GPUMem: 32, GPUModel: "NVIDIA A16"},
			"Standard_NC32_A16": {SKU: "Standard_NC32_A16", GPUCount: 2, GPUMem: 32, GPUModel: "NVIDIA A16"},
		},
	}
}

func (a *ArcSKUHandler) GetSupportedSKUs() []string {
	return GetMapKeys(a.supportedSKUs)
}

func (a *ArcSKUHandler) GetGPUConfigs() map[string]GPUConfig {
	return a.supportedSKUs
}
