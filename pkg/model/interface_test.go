// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package model

import (
	"testing"

	"github.com/kaito-project/kaito/pkg/sku"
)

func TestGetGPUMemoryUtilForVLLM(t *testing.T) {
	tests := []struct {
		name      string
		gpuConfig *sku.GPUConfig
		expected  float64
	}{
		{
			name:      "Nil GPUConfig",
			gpuConfig: nil,
			expected:  0.9,
		},
		{
			name: "Zero GPU Memory",
			gpuConfig: &sku.GPUConfig{
				GPUMemGB: 0,
				GPUCount: 1,
			},
			expected: 0.9,
		},
		{
			name: "Zero GPU Count",
			gpuConfig: &sku.GPUConfig{
				GPUMemGB: 16,
				GPUCount: 0,
			},
			expected: 0.9,
		},
		{
			name: "V100",
			gpuConfig: &sku.GPUConfig{
				GPUMemGB: 16,
				GPUCount: 1,
			},
			expected: 0.9,
		},
		{
			name: "V100 x 2",
			gpuConfig: &sku.GPUConfig{
				GPUMemGB: 32,
				GPUCount: 2,
			},
			expected: 0.9,
		},
		{
			name: "A100",
			gpuConfig: &sku.GPUConfig{
				GPUMemGB: 80,
				GPUCount: 1,
			},
			expected: 0.95,
		},
		{
			name: "Invalid",
			gpuConfig: &sku.GPUConfig{
				GPUMemGB: 1,
				GPUCount: 1,
			},
			expected: 0.9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getGPUMemoryUtilForVLLM(tt.gpuConfig)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
