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
		targetFeature string
		expectedValue bool
	}{
		{
			name:          "WithInvalidFeatureGates",
			featureGates:  "invalid",
			expectedError: true,
		},
		{
			name:          "WithUnsupportedFeatureGate",
			featureGates:  "unsupported=true,enableMIG=false",
			expectedError: true,
		},
		{
			name:          "WithDeprecatedFeatureGateIgnored",
			featureGates:  "vLLM=false,enableMIG=true",
			expectedError: false,
			targetFeature: "enableMIG",
			expectedValue: true,
		},
		{
			name:          "WithDuplicateFeatureGates",
			featureGates:  "enableMIG=true,enableMIG=false",
			expectedError: false,
			targetFeature: "enableMIG",
			expectedValue: false, // Apply the last value.
		},
		{
			name:          "WithEnableInferenceSetController",
			featureGates:  "enableInferenceSetController=true",
			expectedError: false,
			targetFeature: "enableInferenceSetController",
			expectedValue: true,
		},
		{
			name:          "WithDisableInferenceSetController",
			featureGates:  "enableInferenceSetController=false",
			expectedError: false,
			targetFeature: "enableInferenceSetController",
			expectedValue: false,
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
				if tt.targetFeature != "" && FeatureGates[tt.targetFeature] != tt.expectedValue {
					t.Errorf("feature gate test %s fails", tt.name)
				}
				_, registered := FeatureGates["vLLM"]
				assert.Check(t, !registered, "deprecated gate must not be registered")
			}
		})
	}
}
