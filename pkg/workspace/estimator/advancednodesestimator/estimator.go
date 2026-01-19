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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// AdvancedNodesEstimator estimates node count based on SKU memory and model memory requirement
type AdvancedNodesEstimator struct {
	// no fields needed
}

func (c *AdvancedNodesEstimator) Name() string {
	return "advanced"
}

func (c *AdvancedNodesEstimator) EstimateNodeCount(ctx context.Context, workspace *kaitov1beta1.Workspace, client client.Client) (int32, error) {
	// If inference is not configured, default to resource count or 1
	if workspace.Inference == nil || workspace.Inference.Preset == nil || workspace.Inference.Preset.Name == "" {
		//nolint:staticcheck //SA1019: deprecate Resource.Count field
		if workspace.Resource.Count != nil {
			//nolint:staticcheck //SA1019: deprecate Resource.Count field
			return int32(*workspace.Resource.Count), nil
		}
		return 1, nil
	}

	presetName := string(workspace.Inference.Preset.Name)
	secretName := workspace.Inference.Preset.PresetOptions.ModelAccessSecret
	model, err := models.GetModelByName(ctx, presetName, secretName, workspace.Namespace, client)
	if err != nil {
		return 0, fmt.Errorf("failed to get model by name: %w", err)
	}

	var gpuConfig *sku.GPUConfig

	if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		if readyNodes, err := resources.GetReadyNodes(ctx, client, workspace); err != nil {
			return 0, fmt.Errorf("failed to list ready nodes: %w", err)
		} else if len(readyNodes) == 0 {
			return 0, fmt.Errorf("no ready nodes found, unable to determine GPU configuration")
		} else {
			gpuConfig, err = utils.GetGPUConfigFromNodeLabels(readyNodes[0])
			if err != nil {
				return 0, fmt.Errorf("failed to get GPU config from existing nodes: %w", err)
			}
		}
	} else {
		// NAP is enabled - instanceType is required and must be valid
		gpuConfig, err = utils.GetGPUConfigBySKU(workspace.Resource.InstanceType)
		if err != nil {
			return 0, fmt.Errorf("failed to get GPU config for instance type %s: %w", workspace.Resource.InstanceType, err)
		}
	}

	// Start with the user-requested node count (default is 1)
	//nolint:staticcheck //SA1019: deprecate Resource.Count field
	nodeCountPerReplica := 1 // Default to 1 if not specified
	//nolint:staticcheck //SA1019: deprecate Resource.Count field
	if workspace.Resource.Count != nil {
		//nolint:staticcheck //SA1019: deprecate Resource.Count field
		nodeCountPerReplica = int(*workspace.Resource.Count)
	}

	// maxModelLen logic:
	// 1. If user has no custom config, use default 2048
	// 2. If user has custom configmap but no max-model-len, use 2048
	// 3. If user has configmap with max-model-len, use user's value
	maxModelLen := 2048

	if workspace.Inference.Config != "" {
		// User has custom ConfigMap, try to read it
		configMapName := workspace.Inference.Config
		configMap := &corev1.ConfigMap{}
		err := resources.GetResource(ctx, configMapName, workspace.Namespace, client, configMap)
		if err != nil {
			klog.Warningf("[AdvancedEstimator] Failed to get ConfigMap %s: %v, using default maxModelLen=%d", configMapName, err, maxModelLen)
		} else {
			// Parse the ConfigMap content for max-model-len
			if configData, exists := configMap.Data["inference_config.yaml"]; exists {
				if userMaxModelLen, found := utils.ParseExplicitMaxModelLen(configData); found {
					maxModelLen = userMaxModelLen
					klog.Infof("[AdvancedEstimator] workspace=%s using user explicit max-model-len=%d from ConfigMap %s", workspace.Name, maxModelLen, configMapName)
				}
			}
		}
	}

	klog.Infof("[AdvancedEstimator] workspace=%s maxModelLen=%d", workspace.Name, maxModelLen)

	// If GPU memory information is available, calculate the optimal node count
	if gpuConfig.GPUMemGiB > 0 && gpuConfig.GPUCount > 0 {
		totalGPUMemoryRequired := resource.MustParse(model.GetInferenceParameters().TotalSafeTensorFileSize)
		requiredMemoryBytes := int64(float64(totalGPUMemoryRequired.Value()) * 1.02) // vllm model size is about 102% percent of hugging face size
		totalGPUMemoryPerGPUBytes := int64(gpuConfig.GPUMemGiB) * consts.GiBToBytes / int64(gpuConfig.GPUCount)
		availableGPUMemoryPerGPUBytes := int64(float64(totalGPUMemoryPerGPUBytes) * 0.84) // utilization is set to default 0.84

		// Overhead calculation: fixed base overhead (2.3GB) + model length overhead
		// Following the same algorithm as preset_inferences.go
		baseOverhead := 2.3 * consts.GiBToBytes // Convert 2.3 GB to bytes
		kvCache := float64(maxModelLen*model.GetInferenceParameters().BytesPerToken) / float64(gpuConfig.GPUCount)
		overhead := baseOverhead + kvCache // KV cache overhead for the given token length

		// Special case for models that disable tensor parallelism: check if required memory + overhead fits in GPU memory
		if model.GetInferenceParameters().DisableTensorParallelism {
			if int64(requiredMemoryBytes)+int64(overhead) > availableGPUMemoryPerGPUBytes {
				return 0, fmt.Errorf("GPU memory %d bytes is too small for model, needs %d bytes (model: %d + overhead: %.0f)",
					totalGPUMemoryPerGPUBytes, int64(requiredMemoryBytes)+int64(overhead), requiredMemoryBytes, overhead)
			}
		}

		if float64(availableGPUMemoryPerGPUBytes) <= overhead {
			return 0, fmt.Errorf("GPU memory %d bytes is too small, needs at least %.1f GB overhead (base: 2.3GB + Advanced KV Cache: %.1f GB)",
				totalGPUMemoryPerGPUBytes, overhead/float64(consts.GiBToBytes), kvCache/float64(consts.GiBToBytes))
		}

		availableMemoryPerGPU := float64(availableGPUMemoryPerGPUBytes) - overhead
		minGPUs := int(float64(requiredMemoryBytes)/availableMemoryPerGPU) + 1 // Ceiling

		// Calculate minimum nodes: we need minGPUs GPU groups
		// If each node has gpuConfig.GPUCount GPUs, we need ceil(minGPUs / gpuConfig.GPUCount) nodes
		nodeCountPerReplica = (minGPUs + gpuConfig.GPUCount - 1) / gpuConfig.GPUCount

		klog.Infof("totalGPUMemoryRequired(%s), requiredMemoryBytes(%d), totalGPUMemoryPerGPUBytes(%d), availableGPUMemoryPerGPUBytes(%d), overhead(%.0f), availableMemoryPerGPU(%.0f), minGPUs(%d) => nodeCountPerReplica(%d) for workspace %s",
			totalGPUMemoryRequired.String(), requiredMemoryBytes, totalGPUMemoryPerGPUBytes, availableGPUMemoryPerGPUBytes, overhead, availableMemoryPerGPU, minGPUs, nodeCountPerReplica, workspace.Name)

		// Special case for models with disabled tensor parallelism: they cannot be distributed across multiple nodes
		if nodeCountPerReplica > 1 && model.GetInferenceParameters().DisableTensorParallelism {
			return 0, fmt.Errorf("models with disabled tensor parallelism cannot be distributed across more than 1 GPU node, calculated nodes: %d", nodeCountPerReplica)
		}

		if nodeCountPerReplica > 1 && !model.SupportDistributedInference() {
			return 0, fmt.Errorf("models with disabled support distributed inference cannot be distributed across more than 1 GPU node, please use a node with larger GPU memory, calculated nodes: %d", nodeCountPerReplica)
		}
	}

	klog.Infof("[AdvancedEstimator] Final result: nodeCountPerReplica=%d for workspace %s", nodeCountPerReplica, workspace.Name)
	return int32(nodeCountPerReplica), nil
}
