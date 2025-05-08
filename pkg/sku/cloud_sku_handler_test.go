// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package sku

import (
	"testing"
)

func TestAzureSKUHandler(t *testing.T) {
	handler := NewAzureSKUHandler()

	// Test GetSupportedSKUs
	skus := handler.GetSupportedSKUs()
	if len(skus) == 0 {
		t.Errorf("GetSupportedSKUs returned an empty array")
	}

	// Test GetGPUConfigs with a SKU that is supported
	sku := "Standard_NC6s_v3"
	gpuConfig1 := handler.GetGPUConfigBySKU(sku)
	if gpuConfig1 == nil {
		t.Fatalf("Supported SKU missing from GPUConfigs")
	}
	if gpuConfig1.SKU != sku {
		t.Errorf("Incorrect config returned for a supported SKU")
	}

	// Test GetGPUConfigs with a SKU that is not supported
	sku = "Unsupported_SKU"
	gpuConfig2 := handler.GetGPUConfigBySKU(sku)
	if gpuConfig2 != nil {
		t.Errorf("Unsupported SKU found in GPUConfigs")
	}
}

func TestAwsSKUHandler(t *testing.T) {
	handler := NewAwsSKUHandler()

	// Test GetSupportedSKUs
	skus := handler.GetSupportedSKUs()
	if len(skus) == 0 {
		t.Errorf("GetSupportedSKUs returned an empty array")
	}

	// Test GetGPUConfigs with a SKU that is supported
	sku := "p2.xlarge"
	gpuConfig1 := handler.GetGPUConfigBySKU(sku)
	if gpuConfig1 == nil {
		t.Fatalf("Supported SKU missing from GPUConfigs")
	}
	if gpuConfig1.SKU != sku {
		t.Errorf("Incorrect config returned for a supported SKU")
	}

	// Test GetGPUConfigs with a SKU that is not supported
	sku = "Unsupported_SKU"
	gpuConfig2 := handler.GetGPUConfigBySKU(sku)
	if gpuConfig2 != nil {
		t.Errorf("Unsupported SKU found in GPUConfigs")
	}
}
