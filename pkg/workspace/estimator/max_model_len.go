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

package estimator

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	// gpuMemoryUtilization mirrors the --gpu-memory-utilization value vLLM is
	// launched with (see buildVLLMInferenceCommand in pkg/model/interface.go).
	// vLLM treats it as the hard cap on the fraction of total GPU memory used for
	// weights + activations + CUDA graphs + KV cache, so the estimator must use
	// the same value to predict the KV-cache budget vLLM will actually have.
	gpuMemoryUtilization = 0.84

	// weightExpansionFactor accounts for the ~2% expansion of model weights once
	// loaded by vLLM relative to the on-disk safetensor size.
	weightExpansionFactor = 1.02

	// BaseOverheadGiB is the model-independent part of vLLM's fixed per-GPU
	// overhead: non-torch allocations such as the CUDA context and NCCL buffers
	// (~0.6 GiB) plus a baseline for small-model activations and CUDA graphs
	// (~1.7 GiB). Larger models add to this via OverheadWeightFactor below.
	BaseOverheadGiB = 2.3

	// OverheadWeightFactor scales the runtime overhead with the per-GPU model
	// weight share. Peak activation memory and CUDA graph capture both grow with
	// hidden size / layer count and are sharded across TP ranks the same way
	// weights are, so the per-GPU weight share is a good proxy for them. vLLM
	// measures these empirically in determine_available_memory() and
	// profile_cudagraph_memory(). We approximate at best effort here.
	OverheadWeightFactor = 0.05
)

// MaxModelLenInput contains the inputs required to compute the optimal
// max model length for GPU memory efficiency. The struct is intentionally
// self-contained so that external callers do not have to depend on any
// internal Kaito model or SKU types.
type MaxModelLenInput struct {
	// ModelTokenLimit is the upper bound on tokens accepted by the model
	// (typically max_position_embeddings from the model config).
	ModelTokenLimit int
	// BytesPerToken is the per-token KV cache footprint, in bytes.
	BytesPerToken int
	// TotalModelWeightSize is a Kubernetes resource.Quantity formatted string
	// representing the total on-disk safetensor weight size (e.g. "25Gi").
	TotalModelWeightSize string
	// DisableTensorParallelism indicates the model does not shard weights
	// or KV cache across GPUs/nodes (e.g. Falcon-style models).
	DisableTensorParallelism bool
	// MLAReplicatedKVCache indicates the model uses Multi-head Latent Attention
	// (MLA), whose compressed latent KV cache is replicated on every tensor-parallel
	// rank rather than sharded across them. When true, the per-GPU KV footprint per
	// token is NOT divided by GPUCount. Mis-modeling this as sharded under-counts the
	// per-token KV by a factor of GPUCount and inflates the computed max-model-len
	// past what vLLM can honor (deterministic startup ValueError / crash-loop).
	MLAReplicatedKVCache bool
	// GPUMemoryBytes is the memory of a single GPU, in bytes.
	GPUMemoryBytes int64
	// GPUCount is the number of GPUs per node.
	GPUCount int
	// NumRequiredNodes is the number of nodes serving the model.
	NumRequiredNodes int
}

// ComputeMaxModelLen calculates the optimal max model length for GPU memory
// efficiency given the provided model, GPU, and topology inputs.
//
// Returns 0 when the inputs are invalid or insufficient memory is available.
func ComputeMaxModelLen(in MaxModelLenInput) int {
	if in.ModelTokenLimit <= 0 || in.BytesPerToken <= 0 ||
		in.GPUMemoryBytes <= 0 || in.GPUCount <= 0 || in.NumRequiredNodes <= 0 {
		return 0
	}

	weightGiB, ok := parseModelWeight(in.TotalModelWeightSize)
	if !ok {
		return 0
	}

	availableBytes, adjustedBytesPerToken := calculateMemoryParameters(in, weightGiB)
	if availableBytes <= 0 || adjustedBytesPerToken <= 0 {
		return 0
	}

	candidate := int(availableBytes / adjustedBytesPerToken)
	if candidate <= 0 {
		return 0
	}

	finalResult := applyConstraintsAndAlignment(candidate, in.ModelTokenLimit)

	klog.Infof("ComputeMaxModelLen: final result=%d", finalResult)
	return finalResult
}

// parseModelWeight parses the model weight string and returns the weight in GiB.
// It handles Kubernetes resource.Quantity format strings like "25.63Gi".
func parseModelWeight(totalModelWeightSize string) (float64, bool) {
	s := strings.TrimSpace(totalModelWeightSize)
	if s == "" {
		return 0, false
	}

	quantity, err := resource.ParseQuantity(s)
	if err != nil {
		return 0, false
	}

	bytes := quantity.Value()
	if bytes <= 0 {
		return 0, false
	}

	weightGiB := float64(bytes) / (1 << 30)
	if weightGiB <= 0 {
		return 0, false
	}

	return weightGiB, true
}

// calculateMemoryParameters computes available GPU memory and adjusted bytes per token.
// Returns the available memory in bytes and the adjusted bytes per token for the calculation.
func calculateMemoryParameters(in MaxModelLenInput, weightGiB float64) (float64, float64) {
	gpuMemGB := float64(in.GPUMemoryBytes) / (1 << 30)
	gpuCount := float64(in.GPUCount)
	nodes := float64(in.NumRequiredNodes)
	bytesPerToken := float64(in.BytesPerToken)

	// Available GPU memory per GPU. vLLM caps total usage at gpuMemoryUtilization;
	// within that budget the weights, KV cache, and the fixed runtime overhead
	// (staticOverheadGiB) must all fit.
	usableMemoryPerGPU := (gpuMemGB * gpuMemoryUtilization) / gpuCount

	// Model weight overhead per GPU with the weight expansion factor.
	var modelWeightOverhead float64
	if in.DisableTensorParallelism {
		// Falcon-style models: don't distribute weight across nodes/GPUs.
		modelWeightOverhead = weightGiB * weightExpansionFactor
	} else {
		modelWeightOverhead = (weightGiB * weightExpansionFactor) / (nodes * gpuCount)
	}

	// Fixed runtime overhead per GPU: a model-independent base plus a term that
	// scales with the per-GPU model weight share, approximating the activation and
	// CUDA-graph memory that grow with model size (see constant docs).
	staticOverhead := BaseOverheadGiB + OverheadWeightFactor*modelWeightOverhead

	availableMemoryGiB := usableMemoryPerGPU - modelWeightOverhead - staticOverhead
	availableMemoryBytes := availableMemoryGiB * (1 << 30)

	// Per-GPU per-token KV footprint. The KV cache is distributed across GPUs by
	// the parallelism strategy, mirroring how the model weights are sharded above
	// (weights are divided by nodes*gpuCount):
	//   - Tensor parallelism shards the KV heads across the gpuCount GPUs within a
	//     pipeline stage. MLA is the exception: its compressed latent KV is
	//     replicated on every TP rank, so it is NOT divided by gpuCount.
	//   - Pipeline parallelism (multi-node) splits the model's layers across the
	//     `nodes` stages, so each GPU holds only 1/nodes of the layers' KV.
	var adjustedBytesPerToken float64
	if in.DisableTensorParallelism {
		// Falcon-style models replicate the full model on each GPU (no TP/PP
		// sharding), so the per-GPU KV footprint is the full per-token size.
		adjustedBytesPerToken = bytesPerToken
	} else if in.MLAReplicatedKVCache {
		// MLA: latent KV replicated across TP ranks (no /gpuCount), but pipeline
		// parallelism still splits layers across nodes (/nodes).
		adjustedBytesPerToken = bytesPerToken / nodes
	} else {
		// Standard attention: sharded by TP (/gpuCount) and split by PP (/nodes).
		adjustedBytesPerToken = bytesPerToken / gpuCount / nodes
	}

	return availableMemoryBytes, adjustedBytesPerToken
}

// applyConstraintsAndAlignment clamps candidate to modelTokenLimit and aligns
// down to a 256-token boundary for efficient memory allocation.
func applyConstraintsAndAlignment(candidate, modelTokenLimit int) int {
	originalCandidate := candidate
	if modelTokenLimit > 0 && candidate > modelTokenLimit {
		candidate = modelTokenLimit
		klog.Infof("ComputeMaxModelLen: clamped to ModelTokenLimit: %d -> %d", originalCandidate, candidate)
	}

	candidate = (candidate / 256) * 256
	return candidate
}
