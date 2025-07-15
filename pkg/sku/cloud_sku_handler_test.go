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
