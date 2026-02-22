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

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAzureSKUHandler(t *testing.T) {
	handler := NewAzureSKUHandler()

	// Test GetSupportedSKUs
	skus := handler.GetSupportedSKUs()
	if len(skus) == 0 {
		t.Errorf("GetSupportedSKUs returned an empty array")
	}

	// Test GetGPUConfigs with a SKU that is supported
	sku := "Standard_NC4as_T4_v3"
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

func TestGPUConfigMemoryIsQuantity(t *testing.T) {
	tests := []struct {
		name           string
		handler        CloudSKUHandler
		sku            string
		expectedMemGiB string
	}{
		{
			name:           "Azure Standard_NC4as_T4_v3",
			handler:        NewAzureSKUHandler(),
			sku:            "Standard_NC4as_T4_v3",
			expectedMemGiB: "16Gi",
		},
		{
			name:           "Azure Standard_NC24ads_A100_v4",
			handler:        NewAzureSKUHandler(),
			sku:            "Standard_NC24ads_A100_v4",
			expectedMemGiB: "80Gi",
		},
		{
			name:           "AWS p2.xlarge",
			handler:        NewAwsSKUHandler(),
			sku:            "p2.xlarge",
			expectedMemGiB: "12Gi",
		},
		{
			name:           "AWS p5.48xlarge",
			handler:        NewAwsSKUHandler(),
			sku:            "p5.48xlarge",
			expectedMemGiB: "640Gi",
		},
		{
			name:           "Arc Standard_NK6",
			handler:        NewArcSKUHandler(),
			sku:            "Standard_NK6",
			expectedMemGiB: "8Gi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.handler.GetGPUConfigBySKU(tt.sku)
			if config == nil {
				t.Fatalf("SKU %s not found", tt.sku)
			}
			expected := resource.MustParse(tt.expectedMemGiB)
			if config.GPUMem.Cmp(expected) != 0 {
				t.Errorf("GPUMem mismatch for %s: expected %s, got %s",
					tt.sku, expected.String(), config.GPUMem.String())
			}
			// Verify the value is positive (non-zero)
			if config.GPUMem.IsZero() {
				t.Errorf("GPUMem should not be zero for SKU %s", tt.sku)
			}
		})
	}
}
