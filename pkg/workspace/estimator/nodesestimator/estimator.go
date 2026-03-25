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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	estimator "github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// NodeEstimator estimates node count based on SKU memory and model memory requirement
type NodeEstimator struct {
	// no fields needed
}

func (c *NodeEstimator) Name() string {
	return "default"
}

func (c *NodeEstimator) EstimateNodeCount(ctx context.Context, req estimator.NodeEstimateRequest, cl client.Client) (int32, error) {
	// If no preset is configured, default to the requested node count or 1.
	if req.ModelProfile.Name == "" {
		if req.ResourceProfile.RequestedNodeCount > 0 {
			return int32(req.ResourceProfile.RequestedNodeCount), nil
		}
		return 1, nil
	}

	model, err := models.GetModelByNameWithToken(ctx, req.ModelProfile.Name, req.ModelProfile.AccessToken)
	if err != nil {
		return 0, fmt.Errorf("failed to get model by name: %w", err)
	}

	var gpuConfig *sku.GPUConfig

	if req.ResourceProfile.DisableNodeAutoProvisioning {
		// NAP is disabled (BYO scenario) — derive GPU config from existing ready nodes.
		var matchLabels client.MatchingLabels
		if req.ResourceProfile.LabelSelector != nil {
			matchLabels = req.ResourceProfile.LabelSelector.MatchLabels
		}
		nodeList, listErr := resources.ListNodes(ctx, cl, matchLabels)
		if listErr != nil {
			return 0, fmt.Errorf("failed to list ready nodes: %w", listErr)
		}
		var readyNodes []*corev1.Node
		for i := range nodeList.Items {
			if resources.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
				readyNodes = append(readyNodes, &nodeList.Items[i])
			}
		}
		if len(readyNodes) == 0 {
			return 0, fmt.Errorf("no ready nodes found, unable to determine GPU configuration")
		}
		gpuConfig, err = utils.GetGPUConfigFromNodeLabels(readyNodes[0])
		if err != nil {
			return 0, fmt.Errorf("failed to get GPU config from existing nodes: %w", err)
		}
	} else {
		// NAP is enabled — instanceType is required and must be valid.
		gpuConfig, err = utils.GetGPUConfigBySKU(req.ResourceProfile.InstanceType)
		if err != nil {
			return 0, fmt.Errorf("failed to get GPU config for instance type %s: %w", req.ResourceProfile.InstanceType, err)
		}
	}

	// Start with the user-requested node count (default is 1).
	nodeCountPerReplica := 1
	if req.ResourceProfile.RequestedNodeCount > 0 {
		nodeCountPerReplica = req.ResourceProfile.RequestedNodeCount
	}

	// maxModelLen: use the value resolved by the caller (RuntimeProfile.ContextSize), falling back to 2048.
	maxModelLen := 2048
	if req.RuntimeProfile.ContextSize > 0 {
		maxModelLen = req.RuntimeProfile.ContextSize
	}

	klog.Infof("[NodeEstimator] workspace=%s maxModelLen=%d", req.WorkspaceName, maxModelLen)

	// If GPU memory information is available, calculate the optimal node count
	if !gpuConfig.GPUMem.IsZero() && gpuConfig.GPUCount > 0 {
		inferParams := model.GetInferenceParameters()
		totalGPUMemRequired := resource.MustParse(inferParams.TotalSafeTensorFileSize)
		modelSize := float64(totalGPUMemRequired.Value()) * 1.02 // vllm model size is about 102% of HuggingFace size
		gpuMemPerGPU := float64(gpuConfig.GPUMem.Value() / int64(gpuConfig.GPUCount))
		availGPUMem := gpuMemPerGPU * 0.84 // utilization is set to default 0.84

		// Overhead: fixed base (2.3GB) + KV cache for context length
		kvCache := float64(maxModelLen*inferParams.BytesPerToken) / float64(gpuConfig.GPUCount)
		overhead := 2.3*consts.GiBToBytes + kvCache

		if inferParams.DisableTensorParallelism && modelSize+overhead > availGPUMem {
			return 0, fmt.Errorf("GPU memory %.0f bytes is too small for model, needs %.0f bytes (model: %.0f + overhead: %.0f)",
				gpuMemPerGPU, modelSize+overhead, modelSize, overhead)
		}

		if availGPUMem <= overhead {
			return 0, fmt.Errorf("GPU memory %.0f bytes is too small, needs at least %.1f GB overhead (base: 2.3GB + KV Cache: %.1f GB)",
				gpuMemPerGPU, overhead/float64(consts.GiBToBytes), kvCache/float64(consts.GiBToBytes))
		}

		availMemPerGPU := availGPUMem - overhead
		minGPUs := int(modelSize/availMemPerGPU) + 1
		nodeCountPerReplica = (minGPUs + gpuConfig.GPUCount - 1) / gpuConfig.GPUCount

		klog.Infof("modelSize(%.0f), gpuMemPerGPU(%.0f), availGPUMem(%.0f), overhead(%.0f), availMemPerGPU(%.0f), minGPUs(%d) => nodeCountPerReplica(%d) for workspace %s",
			modelSize, gpuMemPerGPU, availGPUMem, overhead, availMemPerGPU, minGPUs, nodeCountPerReplica, req.WorkspaceName)

		if nodeCountPerReplica > 1 && inferParams.DisableTensorParallelism {
			return 0, fmt.Errorf("models with disabled tensor parallelism cannot be distributed across more than 1 GPU node, calculated nodes: %d", nodeCountPerReplica)
		}

		if nodeCountPerReplica > 1 && !model.SupportDistributedInference() {
			return 0, fmt.Errorf("models with disabled support distributed inference cannot be distributed across more than 1 GPU node, please use a node with larger GPU memory, calculated nodes: %d", nodeCountPerReplica)
		}
	}

	klog.Infof("[NodeEstimator] Final result: nodeCountPerReplica=%d for workspace %s", nodeCountPerReplica, req.WorkspaceName)
	return int32(nodeCountPerReplica), nil
}
