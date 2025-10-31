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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
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
			name: "Should return error for invalid instance type when NAP enabled",
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
			errorContains: "failed to get GPU config for instance type Invalid_Instance_Type",
		},
		{
			name: "Should optimize node count with valid instance type when NAP enabled",
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
							Name: "test-model", // 8Gi requirement, easily fits in single A100
						},
					},
				},
			},
			expectedCount: 1, // Should optimize to 1 node despite user requesting 4
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
			// Ensure NAP is enabled (default behavior) for these tests
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

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

func TestAdvancedNodesEstimator_EstimateNodeCount_BYO(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &AdvancedNodesEstimator{}

	tests := []struct {
		name          string
		workspace     *kaitov1beta1.Workspace
		setupMocks    func(*test.MockClient)
		expectedCount int32
		expectedError bool
		errorContains string
	}{
		{
			name: "Should return error when no ready nodes found (NAP disabled)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					InstanceType: "Invalid_Instance_Type", // Instance type is optional for BYO
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock empty ready nodes list
				nodeList := &corev1.NodeList{Items: []corev1.Node{}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)
			},
			expectedCount: 0,
			expectedError: true,
			errorContains: "no ready nodes found, unable to determine GPU configuration",
		},
		{
			name: "Should return error when GetReadyNodes fails (NAP disabled)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					// No InstanceType - should get config from existing nodes
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock GetReadyNodes to fail
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Return(assert.AnError)
			},
			expectedCount: 0,
			expectedError: true,
			errorContains: "failed to list ready nodes",
		},
		{
			name: "Should use GPU config from ready nodes (NAP disabled)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					// No InstanceType specified - should get config from existing nodes
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock ready node with GPU labels
				readyNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "byo-gpu-node",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "Standard_NC96ads_A100_v4",
							"kubernetes.azure.com/accelerator": "nvidia-tesla-a100",
							"nvidia.com/gpu.product":           "Tesla-A100-SXM4-80GB",
							"nvidia.com/gpu.count":             "4",
							"nvidia.com/gpu.memory":            "81920", // 80GB in MiB
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
						Capacity: corev1.ResourceList{
							resources.CapacityNvidiaGPU: resource.MustParse("4"), // 4 A100 GPUs
						},
					},
				}
				nodeList := &corev1.NodeList{Items: []corev1.Node{readyNode}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)
			},
			expectedCount: 1, // Should work with BYO node configuration
			expectedError: false,
		},
		{
			name: "Should return error when GetGPUConfigFromNodeLabels fails (NAP disabled)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			},
			setupMocks: func(mockClient *test.MockClient) {
				// Mock ready node without proper GPU labels
				readyNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cpu-only-node",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "Standard_D4s_v3", // CPU-only instance
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				}
				nodeList := &corev1.NodeList{Items: []corev1.Node{readyNode}}
				mockClient.On("List", mock.Anything, mock.IsType(&corev1.NodeList{}), mock.Anything).Run(func(args mock.Arguments) {
					nl := args.Get(1).(*corev1.NodeList)
					*nl = *nodeList
				}).Return(nil)
			},
			expectedCount: 0,
			expectedError: true,
			errorContains: "failed to get GPU config from existing nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original feature gate value and enable BYO mode
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

			// Create a mock client for BYO scenarios
			mockClient := test.NewClient()
			if tt.setupMocks != nil {
				tt.setupMocks(mockClient)
			}

			count, err := calculator.EstimateNodeCount(ctx, tt.workspace, mockClient)

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

			mockClient.AssertExpectations(t)
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
			// Ensure NAP is enabled for these tests
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

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
			// Ensure NAP is enabled for these tests
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

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
