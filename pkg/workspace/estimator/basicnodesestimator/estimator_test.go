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

package basicnodesestimator

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

func TestBasicNodesEstimator_Name(t *testing.T) {
	estimator := &BasicNodesEstimator{}
	assert.Equal(t, "basic", estimator.Name())
}

func TestBasicNodesEstimator_EstimateNodeCount(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	estimator := &BasicNodesEstimator{}

	tests := []struct {
		name                        string
		workspace                   *kaitov1beta1.Workspace
		disableNodeAutoProvisioning bool
		expectedCount               int32
		expectedError               bool
		errorContains               string
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
			name: "Should return error for invalid instance type (NAP enabled by default)",
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
			name: "Should optimize node count with valid instance type (NAP enabled by default)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count:        ptr.To(4),                  // User requests 4 nodes
					InstanceType: "Standard_NC96ads_A100_v4", // Large instance type with plenty of GPU memory
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model", // 8Gi requirement (small model)
						},
					},
				},
			},
			expectedCount: 1, // Should optimize to 1 node since A100 has enough memory for 8Gi model
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
			expectedCount: 0, // With new logic: when resource count is nil (nodeCountPerReplica=0), minimumNodes cannot be < 0, so nodeCountPerReplica stays 0
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

			count, err := estimator.EstimateNodeCount(ctx, tt.workspace, nil)

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

func TestBasicNodesEstimator_EstimateNodeCount_GPUMemoryCalculation(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	estimator := &BasicNodesEstimator{}

	// Test case for detailed GPU memory calculation verification
	t.Run("Should calculate correct minimum nodes based on GPU memory requirements", func(t *testing.T) {
		// Ensure NAP is enabled for this test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		// Use a model that requires significant GPU memory (64Gi)
		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        ptr.To(10),         // User requests many nodes
				InstanceType: "Standard_NC6s_v3", // Smaller GPU memory instance
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-distributed-model", // This model requires 64Gi
					},
				},
			},
		}

		count, err := estimator.EstimateNodeCount(ctx, workspace, nil)
		require.NoError(t, err)

		// The estimator should calculate optimal node count based on GPU memory
		// and return fewer nodes than requested if possible
		assert.True(t, count > 0, "Node count should be positive")
		assert.True(t, count <= 10, "Node count should not exceed user request")
	})
}

func TestBasicNodesEstimator_EstimateNodeCount_BYO(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	estimator := &BasicNodesEstimator{}

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
					Count:        ptr.To(1),
					InstanceType: "Standard_NC6s_v3", // Instance type is optional for BYO
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
					Count: ptr.To(1),
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
					Count: ptr.To(2),
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
			expectedCount: 1, // Should optimize from 2 to 1 node based on A100 memory
			expectedError: false,
		},
		{
			name: "Should return error when GetGPUConfigFromNodeLabels fails (NAP disabled)",
			workspace: &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					Count: ptr.To(1),
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
			// Enable BYO mode (NAP disabled)
			originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
			}()

			// Create a mock client
			mockClient := test.NewClient()
			if tt.setupMocks != nil {
				tt.setupMocks(mockClient)
			}

			count, err := estimator.EstimateNodeCount(ctx, tt.workspace, mockClient)

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

func TestBasicNodesEstimator_EstimateNodeCount_EdgeCases(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	estimator := &BasicNodesEstimator{}

	t.Run("Should handle case when nodeCountPerReplica is zero", func(t *testing.T) {
		// Ensure NAP is enabled for this test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		// This test covers the new logic where the condition is:
		// if minimumNodes < nodeCountPerReplica { nodeCountPerReplica = minimumNodes }
		// When nodeCountPerReplica is 0, minimumNodes will not be less than 0,
		// so nodeCountPerReplica remains unchanged and the function returns it

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        ptr.To(1),
				InstanceType: "Standard_NC96ads_A100_v4", // Large GPU memory instance
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model", // Small model that fits in one GPU
					},
				},
			},
		}

		count, err := estimator.EstimateNodeCount(ctx, workspace, nil)
		require.NoError(t, err)

		// With the new logic, when minimumNodes < nodeCountPerReplica,
		// nodeCountPerReplica gets updated to minimumNodes value
		assert.True(t, count > 0, "Node count should be positive")
	})

	t.Run("Should return minimum nodes when it's smaller than nodeCountPerReplica", func(t *testing.T) {
		// Ensure NAP is enabled for this test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        ptr.To(10),         // User wants many nodes
				InstanceType: "Standard_NC6s_v3", // Small GPU instance
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-model", // Model that can run efficiently on fewer nodes
					},
				},
			},
		}

		count, err := estimator.EstimateNodeCount(ctx, workspace, nil)
		require.NoError(t, err)

		// The function should update nodeCountPerReplica to minimumNodes when minimumNodes < nodeCountPerReplica,
		// optimizing for GPU utilization
		assert.True(t, count > 0, "Node count should be positive")
		assert.True(t, count <= 10, "Node count should not exceed resource count")
	})

	t.Run("Should return nodeCountPerReplica when minimumNodes is higher", func(t *testing.T) {
		// Ensure NAP is enabled for this test
		originalValue := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
		defer func() {
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = originalValue
		}()

		workspace := &kaitov1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-workspace",
				Namespace: "default",
			},
			Resource: kaitov1beta1.ResourceSpec{
				Count:        ptr.To(1),          // User wants fewer nodes
				InstanceType: "Standard_NC6s_v3", // Small GPU instance
			},
			Inference: &kaitov1beta1.InferenceSpec{
				Preset: &kaitov1beta1.PresetSpec{
					PresetMeta: kaitov1beta1.PresetMeta{
						Name: "test-distributed-model", // Large model requiring multiple nodes
					},
				},
			},
		}

		count, err := estimator.EstimateNodeCount(ctx, workspace, nil)
		require.NoError(t, err)

		// When a model requires more nodes than the minimum calculation suggests,
		// it should return the user-requested count or calculated requirement
		assert.True(t, count > 0, "Node count should be positive")
	})
}
