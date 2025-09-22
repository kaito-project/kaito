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

package advancednodesestimator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func init() {
	// Register test models for testing
	test.RegisterTestModel()
}

func TestAdvancedNodesEstimator_Name(t *testing.T) {
	calculator := &AdvancedNodesEstimator{}
	assert.Equal(t, "advanced", calculator.Name())
}

func TestAdvancedNodesEstimator_EstimateNodeCount(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &AdvancedNodesEstimator{}

	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		expectedCount int32
		expectedError bool
		errorContains string
	}{
		{
			name: "Should return resource count when inference is nil",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(3),
					InstanceType: "Standard_NC6s_v3",
				},
				Inference: nil,
			},
			expectedCount: 3,
			expectedError: false,
		},
		{
			name: "Should return 1 when inference is nil and count is nil",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        nil,
					InstanceType: "Standard_NC6s_v3",
				},
				Inference: nil,
			},
			expectedCount: 1,
			expectedError: false,
		},
		{
			name: "Should return resource count when preset is nil",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(2),
					InstanceType: "Standard_NC6s_v3",
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: nil,
				},
			},
			expectedCount: 2,
			expectedError: false,
		},
		{
			name: "Should return resource count when preset name is empty",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(4),
					InstanceType: "Standard_NC6s_v3",
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "",
						},
					},
				},
			},
			expectedCount: 4,
			expectedError: false,
		},
		{
			name: "Should return error for invalid instance type",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(1),
					InstanceType: "Invalid_Instance_Type",
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			expectedCount: 0,
			expectedError: true,
			errorContains: "GPU config is nil for instance type",
		},
		{
			name: "Should calculate optimal node count when GPU memory allows optimization",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(4),                  // User requests 4 nodes
					InstanceType: "Standard_NC96ads_A100_v4", // Has large GPU memory (80GB per GPU)
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model", // 8Gi requirement
						},
					},
				},
			},
			expectedCount: 1, // Should optimize to 1 node (8Gi easily fits in 80GB GPU)
			expectedError: false,
		},
		{
			name: "Should respect user node count when already optimal",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(1),          // User requests 1 node
					InstanceType: "Standard_NC6s_v3", // Smaller GPU memory
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			expectedCount: 1, // Should keep user's choice
			expectedError: false,
		},
		{
			name: "Should handle workspace with nil resource count",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        nil, // No count specified
					InstanceType: "Standard_NC6s_v3",
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			expectedCount: 1, // Default to 1 when count is nil, sufficient for test-model
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := calculator.EstimateNodeCount(ctx, tt.workspace, nil)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Equal(t, tt.expectedCount, count)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCount, count)
			}
		})
	}
}

func TestAdvancedNodesEstimator_EstimateNodeCount_Falcon7B(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &AdvancedNodesEstimator{}

	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		expectedCount int32
		expectedError bool
		errorContains string
	}{
		{
			name: "Should optimize falcon-7b with A10 GPU - single node sufficient",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-falcon-7b-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(3),                 // User requests 3 nodes
					InstanceType: "Standard_NV36ads_A10_v5", // A10 GPU with 24GB memory
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-falcon-7b", // 13.44Gi requirement, tensor parallelism disabled
						},
					},
				},
			},
			expectedCount: 1, // Should optimize to 1 node (13.44Gi fits easily in 24GB A10 GPU)
			expectedError: false,
		},
		{
			name: "Should respect user choice for falcon-7b when user requests 1 node",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-falcon-7b-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(1),                 // User requests 1 node (already optimal)
					InstanceType: "Standard_NV36ads_A10_v5", // A10 GPU with 24GB memory
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-falcon-7b", // 13.44Gi requirement
						},
					},
				},
			},
			expectedCount: 1, // Should keep user's choice since it's already optimal
			expectedError: false,
		},
		{
			name: "Should optimize falcon-7b with A100 GPU - single node more than sufficient",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-falcon-7b-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(4),                  // User requests 4 nodes
					InstanceType: "Standard_NC24ads_A100_v4", // A100 GPU with 80GB memory
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-falcon-7b", // 13.44Gi requirement, easily fits in 80GB A100
						},
					},
				},
			},
			expectedCount: 1, // Should optimize to 1 node (13.44Gi is tiny compared to 80GB A100 GPU)
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := calculator.EstimateNodeCount(ctx, tt.workspace, nil)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Equal(t, tt.expectedCount, count)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCount, count)
			}
		})
	}
}

func TestAdvancedNodesEstimator_EstimateNodeCount_Qwen25Coder32B(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &AdvancedNodesEstimator{}

	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		expectedCount int32
		expectedError bool
		errorContains string
	}{
		{
			name: "Should optimize qwen2.5-coder-32b with A100 GPU - single node sufficient",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-qwen25-coder-32b-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(3),                  // User requests 3 nodes
					InstanceType: "Standard_NC24ads_A100_v4", // A100 GPU with 80GB memory
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-qwen2.5-coder-32b-instruct", // 62.5Gi requirement, supports tensor parallelism
						},
					},
				},
			},
			expectedCount: 1, // Should optimize to 1 node (62.5Gi fits in 80GB A100 GPU with 0.84 utilization)
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := calculator.EstimateNodeCount(ctx, tt.workspace, nil)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Equal(t, tt.expectedCount, count)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCount, count)
			}
		})
	}
}
