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

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
	estimator "github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	// gpuMemoryUtilization mirrors the --gpu-memory-utilization value vLLM is
	// launched with (see buildVLLMInferenceCommand in pkg/model/interface.go).
	// vLLM treats it as the hard cap on the fraction of total GPU memory used for
	// weights + activations + CUDA graphs + KV cache, so the estimator must use
	// the same value to predict the per-GPU memory budget vLLM will actually have.
	gpuMemoryUtilization = 0.84

	// weightExpansionFactor accounts for the ~2% expansion of model weights once
	// loaded by vLLM relative to the on-disk safetensor size.
	weightExpansionFactor = 1.02

	// baseOverheadGiB is the model-independent part of vLLM's fixed per-GPU
	// overhead: non-torch allocations such as the CUDA context and NCCL buffers
	// (~0.6 GiB) plus a baseline for small-model activations and CUDA graphs
	// (~1.7 GiB). Larger models add to this via overheadWeightFactor below.
	baseOverheadGiB = 2.3

	// overheadWeightFactor scales the runtime overhead with the per-GPU model
	// weight share. Peak activation memory and CUDA graph capture both grow with
	// hidden size / layer count and are sharded across TP ranks the same way
	// weights are, so the per-GPU weight share is a good proxy for them. vLLM
	// measures these empirically in determine_available_memory() and
	// profile_cudagraph_memory(). We approximate at best effort here.
	overheadWeightFactor = 0.05
)

// NodeEstimator estimates node count based on SKU memory and model memory requirement
type NodeEstimator struct {
	// no fields needed
}

func (c *NodeEstimator) Name() string {
	return "node-estimator"
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

	// Resolve the GPU configuration for a single node.
	var gpuConfig *sku.GPUConfig
	if req.ResourceProfile.DisableNodeAutoProvisioning {
		// NAP is disabled (BYO scenario).
		if req.ResourceProfile.MIGProfile != "" {
			// MIG partition: a single, non-shardable slice (GPUCount == 1). The
			// model must fit one slice, which is enforced by the IsMIG check after
			// the fit calculation below. MIG is only supported when NAP is disabled.
			gpuConfig, err = utils.GetMIGGPUConfig(req.ResourceProfile.MIGProfile)
			if err != nil {
				return 0, fmt.Errorf("failed to get MIG GPU config: %w", err)
			}
		} else {
			// Derive GPU config from existing ready nodes.
			matchLabels := client.MatchingLabels(kaitov1beta1.SanitizedMatchLabels(req.ResourceProfile.LabelSelector))
			nodeList, listErr := nodes.ListNodes(ctx, cl, matchLabels)
			if listErr != nil {
				return 0, fmt.Errorf("failed to list ready nodes: %w", listErr)
			}
			var readyNodes []*corev1.Node
			for i := range nodeList.Items {
				if nodes.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
					readyNodes = append(readyNodes, &nodeList.Items[i])
				}
			}
			if len(readyNodes) == 0 {
				return 0, fmt.Errorf("no ready nodes found, unable to determine GPU configuration")
			}
			gpuConfig, err = sku.GetGPUConfigFromNodeLabels(readyNodes[0])
			if err != nil {
				return 0, fmt.Errorf("failed to get GPU config from existing nodes: %w", err)
			}
		}
	} else {
		// NAP is enabled — instanceType is required and must be valid.
		gpuConfig, err = sku.GetGPUConfigBySKU(req.ResourceProfile.InstanceType)
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
		modelSize := float64(totalGPUMemRequired.Value()) * weightExpansionFactor // vllm model size is about 102% of HuggingFace size
		gpuMemPerGPU := float64(gpuConfig.GPUMem.Value() / int64(gpuConfig.GPUCount))
		availGPUMem := gpuMemPerGPU * gpuMemoryUtilization // utilization is set to default 0.84

		// Overhead: a fixed base plus the KV cache for the
		// context length, plus a term that scales with the per-GPU model weight
		// share (overheadWeightFactor). For the tensor-parallel (sharded)
		// case the weight-scaled term folds into the (1 + overheadWeightFactor)
		// divisor below, keeping the solve non-circular.
		baseOverhead := baseOverheadGiB * float64(consts.GiBToBytes)
		kvCache := float64(maxModelLen*inferParams.BytesPerToken) / float64(gpuConfig.GPUCount)
		fixedReserve := baseOverhead + kvCache

		if availGPUMem <= fixedReserve {
			return 0, fmt.Errorf("GPU memory %.0f bytes is too small, needs at least %.1f GB overhead (base: %.1fGB + KV Cache: %.1f GB)",
				gpuMemPerGPU, fixedReserve/float64(consts.GiBToBytes), baseOverheadGiB, kvCache/float64(consts.GiBToBytes))
		}

		// Per-GPU memory available for model weights. The weight-scaled overhead
		// (overheadWeightFactor x per-GPU weight) folds into the (1 + factor) divisor.
		availMemPerGPU := (availGPUMem - fixedReserve) / (1 + overheadWeightFactor)
		minGPUs := int(modelSize/availMemPerGPU) + 1
		nodeCountPerReplica = (minGPUs + gpuConfig.GPUCount - 1) / gpuConfig.GPUCount

		klog.Infof("modelSize(%.0f), gpuMemPerGPU(%.0f), availGPUMem(%.0f), fixedReserve(%.0f), availMemPerGPU(%.0f), minGPUs(%d) => nodeCountPerReplica(%d) for workspace %s",
			modelSize, gpuMemPerGPU, availGPUMem, fixedReserve, availMemPerGPU, minGPUs, nodeCountPerReplica, req.WorkspaceName)

		// MIG partitions are a single, non-shardable device: the model plus its
		// runtime overhead must fit one slice. Report the slice-specific shortfall
		// instead of scaling to multiple GPUs/nodes.
		if gpuConfig.IsMIG && nodeCountPerReplica > 1 {
			overhead := fixedReserve + overheadWeightFactor*modelSize
			sliceGiB := gpuMemPerGPU / float64(consts.GiBToBytes)
			return 0, fmt.Errorf("model needs %.1fGB (weights %.1fGB + overhead %.1fGB) but MIG profile %s only provides %.0fGB (%.1fGB available after vLLM gpu-memory-utilization)",
				(modelSize+overhead)/float64(consts.GiBToBytes),
				modelSize/float64(consts.GiBToBytes),
				overhead/float64(consts.GiBToBytes),
				req.ResourceProfile.MIGProfile,
				sliceGiB, availGPUMem/float64(consts.GiBToBytes))
		}

		if nodeCountPerReplica > 1 && !model.SupportDistributedInference() {
			return 0, fmt.Errorf("models with disabled support distributed inference cannot be distributed across more than 1 GPU node, please use a node with larger GPU memory, calculated nodes: %d", nodeCountPerReplica)
		}
	}

	klog.Infof("[NodeEstimator] Final result: nodeCountPerReplica=%d for workspace %s", nodeCountPerReplica, req.WorkspaceName)
	return int32(nodeCountPerReplica), nil
}
