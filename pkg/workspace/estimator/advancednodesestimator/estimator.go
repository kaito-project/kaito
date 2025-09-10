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

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

// AdvancedNodesEstimator estimates node count based on SKU memory and model memory requirement
type AdvancedNodesEstimator struct {
	// no fields needed
}

func (c *AdvancedNodesEstimator) Name() string {
	return "advanced"
}

func (c *AdvancedNodesEstimator) EstimateNodeCount(ctx context.Context, workspace *kaitov1beta1.Workspace) (int32, error) {
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
	model := plugin.KaitoModelRegister.MustGet(presetName)

	gpuConfig, err := utils.GetGPUConfigBySKU(workspace.Resource.InstanceType)
	if err != nil {
		return 0, fmt.Errorf("failed to get GPU config for instance type %s: %w", workspace.Resource.InstanceType, err)
	}
	if gpuConfig == nil {
		return 0, fmt.Errorf("GPU config is nil for instance type %s", workspace.Resource.InstanceType)
	}

	// Start with the user-requested node count (default is 1)
	//nolint:staticcheck //SA1019: deprecate Resource.Count field
	nodeCountPerReplica := 1 // Default to 1 if not specified
	//nolint:staticcheck //SA1019: deprecate Resource.Count field
	if workspace.Resource.Count != nil {
		//nolint:staticcheck //SA1019: deprecate Resource.Count field
		nodeCountPerReplica = int(*workspace.Resource.Count)
	}

	// Use default max-model-len value
	maxModelLen := 2048 // Default value

	// If GPU memory information is available, calculate the optimal node count
	if gpuConfig.GPUMemGB > 0 && gpuConfig.GPUCount > 0 {
		totalGPUMemoryRequired := resource.MustParse(model.GetInferenceParameters().TotalSafeTensorFileSize)
		requiredMemoryBytes := int64(float64(totalGPUMemoryRequired.Value()) * 0.95) // vllm model size is about 95% percent of hugging face size
		totalGPUMemoryPerGPUBytes := int64(gpuConfig.GPUMemGB) * consts.GiBToBytes / int64(gpuConfig.GPUCount)
		availableGPUMemoryPerGPUBytes := int64(float64(totalGPUMemoryPerGPUBytes) * 0.9) // utilization is set to default 0.9

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
		optimizedNodes := (minGPUs + gpuConfig.GPUCount - 1) / gpuConfig.GPUCount

		// Optimization logic moved from preset_inferences.go
		if optimizedNodes < nodeCountPerReplica {
			klog.Infof("Optimizing node count from %d to %d based on GPU memory calculation", nodeCountPerReplica, optimizedNodes)
			nodeCountPerReplica = optimizedNodes
		}
	}

	return int32(nodeCountPerReplica), nil
}
