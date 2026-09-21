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
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
	estimator "github.com/kaito-project/kaito/pkg/workspace/estimator"
)

const (
	// mambaStateReferenceConcurrency is the reference number of concurrent
	// sequences used to size the per-GPU Mamba-2 state reservation for hybrid
	// models. It mirrors how the KV-cache term uses a fixed reference context
	// length: a representative serving batch rather than vLLM's max_num_seqs.
	mambaStateReferenceConcurrency = 64
)

// NodeEstimator estimates node count based on SKU memory and model memory requirement
type NodeEstimator struct {
	// no fields needed
}

func (c *NodeEstimator) Name() string {
	return "node-estimator"
}

func (c *NodeEstimator) EstimateNodeCount(ctx context.Context, req estimator.NodeEstimateRequest, cl client.Client) (int32, error) {
	// If no model is configured, default to the requested node count or 1.
	model := req.ModelProfile.Model
	if model == nil {
		if req.ResourceProfile.RequestedNodeCount > 0 {
			return int32(req.ResourceProfile.RequestedNodeCount), nil
		}
		return 1, nil
	}

	// Resolve the GPU configuration for a single node.
	var gpuConfig *sku.GPUConfig
	var err error
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
			// Use a node matching the effective SKU when available. Without one,
			// retain the legacy behavior of sizing from the first ready node.
			sizingNode := readyNodes[0]
			if req.ResourceProfile.InstanceType != "" {
				for _, n := range readyNodes {
					if n.Labels[corev1.LabelInstanceTypeStable] == req.ResourceProfile.InstanceType {
						sizingNode = n
						break
					}
				}
			}
			gpuConfig, err = sku.GetGPUConfigFromNodeLabels(sizingNode)
			if err != nil {
				return 0, fmt.Errorf("failed to get GPU config from existing nodes: %w", err)
			}
			if req.ResourceProfile.AcceleratorCount > 0 {
				gpuConfig, err = sku.ScaleGPUConfigToCount(gpuConfig, req.ResourceProfile.AcceleratorCount)
				if err != nil {
					return 0, err
				}
			}
		}
	} else {
		// NAP is enabled — instanceType is required and must be valid.
		gpuConfig, err = sku.GetGPUConfigBySKU(req.ResourceProfile.InstanceType)
		if err != nil {
			return 0, fmt.Errorf("failed to get GPU config for instance type %s: %w", req.ResourceProfile.InstanceType, err)
		}
	}

	// maxModelLen: use the value resolved by the caller (RuntimeProfile.ContextSize), falling back to 2048.
	maxModelLen := 2048
	if req.RuntimeProfile.ContextSize > 0 {
		maxModelLen = req.RuntimeProfile.ContextSize
	}

	klog.Infof("[NodeEstimator] workspace=%s maxModelLen=%d", req.WorkspaceName, maxModelLen)

	return ComputeNodeCountForGPUConfig(model, gpuConfig, maxModelLen, req.ResourceProfile.RequestedNodeCount, req.ResourceProfile.MIGProfile, req.WorkspaceName)
}

// ComputeNodeCountForGPUConfig returns the node count required to serve model m on
// nodes with the given per-node gpuConfig and resolved maxModelLen. It performs no
// cluster I/O: callers that already know the GPU configuration (e.g. BYO SKU
// selection, which sizes each candidate SKU) share this core sizing math with
// EstimateNodeCount, which resolves gpuConfig first and delegates here.
//
// requestedNodeCount is the caller-preferred count (0 means unspecified, default 1);
// migProfile is used only for MIG-specific error messages; wsName is for logging.
func ComputeNodeCountForGPUConfig(m pkgmodel.Model, gpuConfig *sku.GPUConfig, maxModelLen, requestedNodeCount int, migProfile, wsName string) (int32, error) {
	nodeCountPerReplica := 1
	if requestedNodeCount > 0 {
		nodeCountPerReplica = requestedNodeCount
	}

	// If GPU memory information is available, calculate the optimal node count
	if !gpuConfig.GPUMem.IsZero() && gpuConfig.GPUCount > 0 {
		inferParams := m.GetInferenceParameters()
		totalGPUMemRequired := resource.MustParse(inferParams.TotalSafeTensorFileSize)
		modelSize := float64(totalGPUMemRequired.Value()) * estimator.WeightExpansionFactor // vllm model size is about 102% of HuggingFace size
		gpuMemPerGPU := float64(gpuConfig.GPUMem.Value() / int64(gpuConfig.GPUCount))
		availGPUMem := gpuMemPerGPU * estimator.ResolveGPUMemoryUtilization(gpuConfig.GPUModel)

		// Overhead: a fixed base plus the KV cache for the
		// context length, plus a term that scales with the per-GPU model weight
		// share (OverheadWeightFactor). For the tensor-parallel (sharded)
		// case the weight-scaled term folds into the (1 + OverheadWeightFactor)
		// divisor below, keeping the solve non-circular.
		baseOverheadGiBForGPU := estimator.ResolveBaseOverheadGiB(gpuConfig.GPUModel)
		baseOverhead := baseOverheadGiBForGPU * float64(consts.GiBToBytes)
		kvCache := float64(maxModelLen*inferParams.BytesPerToken) / float64(gpuConfig.GPUCount)
		fixedReserve := baseOverhead + kvCache

		if availGPUMem <= fixedReserve {
			return 0, fmt.Errorf("GPU memory %.0f bytes is too small, needs at least %.1f GB overhead (base: %.1fGB + KV Cache: %.1f GB)",
				gpuMemPerGPU, fixedReserve/float64(consts.GiBToBytes), baseOverheadGiBForGPU, kvCache/float64(consts.GiBToBytes))
		}

		// Per-GPU memory available for model weights. The weight-scaled overhead
		// (OverheadWeightFactor x per-GPU weight) folds into the (1 + factor) divisor.
		availMemPerGPU := (availGPUMem - fixedReserve) / (1 + estimator.OverheadWeightFactor)

		// Hybrid Mamba/Attention models (e.g. NemotronH) allocate a per-sequence
		// Mamba-2 state cache in addition to the attention KV cache. Like weights it
		// shards across TP ranks, so fold the reservation for a reference serving
		// concurrency into the total sharded footprint. Zero for pure-attention models.
		mambaState := float64(inferParams.MambaStateBytesPerSeq * mambaStateReferenceConcurrency)
		minGPUs := int((modelSize+mambaState)/availMemPerGPU) + 1
		nodeCountPerReplica = (minGPUs + gpuConfig.GPUCount - 1) / gpuConfig.GPUCount

		klog.Infof("modelSize(%.0f), mambaState(%.0f), gpuMemPerGPU(%.0f), availGPUMem(%.0f), fixedReserve(%.0f), availMemPerGPU(%.0f), minGPUs(%d) => nodeCountPerReplica(%d) for workspace %s",
			modelSize, mambaState, gpuMemPerGPU, availGPUMem, fixedReserve, availMemPerGPU, minGPUs, nodeCountPerReplica, wsName)

		// MIG partitions are a single, non-shardable device: the model plus its
		// runtime overhead must fit one slice. Report the slice-specific shortfall
		// instead of scaling to multiple GPUs/nodes.
		if gpuConfig.IsMIG && nodeCountPerReplica > 1 {
			overhead := fixedReserve + estimator.OverheadWeightFactor*modelSize
			sliceGiB := gpuMemPerGPU / float64(consts.GiBToBytes)
			return 0, fmt.Errorf("model needs %.1fGB (weights %.1fGB + overhead %.1fGB) but MIG profile %s only provides %.0fGB (%.1fGB available after vLLM gpu-memory-utilization)",
				(modelSize+overhead)/float64(consts.GiBToBytes),
				modelSize/float64(consts.GiBToBytes),
				overhead/float64(consts.GiBToBytes),
				migProfile,
				sliceGiB, availGPUMem/float64(consts.GiBToBytes))
		}

		if nodeCountPerReplica > 1 && !m.SupportDistributedInference() {
			return 0, fmt.Errorf("models with disabled support distributed inference cannot be distributed across more than 1 GPU node, please use a node with larger GPU memory, calculated nodes: %d", nodeCountPerReplica)
		}
	}

	klog.Infof("[NodeEstimator] Final result: nodeCountPerReplica=%d for workspace %s", nodeCountPerReplica, wsName)
	return int32(nodeCountPerReplica), nil
}
