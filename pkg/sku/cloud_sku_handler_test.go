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

	// Test case-insensitive matching - lowercase
	skuLower := "standard_nc6s_v3"
	gpuConfigLower := handler.GetGPUConfigBySKU(skuLower)
	if gpuConfigLower == nil {
		t.Fatalf("Case-insensitive lookup failed for lowercase SKU: %s", skuLower)
	}
	if gpuConfigLower.SKU != "Standard_NC6s_v3" {
		t.Errorf("Expected SKU 'Standard_NC6s_v3', got '%s'", gpuConfigLower.SKU)
	}

	// Test case-insensitive matching - uppercase
	skuUpper := "STANDARD_NC6S_V3"
	gpuConfigUpper := handler.GetGPUConfigBySKU(skuUpper)
	if gpuConfigUpper == nil {
		t.Fatalf("Case-insensitive lookup failed for uppercase SKU: %s", skuUpper)
	}
	if gpuConfigUpper.SKU != "Standard_NC6s_v3" {
		t.Errorf("Expected SKU 'Standard_NC6s_v3', got '%s'", gpuConfigUpper.SKU)
	}

	// Test case-insensitive matching - mixed case
	skuMixed := "standard_D2S_v6"
	gpuConfigMixed := handler.GetGPUConfigBySKU(skuMixed)
	// This should NOT be found as it's not a real Azure GPU SKU
	if gpuConfigMixed != nil {
		t.Errorf("Unsupported SKU 'standard_D2S_v6' should not be found")
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
