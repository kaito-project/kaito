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

package webhooks

import (
	"testing"

	"github.com/stretchr/testify/assert"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestNewControllerWebhooks(t *testing.T) {
	tests := []struct {
		name                     string
		enableInferenceSet       bool
		expectedConstructorCount int
	}{
		{
			name:                     "InferenceSet controller disabled",
			enableInferenceSet:       false,
			expectedConstructorCount: 2,
		},
		{
			name:                     "InferenceSet controller enabled",
			enableInferenceSet:       true,
			expectedConstructorCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original feature gate state
			originalValue := featuregates.FeatureGates[consts.FeatureFlagEnableInferenceSetController]
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagEnableInferenceSetController] = originalValue
			}()

			// Set feature gate for test
			featuregates.FeatureGates[consts.FeatureFlagEnableInferenceSetController] = tt.enableInferenceSet

			// Call the function
			constructors := NewControllerWebhooks()

			// Assert the expected number of constructors
			assert.Equal(t, tt.expectedConstructorCount, len(constructors))

			// Verify that the first two constructors are always present
			assert.NotNil(t, constructors[0])
			assert.NotNil(t, constructors[1])

			// If InferenceSet is enabled, verify the third constructor
			if tt.enableInferenceSet {
				assert.NotNil(t, constructors[2])
			}
		})
	}
}

func TestWorkspaceResources(t *testing.T) {
	// Verify that WorkspaceResources contains the expected GVKs
	assert.Equal(t, 2, len(WorkspaceResources))

	// Check v1alpha1 Workspace
	v1alpha1GVK := kaitov1alpha1.GroupVersion.WithKind("Workspace")
	assert.Contains(t, WorkspaceResources, v1alpha1GVK)
	assert.IsType(t, &kaitov1alpha1.Workspace{}, WorkspaceResources[v1alpha1GVK])

	// Check v1beta1 Workspace
	v1beta1GVK := kaitov1beta1.GroupVersion.WithKind("Workspace")
	assert.Contains(t, WorkspaceResources, v1beta1GVK)
	assert.IsType(t, &kaitov1beta1.Workspace{}, WorkspaceResources[v1beta1GVK])
}

func TestInferenceSetResources(t *testing.T) {
	// Verify that InferenceSetResources contains the expected GVK
	assert.Equal(t, 1, len(InferenceSetResources))

	// Check v1alpha1 InferenceSet
	v1alpha1GVK := kaitov1alpha1.GroupVersion.WithKind("InferenceSet")
	assert.Contains(t, InferenceSetResources, v1alpha1GVK)
	assert.IsType(t, &kaitov1alpha1.InferenceSet{}, InferenceSetResources[v1alpha1GVK])
}
