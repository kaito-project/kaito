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

package nodesestimator

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
	"github.com/kaito-project/kaito/pkg/utils/nodes"
	"github.com/kaito-project/kaito/pkg/utils/test"
	workspaceutil "github.com/kaito-project/kaito/pkg/utils/workspace"
)

func init() {
	// Register test models for testing
	test.RegisterTestModel()
}

func TestNodeEstimator_Name(t *testing.T) {
	calculator := &NodeEstimator{}
	assert.Equal(t, "node-estimator", calculator.Name())
}

func TestNodeEstimator_EstimateNodeCount(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &NodeEstimator{}

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
					InstanceType: "Standard_NV36ads_A10_v5",
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
					InstanceType: "Standard_NV36ads_A10_v5",
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
					InstanceType: "Standard_NV36ads_A10_v5",
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
					InstanceType: "Standard_NV36ads_A10_v5",
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
					Count:        ptr.To(1),                 // User requests 1 node
					InstanceType: "Standard_NV36ads_A10_v5", // Smaller GPU memory
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
					InstanceType: "Standard_NV36ads_A10_v5",
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

			req, reqErr := workspaceutil.NodeEstimateRequestFromWorkspace(ctx, tt.workspace, nil)
			require.NoError(t, reqErr)
			count, err := calculator.EstimateNodeCount(ctx, req, nil)

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

func TestNodeEstimator_EstimateNodeCount_BYO(t *testing.T) {
	// Set the cloud provider environment variable for SKU lookup
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &NodeEstimator{}

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
							nodes.CapacityNvidiaGPU: resource.MustParse("4"), // 4 A100 GPUs
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

			req, reqErr := workspaceutil.NodeEstimateRequestFromWorkspace(ctx, tt.workspace, mockClient)
			require.NoError(t, reqErr)
			count, err := calculator.EstimateNodeCount(ctx, req, mockClient)

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

func TestNodeEstimator_EstimateNodeCount_MIG(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &NodeEstimator{}

	// test-model requires 8Gi of weights with BytesPerToken=0, so the fit check is:
	//   modelSize  = 8Gi * 1.02                       = 8.16 GiB
	//   overhead   = 2.3 GiB base + 0 KV + 0.05*8.16  = 2.71 GiB
	//   needed     ~= 10.87 GiB
	//   available  = <sliceGB> * 0.92 GiB
	// so a slice must expose ~12GB before overhead to fit the model.
	tests := []struct {
		name          string
		migProfile    string
		expectedCount int32
		expectedError bool
		errorContains string
	}{
		{
			name:          "Model fits in a large MIG slice",
			migProfile:    "2g.24gb", // 24 * 0.92 = 22.08 GiB available
			expectedCount: 1,
			expectedError: false,
		},
		{
			name:          "Model fits in a 3g.47gb slice",
			migProfile:    "3g.47gb", // 47 * 0.92 = 43.24 GiB available
			expectedCount: 1,
			expectedError: false,
		},
		{
			name:          "Model does not fit in a small MIG slice",
			migProfile:    "1g.10gb", // 10 * 0.92 = 9.2 GiB available < 10.87 GiB needed
			expectedCount: 0,
			expectedError: true,
			errorContains: "only provides",
		},
		{
			name:          "Invalid MIG profile returns an error",
			migProfile:    "bogus",
			expectedCount: 0,
			expectedError: true,
			errorContains: "failed to get MIG GPU config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// MIG is only active behind the enableMIG gate and is BYO-only.
			origMIG := featuregates.FeatureGates[consts.FeatureFlagEnableMIG]
			origNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
			featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = true
			featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
			defer func() {
				featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = origMIG
				featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAP
			}()

			workspace := &kaitov1beta1.Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mig-workspace",
					Namespace: "default",
				},
				Resource: kaitov1beta1.ResourceSpec{
					// InstanceType is empty for BYO/MIG.
					Partition: &kaitov1beta1.PartitionSpec{Mode: kaitov1beta1.PartitionModeMIG, Profile: tt.migProfile},
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "test-model",
						},
					},
				},
			}

			req, reqErr := workspaceutil.NodeEstimateRequestFromWorkspace(ctx, workspace, nil)
			require.NoError(t, reqErr)
			count, err := calculator.EstimateNodeCount(ctx, req, nil)

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

// TestNodeEstimator_EstimateNodeCount_MIG_ContextSize exercises the MIG path when
// a non-default context length is supplied via RuntimeProfile.ContextSize.
func TestNodeEstimator_EstimateNodeCount_MIG_ContextSize(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &NodeEstimator{}

	origMIG := featuregates.FeatureGates[consts.FeatureFlagEnableMIG]
	origNAP := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	defer func() {
		featuregates.FeatureGates[consts.FeatureFlagEnableMIG] = origMIG
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAP
	}()

	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mig-ctx-workspace",
			Namespace: "default",
		},
		Resource: kaitov1beta1.ResourceSpec{
			Partition: &kaitov1beta1.PartitionSpec{Mode: kaitov1beta1.PartitionModeMIG, Profile: "1g.24gb"}, // 24 * 0.92 = 22.08 GiB available
		},
		Inference: &kaitov1beta1.InferenceSpec{
			Preset: &kaitov1beta1.PresetSpec{
				PresetMeta: kaitov1beta1.PresetMeta{
					Name: "test-model",
				},
			},
		},
	}

	req, reqErr := workspaceutil.NodeEstimateRequestFromWorkspace(ctx, workspace, nil)
	require.NoError(t, reqErr)

	// test-model has BytesPerToken=0, so a large context length does not change
	// the KV cache term; the model still fits in a 1g.24gb slice. This guards the
	// ContextSize plumbing through the MIG path.
	req.RuntimeProfile.ContextSize = 131072
	count, err := calculator.EstimateNodeCount(ctx, req, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), count)
}

// TestNodeEstimator_EstimateNodeCount_RealCatalogModels exercises real
// model_catalog.yaml entries (resolved without any network call) against
// single-GPU Azure A100/H100 SKUs to validate the node-count math for
// production-sized models with distinct KV-cache shapes (GQA/MQA, hybrid).
// realCatalogModelCase is a single model/instanceType/expected-node-count case
// evaluated by runRealCatalogModelCases.
type realCatalogModelCase struct {
	name          string
	modelName     string
	instanceType  string
	expectedCount int32
}

// runRealCatalogModelCases resolves each real model_catalog.yaml entry (no
// network call) on its instanceType and asserts the estimated node count.
func runRealCatalogModelCases(t *testing.T, cases []realCatalogModelCase) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	ctx := context.Background()
	calculator := &NodeEstimator{}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
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
					Count:        ptr.To(1),
					InstanceType: tt.instanceType,
				},
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: kaitov1beta1.ModelName(tt.modelName),
						},
					},
				},
			}

			req, reqErr := workspaceutil.NodeEstimateRequestFromWorkspace(ctx, workspace, nil)
			require.NoError(t, reqErr)

			count, err := calculator.EstimateNodeCount(ctx, req, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

// TestNodeEstimator_EstimateNodeCount_RealCatalogModels_A100 exercises real
// model_catalog.yaml entries on 1-GPU and 2-GPU Azure A100 SKUs (80Gi/GPU) to
// validate the node-count math for production-sized models with distinct
// KV-cache shapes (GQA/MQA, hybrid) and correct scaling with per-node GPU count.
func TestNodeEstimator_EstimateNodeCount_RealCatalogModels_A100(t *testing.T) {
	const (
		a100_1GPU = "Standard_NC24ads_A100_v4"
		a100_2GPU = "Standard_NC48ads_A100_v4"
	)
	runRealCatalogModelCases(t, []realCatalogModelCase{
		{"gpt-oss-120b/1xA100", "openai/gpt-oss-120b", a100_1GPU, 2},                          // 121.54Gi
		{"gpt-oss-120b/2xA100", "openai/gpt-oss-120b", a100_2GPU, 1},                          // 121.54Gi
		{"DeepSeek-V4-Flash-0731/1xA100", "deepseek-ai/DeepSeek-V4-Flash-0731", a100_1GPU, 3}, // 155.43Gi
		{"DeepSeek-V4-Flash-0731/2xA100", "deepseek-ai/DeepSeek-V4-Flash-0731", a100_2GPU, 2}, // 155.43Gi
		{"Qwen3.8-27B/1xA100", "Qwen/Qwen3.8-27B", a100_1GPU, 1},                              // 51.75Gi
		{"gemma-4-31B-it/1xA100", "google/gemma-4-31B-it", a100_1GPU, 1},                      // 58.25Gi
	})
}

// TestNodeEstimator_EstimateNodeCount_RealCatalogModels_H100 exercises real
// model_catalog.yaml entries on 1-GPU and 2-GPU Azure H100 SKUs (94Gi/GPU) to
// validate the node-count math for production-sized models with distinct
// KV-cache shapes (GQA/MQA, hybrid) and correct scaling with per-node GPU count.
func TestNodeEstimator_EstimateNodeCount_RealCatalogModels_H100(t *testing.T) {
	const (
		h100_1GPU = "Standard_NC40ads_H100_v5"
		h100_2GPU = "Standard_NC80adis_H100_v5"
	)
	runRealCatalogModelCases(t, []realCatalogModelCase{
		{"gpt-oss-120b/1xH100", "openai/gpt-oss-120b", h100_1GPU, 2},                          // 121.54Gi
		{"gpt-oss-120b/2xH100", "openai/gpt-oss-120b", h100_2GPU, 1},                          // 121.54Gi
		{"DeepSeek-V4-Flash-0731/1xH100", "deepseek-ai/DeepSeek-V4-Flash-0731", h100_1GPU, 2}, // 155.43Gi
		{"DeepSeek-V4-Flash-0731/2xH100", "deepseek-ai/DeepSeek-V4-Flash-0731", h100_2GPU, 1}, // 155.43Gi
		{"Qwen3.8-27B/1xH100", "Qwen/Qwen3.8-27B", h100_1GPU, 1},                              // 51.75Gi
		{"gemma-4-31B-it/1xH100", "google/gemma-4-31B-it", h100_1GPU, 1},                      // 58.25Gi
	})
}

// TestNodeEstimator_EstimateNodeCount_RealCatalogModels_A10 exercises real
// model_catalog.yaml entries on 1-GPU and 2-GPU Azure A10 SKUs (24Gi/GPU) to
// validate the node-count math on small-VRAM GPUs, where models that fit
// comfortably on A100/H100 need multiple A10 nodes.
func TestNodeEstimator_EstimateNodeCount_RealCatalogModels_A10(t *testing.T) {
	const (
		a10_1GPU = "Standard_NV36ads_A10_v5"
		a10_2GPU = "Standard_NV72ads_A10_v5"
	)
	runRealCatalogModelCases(t, []realCatalogModelCase{
		{"gpt-oss-20b/1xA10", "openai/gpt-oss-20b", a10_1GPU, 2},                        // 25.63Gi
		{"gpt-oss-20b/2xA10", "openai/gpt-oss-20b", a10_2GPU, 1},                        // 25.63Gi
		{"gemma-4-12B-it/1xA10", "google/gemma-4-12B-it", a10_1GPU, 2},                  // 22.28Gi
		{"gemma-4-12B-it/2xA10", "google/gemma-4-12B-it", a10_2GPU, 1},                  // 22.28Gi
		{"granite-4.1-8b/1xA10", "ibm-granite/granite-4.1-8b", a10_1GPU, 1},             // 16.38Gi
		{"Nemotron-Nano-9B-v2/1xA10", "nvidia/NVIDIA-Nemotron-Nano-9B-v2", a10_1GPU, 1}, // 16.56Gi
		{"Qwen3.5-9B/1xA10", "Qwen/Qwen3.5-9B", a10_1GPU, 2},                            // 17.98Gi, can't launch on 1 1xA10 node
		{"Qwen3.6-35B-A3B-FP8/1xA10", "Qwen/Qwen3.6-35B-A3B-FP8", a10_1GPU, 3},          // 34.90Gi
		{"Qwen3.6-35B-A3B-FP8/2xA10", "Qwen/Qwen3.6-35B-A3B-FP8", a10_2GPU, 2},          // 34.90Gi, can't launch on 1 2xA10 node
	})
}
