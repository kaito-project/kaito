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

package maxnumseqestimator

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	estimator "github.com/kaito-project/kaito/pkg/workspace/estimator"
)

const (
	// safetyFactor scales the estimated Mamba-cache-block ceiling down to a
	// --max-num-seqs value, leaving margin for runtime factors this estimate does
	// not model exactly (page rounding, kv-cache dtype, LoRA, speculative decoding).
	// Bias low: underestimating is a safe engine start, while overestimating
	// hard-fails at CUDA graph capture after the weights are already loaded.
	safetyFactor = 0.9

	// largeGPUMemThresholdGiB, largeGPUDefaultMaxNumSeqs and defaultMaxNumSeqs
	// mirror vLLM's own default_max_num_seqs selection (see vLLMDefaultMaxNumSeqs).
	largeGPUMemThresholdGiB   = 70
	largeGPUDefaultMaxNumSeqs = 1024
	defaultMaxNumSeqs         = 256

	// paddingHeuristicRatio mirrors the 1.5 constant vLLM uses when choosing a KV
	// cache group size (see resolveGroupSize).
	paddingHeuristicRatio = 1.5
)

// vLLMDefaultMaxNumSeqs returns the max_num_seqs vLLM picks for the OpenAI API
// server when the caller does not pass one, mirroring
// _set_default_max_num_seqs_and_batched_tokens_args in vllm/engine/arg_utils.py:
// https://github.com/vllm-project/vllm/blob/releases/v0.25.1/vllm/engine/arg_utils.py#L2423-L2445
// A100 is name-excluded from the large-GPU branch upstream because large batches
// regress its throughput (vLLM PR #17885), so it keeps the smaller default even
// though it clears the memory threshold.
func vLLMDefaultMaxNumSeqs(gpuConfig *sku.GPUConfig) int {
	memPerGPU := gpuConfig.GPUMem.Value() / int64(gpuConfig.GPUCount)
	isA100 := strings.Contains(strings.ToLower(gpuConfig.GPUModel), "a100")
	if memPerGPU >= int64(largeGPUMemThresholdGiB*consts.GiBToBytes) && !isA100 {
		return largeGPUDefaultMaxNumSeqs
	}
	return defaultMaxNumSeqs
}

// resolveGroupSize returns the number of layers vLLM places in each KV cache group,
// mirroring _get_kv_cache_groups_uniform_page_size in vllm/v1/core/kv_cache_utils.py:
// the group size is the smallest per-attention-type layer count, unless the largest
// type is within 1.5x of it, in which case the largest is used to avoid padding.
// vLLM buckets layers by full KV cache spec; we approximate with the two types a
// hybrid model has (linear/Mamba and full attention).
func resolveGroupSize(numLinear, numFull int) int {
	minLayers := min(numLinear, numFull)
	maxLayers := max(numLinear, numFull)
	if float64(maxLayers) < paddingHeuristicRatio*float64(minLayers) {
		return maxLayers
	}
	return minLayers
}

// MaxNumSeqsEstimateRequest holds the inputs needed to estimate --max-num-seqs.
type MaxNumSeqsEstimateRequest struct {
	// WorkspaceName is used for logging and diagnostics.
	WorkspaceName string
	// InferenceParams carries the model's hybrid-state metadata and weight size.
	InferenceParams *pkgmodel.PresetParam
	// GPUConfig describes the GPU on a single node.
	GPUConfig *sku.GPUConfig
	// NumNodes is the node count per replica; > 1 means pipeline parallelism.
	NumNodes int
}

// MaxNumSeqsEstimator estimates a safe --max-num-seqs for hybrid
// Mamba/Gated-DeltaNet models.
type MaxNumSeqsEstimator struct {
	// no fields needed
}

func (c *MaxNumSeqsEstimator) Name() string {
	return "max-num-seqs-estimator"
}

// Estimate returns a --max-num-seqs value that keeps vLLM engine initialization
// below the available Mamba cache blocks. This is needed because hybrid models
// allocate one Mamba cache block per decode sequence, and vLLM hard-fails at CUDA
// graph capture when max_num_seqs exceeds the number of blocks it could allocate:
// https://github.com/vllm-project/vllm/issues/49064.
// Internally, vLLM pads the attention block size up to the per-layer Mamba page so
// every KV cache group shares one page size, and it charges one page per layer in
// a group, so:
//
//	num_blocks ≈ availPool / (mambaStatePerLayerPerRank × groupSize)
//
// Returns (value, true) when the model is hybrid, runs on a single node, and the
// cap actually reduces below vLLM's default. Returns (0, false) otherwise, leaving
// vLLM's own default in place.
// TODO: remove this estimator once auto-clamping is supported for max-num-seqs in vLLM.
func (c *MaxNumSeqsEstimator) Estimate(req MaxNumSeqsEstimateRequest) (int, bool) {
	params := req.InferenceParams
	gpuConfig := req.GPUConfig
	if params == nil || gpuConfig == nil || gpuConfig.GPUCount <= 0 || gpuConfig.GPUMem.IsZero() {
		return 0, false
	}
	// Pure-attention models have no Mamba state, so vLLM's default always fits.
	if params.MambaStateBytesPerLayer <= 0 || params.NumFullAttnLayers <= 0 || params.NumLinearLayers <= 0 {
		return 0, false
	}
	// Multi-node (pipeline-parallel) block accounting is not modeled here; leave
	// vLLM's default in place rather than guessing.
	if req.NumNodes > 1 {
		return 0, false
	}
	weights, err := resource.ParseQuantity(params.TotalSafeTensorFileSize)
	if err != nil || weights.IsZero() {
		return 0, false
	}

	tp := gpuConfig.GPUCount
	gpuMemPerGPU := float64(gpuConfig.GPUMem.Value() / int64(tp))
	availGPUMem := gpuMemPerGPU * estimator.ResolveGPUMemoryUtilization(gpuConfig.GPUModel)
	weightsPerGPU := float64(weights.Value()) * estimator.WeightExpansionFactor / float64(tp)
	baseOverhead := estimator.ResolveBaseOverheadGiB(gpuConfig.GPUModel) * float64(consts.GiBToBytes)

	// Memory vLLM has left for the unified KV cache / Mamba state pool after
	// weights and fixed runtime overhead.
	availPool := availGPUMem - weightsPerGPU - baseOverhead - estimator.OverheadWeightFactor*weightsPerGPU
	if availPool <= 0 {
		return 0, false
	}

	perLayerPerRank := float64(params.MambaStateBytesPerLayer) / float64(tp)
	groupSize := resolveGroupSize(params.NumLinearLayers, params.NumFullAttnLayers)
	pageBytes := perLayerPerRank * float64(groupSize)
	if pageBytes <= 0 {
		return 0, false
	}
	numBlocks := availPool / pageBytes

	maxNumSeqs := int(safetyFactor * numBlocks)
	if maxNumSeqs < 1 {
		maxNumSeqs = 1
	}
	// Only cap when it actually reduces below the default vLLM would have picked;
	// otherwise that default already fits within the available blocks.
	defaultSeqs := vLLMDefaultMaxNumSeqs(gpuConfig)
	if maxNumSeqs >= defaultSeqs {
		return 0, false
	}

	klog.Infof("[MaxNumSeqsEstimator] availPool(%.0f), groupSize(%d), pageBytes(%.0f), estimatedBlocks(%.0f), vLLMDefault(%d) => maxNumSeqs(%d) for workspace %s",
		availPool, groupSize, pageBytes, numBlocks, defaultSeqs, maxNumSeqs, req.WorkspaceName)

	return maxNumSeqs, true
}
