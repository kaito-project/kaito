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
	"strconv"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

// Shared per-GPU memory budget parameters. Both the node estimator (which solves
// for how many GPUs the weights need) and the max-num-seqs estimator (which solves
// for the KV-cache pool left over on a GPU) model the same vLLM memory layout, so
// they must agree on these values.
const (
	// WeightExpansionFactor accounts for the ~2% expansion of model weights once
	// loaded by vLLM relative to the on-disk safetensor size.
	WeightExpansionFactor = 1.02

	// BaseOverheadGiB is the model-independent part of vLLM's fixed per-GPU
	// overhead: non-torch allocations such as the CUDA context and NCCL buffers
	// (~0.6 GiB) plus a baseline for small-model activations and CUDA graphs
	// (~1.7 GiB). Larger models add to this via OverheadWeightFactor below.
	// Overridden per GPU model in baseOverheadGiBByGPUModel.
	BaseOverheadGiB = 2.3

	// OverheadWeightFactor scales the runtime overhead with the per-GPU model
	// weight share. Peak activation memory and CUDA graph capture both grow with
	// hidden size / layer count and are sharded across TP ranks the same way
	// weights are, so the per-GPU weight share is a good proxy for them. vLLM
	// measures these empirically in determine_available_memory() and
	// profile_cudagraph_memory(). We approximate at best effort here.
	OverheadWeightFactor = 0.05

	// defaultGPUMemoryUtilization is the fallback used when the launcher's
	// --gpu-memory-utilization value cannot be parsed.
	defaultGPUMemoryUtilization = 0.92
)

// baseOverheadGiBByGPUModel overrides BaseOverheadGiB for specific GPU models.
// The 24 GiB A10 measures less fixed runtime overhead in practice than the
// default reserve assumes, so a lower value lets ~16-17 GiB models fit a single
// A10 (empirically verified, e.g. granite-4.1-8b) instead of being pushed to an
// extra node. Keyed by sku.GPUConfig.GPUModel (e.g. "NVIDIA A10").
var baseOverheadGiBByGPUModel = map[string]float64{
	"NVIDIA A10": 1.5,
}

// ResolveGPUMemoryUtilization returns the --gpu-memory-utilization the launcher
// runs vLLM with for the given GPU model (see ResolveGPUMemoryUtilization in
// pkg/model), so estimators predict the same per-GPU budget vLLM will have.
func ResolveGPUMemoryUtilization(gpuModel string) float64 {
	v, err := strconv.ParseFloat(pkgmodel.ResolveGPUMemoryUtilization(gpuModel), 64)
	if err != nil {
		return defaultGPUMemoryUtilization
	}
	return v
}

// ResolveBaseOverheadGiB returns the fixed per-GPU overhead reserve for the GPU model.
func ResolveBaseOverheadGiB(gpuModel string) float64 {
	if v, ok := baseOverheadGiBByGPUModel[gpuModel]; ok {
		return v
	}
	return BaseOverheadGiB
}
