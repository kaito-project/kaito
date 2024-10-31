// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package sku

import (
	"testing"

	"github.com/azure/kaito/pkg/utils/consts"
)

func TestAzureSKUHandler(t *testing.T) {
	handler := GetCloudSKUHandler(consts.AzureCloudName)

	// Test GetSupportedSKUs
	skus := handler.GetSupportedSKUs()
	if len(skus) == 0 {
		t.Errorf("GetSupportedSKUs returned an empty array")
	}

	// Test GetGPUConfigs with a SKU that is supported
	sku := "Standard_NC6s_v3"
	configMap := handler.GetGPUConfigs()
	config, exists := configMap[sku]
	if !exists {
		t.Errorf("Supported SKU missing from GPUConfigs")
	}
	if config.SKU != sku {
		t.Errorf("Incorrect config returned for a supported SKU")
	}

	// Test GetGPUConfigs with a SKU that is not supported
	sku = "Unsupported_SKU"
	config, exists = configMap[sku]
	if exists {
		t.Errorf("Unsupported SKU found in GPUConfigs")
	}
}

func TestAwsSKUHandler(t *testing.T) {
	handler := GetCloudSKUHandler(consts.AWSCloudName)

	// Test GetSupportedSKUs
	skus := handler.GetSupportedSKUs()
	if len(skus) == 0 {
		t.Errorf("GetSupportedSKUs returned an empty array")
	}

	// Test GetGPUConfigs with a SKU that is supported
	sku := "p2.xlarge"
	configMap := handler.GetGPUConfigs()
	config, exists := configMap[sku]
	if !exists {
		t.Errorf("Supported SKU missing from GPUConfigs")
	}
	if config.SKU != sku {
		t.Errorf("Incorrect config returned for a supported SKU")
	}

	// Test GetGPUConfigs with a SKU that is not supported
	sku = "Unsupported_SKU"
	config, exists = configMap[sku]
	if exists {
		t.Errorf("Unsupported SKU found in GPUConfigs")
	}
}
