// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package featuregates

import (
	"testing"

	"gotest.tools/assert"
)

func TestParseFeatureGates(t *testing.T) {
	tests := []struct {
		name          string
		featureGates  string
		expectedError bool
		expectedValue string
	}{
		{
			name:          "WithValidEnableFeatureGates",
			featureGates:  "vLLM=true",
			expectedError: false,
			expectedValue: "true",
		},
		{
			name:          "WithDuplicateFeatureGates",
			featureGates:  "vLLM=false,vLLM=true",
			expectedError: false,
			expectedValue: "true", // Apply the last value.
		},
		{
			name:          "WithInvalidFeatureGates",
			featureGates:  "invalid",
			expectedError: true,
		},
		{
			name:          "WithUnsupportedFeatureGate",
			featureGates:  "unsupported=true,vLLM=false",
			expectedError: true,
		},
		{
			name:          "WithValidDisableFeatureGates",
			featureGates:  "vLLM=false",
			expectedError: false,
			expectedValue: "false",
		},
		{
			name:          "WithEmptyFeatureGates",
			featureGates:  "",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ParseAndValidateFeatureGates(tt.featureGates)
			if tt.expectedError {
				assert.Check(t, err != nil, "expected error but got nil")
			} else {
				assert.NilError(t, err)
			}
		})
	}
}
