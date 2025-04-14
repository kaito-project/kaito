// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package sku

func NewAzureSKUHandler() CloudSKUHandler {
	supportedSKUs := []GPUConfig{
		{SKU: "Standard_NC6s_v3", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA V100"},
		{SKU: "Standard_NC12s_v3", GPUCount: 2, GPUMemGB: 32, GPUModel: "NVIDIA V100"},
		{SKU: "Standard_NC24s_v3", GPUCount: 4, GPUMemGB: 64, GPUModel: "NVIDIA V100"},
		{SKU: "Standard_NC24rs_v3", GPUCount: 4, GPUMemGB: 64, GPUModel: "NVIDIA V100"},
		{SKU: "Standard_NC4as_T4_v3", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NC8as_T4_v3", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NC16as_T4_v3", GPUCount: 1, GPUMemGB: 16, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NC64as_T4_v3", GPUCount: 4, GPUMemGB: 64, GPUModel: "NVIDIA T4"},
		{SKU: "Standard_NC24ads_A100_v4", GPUCount: 1, GPUMemGB: 80, GPUModel: "NVIDIA A100"},
		{SKU: "Standard_NC48ads_A100_v4", GPUCount: 2, GPUMemGB: 160, GPUModel: "NVIDIA A100"},
		{SKU: "Standard_NC96ads_A100_v4", GPUCount: 4, GPUMemGB: 320, GPUModel: "NVIDIA A100"},
		{SKU: "Standard_ND96asr_A100_v4", GPUCount: 8, GPUMemGB: 320, GPUModel: "NVIDIA A100"},
		{SKU: "Standard_ND96amsr_A100_v4", GPUCount: 8, GPUMemGB: 80, GPUModel: "NVIDIA A100"},
		// https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/ncadsh100v5-series
		{SKU: "Standard_NC40ads_H100_v5", GPUCount: 1, GPUMemGB: 94, GPUModel: "NVIDIA H100"},
		{SKU: "Standard_NC80adis_H100_v5", GPUCount: 2, GPUMemGB: 188, GPUModel: "NVIDIA H100"},
		// https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/ndh100v5-series
		{SKU: "Standard_ND96isr_H100_v5", GPUCount: 8, GPUMemGB: 640, GPUModel: "NVIDIA H100"},
		// https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/nccadsh100v5-series
		{SKU: "Standard_NCC40ads_H100_v5", GPUCount: 1, GPUMemGB: 94, GPUModel: "NVIDIA H100"},
		// https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/nd-h200-v5-series
		{SKU: "Standard_ND96isr_H200_v5", GPUCount: 8, GPUMemGB: 1128, GPUModel: "NVIDIA H200"},
		{SKU: "Standard_NG32ads_V620_v1", GPUCount: 1, GPUMemGB: 32, GPUModel: "AMD Radeon PRO V620"},
		{SKU: "Standard_NG32adms_V620_v1", GPUCount: 1, GPUMemGB: 32, GPUModel: "AMD Radeon PRO V620"},
		{SKU: "Standard_NV6", GPUCount: 1, GPUMemGB: 8, GPUModel: "NVIDIA M60"},
		{SKU: "Standard_NV12", GPUCount: 2, GPUMemGB: 16, GPUModel: "NVIDIA M60"},
		{SKU: "Standard_NV24", GPUCount: 4, GPUMemGB: 32, GPUModel: "NVIDIA M60"},
		{SKU: "Standard_NV12s_v3", GPUCount: 1, GPUMemGB: 8, GPUModel: "NVIDIA M60"},
		{SKU: "Standard_NV24s_v3", GPUCount: 2, GPUMemGB: 16, GPUModel: "NVIDIA M60"},
		{SKU: "Standard_NV48s_v3", GPUCount: 4, GPUMemGB: 32, GPUModel: "NVIDIA M60"},
		{SKU: "Standard_NV32as_v4", GPUCount: 1, GPUMemGB: 16, GPUModel: "AMD Radeon Instinct MI25"},

		// Not supporting partial gpu skus for now
		// {SKU: "Standard_NG8ads_V620_v1", GPUCount: 1.0 / 4.0, GPUMem: 8, GPUModel: "AMD Radeon PRO V620"},
		// {SKU: "Standard_NG16ads_V620_v1", GPUCount: 1.0 / 2.0, GPUMem: 16, GPUModel: "AMD Radeon PRO V620"},
		// {SKU: "Standard_NV4as_v4", GPUCount: 1.0 / 8.0, GPUMem: 2, GPUModel: "AMD Radeon Instinct MI25"},
		// {SKU: "Standard_NV8as_v4", GPUCount: 1.0 / 4.0, GPUMem: 4, GPUModel: "AMD Radeon Instinct MI25"},
		// {SKU: "Standard_NV16as_v4", GPUCount: 1.0 / 2.0, GPUMem: 8, GPUModel: "AMD Radeon Instinct MI25"},
	}
	return NewGeneralSKUHandler(supportedSKUs)
}
