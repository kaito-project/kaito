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

func TestGetGPUConfigBySKUCaseInsensitive(t *testing.T) {
	handler := NewAzureSKUHandler()

	canonical := "Standard_NC4as_T4_v3"
	// Canonical casing
	config := handler.GetGPUConfigBySKU(canonical)
	if config == nil {
		t.Fatalf("Expected GPUConfig for %s, got nil", canonical)
	}
	if config.SKU != canonical {
		t.Errorf("Expected SKU %s, got %s", canonical, config.SKU)
	}

	// All-lowercase
	configLower := handler.GetGPUConfigBySKU("standard_nc4as_t4_v3")
	if configLower == nil {
		t.Fatal("Expected GPUConfig for lowercase SKU, got nil")
	}
	if configLower.SKU != canonical {
		t.Errorf("Expected canonical SKU %s, got %s", canonical, configLower.SKU)
	}

	// All-uppercase
	configUpper := handler.GetGPUConfigBySKU("STANDARD_NC4AS_T4_V3")
	if configUpper == nil {
		t.Fatal("Expected GPUConfig for uppercase SKU, got nil")
	}
	if configUpper.SKU != canonical {
		t.Errorf("Expected canonical SKU %s, got %s", canonical, configUpper.SKU)
	}

	// GetSupportedSKUs should return canonical (not lowercased) SKU names
	skus := handler.GetSupportedSKUs()
	found := false
	for _, s := range skus {
		if s == canonical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GetSupportedSKUs() should return canonical SKU names, %s not found", canonical)
	}
}

func TestHasSKUNamePrefix(t *testing.T) {
	tests := []struct {
		name     string
		skuName  string
		prefixes []string
		expected bool
	}{
		{"exact case match", "Standard_NC4as_T4_v3", []string{"Standard_N"}, true},
		{"lowercase sku", "standard_nc4as_t4_v3", []string{"Standard_N"}, true},
		{"uppercase sku", "STANDARD_NC4AS_T4_V3", []string{"Standard_N"}, true},
		{"d-series match", "standard_d2s_v6", []string{"Standard_D"}, true},
		{"multiple prefixes first match", "Standard_NC4as_T4_v3", []string{"Standard_N", "Standard_D"}, true},
		{"multiple prefixes second match", "Standard_D2s_v6", []string{"Standard_N", "Standard_D"}, true},
		{"no match", "Standard_E4s_v3", []string{"Standard_N", "Standard_D"}, false},
		{"empty sku", "", []string{"Standard_N"}, false},
		{"empty prefixes", "Standard_NC4as_T4_v3", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasSKUNamePrefix(tt.skuName, tt.prefixes...)
			if result != tt.expected {
				t.Errorf("HasSKUNamePrefix(%q, %v) = %v, want %v", tt.skuName, tt.prefixes, result, tt.expected)
			}
		})
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
