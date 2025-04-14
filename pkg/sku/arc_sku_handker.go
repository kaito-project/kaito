// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package sku

func NewArcSKUHandler() CloudSKUHandler {
	// The SKU including Azure Local official sku: https://learn.microsoft.com/en-us/azure/aks/aksarc/scale-requirements
	supportedSKUs := []GPUConfig{
		{SKU: "Standard_NK6", GPUCount: 1, GPUMemGB: 8, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NK12", GPUCount: 2, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NC4_A2", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC8_A2", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC16_A2", GPUCount: 2, GPUMemGB: 48, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC32_A2", GPUCount: 2, GPUMemGB: 48, GPUModel: "NVIDIA A2"},
		{SKU: "Standard_NC4_A16", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A16"},
		{SKU: "Standard_NC8_A16", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA A16"},
		{SKU: "Standard_NC16_A16", GPUCount: 2, GPUMemGB: 48, GPUModel: "NVIDIA A16"},
		{SKU: "Standard_NC32_A16", GPUCount: 2, GPUMemGB: 48, GPUModel: "NVIDIA A16"},
	}
	return NewGeneralSKUHandler(supportedSKUs)
}
