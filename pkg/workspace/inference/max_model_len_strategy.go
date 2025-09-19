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

package inference

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

// This file contains ONLY pseudocode / scaffolding for a future dynamic max-model-len
// computation. It is intentionally NOT wired into any runtime path yet.
//
// Goal:
//   When user does NOT provide a custom inference config ConfigMap, we want to
//   be able to derive a safer / larger max-model-len instead of using the current
//   implicit default (2048) while respecting the model's hard upper bound
//   (PresetParam.ModelTokenLimit) and staying within GPU memory capacity.
//
// Inputs Needed (available in existing code paths):
//   - *model.PresetParam (from ctx.Model.GetInferenceParameters())
//       * ModelTokenLimit (hard cap)
//       * BytesPerToken (empirical scaling constant for KV cache / activations)
//   - *sku.GPUConfig (from GenerateInferencePodSpec) giving:
//       * GPUMemGB (memory per GPU)
//       * GPUCount (number of GPUs per Pod)
//   - (Optional future) Inference runtime context for distributed / pipeline parallel.
//
// High-Level Formula (pseudocode):
//   mem_bytes_per_gpu = GPUMemGB * 2^30
//   usable = mem_bytes_per_gpu * UTILIZATION_FACTOR - STATIC_OVERHEAD
//   effective_bpt = BytesPerToken / max(1, TP_SIZE)
//   candidate = floor( usable / effective_bpt )
//   candidate_aligned = floor(candidate / ALIGN_STEP) * ALIGN_STEP
//   candidate_clamped = clamp(candidate_aligned, MIN_FLOOR, ModelTokenLimit)
//   return candidate_clamped
//
// Constants (tunable):
//   UTILIZATION_FACTOR ~ 0.85 - 0.9   (leave headroom)
//   STATIC_OVERHEAD = 1 GiB           (process + fragmentation)
//   ALIGN_STEP = 256 tokens
//   MIN_FLOOR = 2048
//
// Edge Cases:
//   - BytesPerToken <= 0: fallback = clamp(ModelTokenLimit/4, MIN_FLOOR, ModelTokenLimit)
//   - Result > ModelTokenLimit: clamp
//   - Multi-GPU (tensor parallel): assume linear reduction in per-token cost:
//       effective_bpt = BytesPerToken / GPUCount
//   - If future pipeline parallel or distributed inference present, can refine.
//
// Integration Point (recommended):
//   Option A (before Pod command build): inside GenerateInferencePodSpec AFTER
//   EnsureConfigOrCopyFromDefault returns the ConfigMap and BEFORE building the
//   inference command.
//     1. Detect user did not supply ctx.Workspace.Inference.Config (empty string)
//     2. If target runtime == vLLM
//     3. Compute value via this function
//     4. If the ConfigMap's inference_config.yaml lacks max-model-len, inject it.
//   Option B (Alternative): modify buildVLLMInferenceCommand to inject param directly
//   (less transparent—ConfigMap wouldn't show final value). Option A preferred.
//
// NOTE: This stub currently returns 0 and is not referenced anywhere. When you provide
// the final algorithm parameters, replace pseudocode with the concrete logic and call it.
//
// Example future usage sketch (NOT implemented):
//   val := computePlannedMaxModelLen(presetParams, gpuConfig)
//   if val > 0 { inject into YAML }
//
// ProvideAlgorithmInputs will be the hook you can fill once you finalize constants.

// computePlannedMaxModelLen is a placeholder that will later hold the real algorithm.
// Returning 0 signals "no dynamic suggestion" so callers can skip mutation.
// computePlannedMaxModelLen implements the provided formula:
//
//	max_model_len = (GPUMemGB * 0.82 - TotalSafeTensorFileSize*1.02 / nodeCountPerReplica / GPUCount - 2.3) * 2^30 /
//	                (BytesPerToken / nodeCountPerReplica / GPUCount)
//
// Notes:
// - TotalSafeTensorFileSize stored as string like "25.63Gi"; we parse Gi/Mi/Ti.
// - nodeCountPerReplica: number of nodes participating in one logical replica (>=1). If unknown use 1.
// - Returns 0 on any invalid input (caller interprets as "skip dynamic").
// - Final value clamped to [2048, ModelTokenLimit] and aligned down to 256.
func computePlannedMaxModelLen(preset *pkgmodel.PresetParam, gpu *sku.GPUConfig, nodeCountPerReplica int) int {
	if preset == nil || gpu == nil || nodeCountPerReplica <= 0 {
		return 0
	}
	if preset.ModelTokenLimit <= 0 || preset.BytesPerToken <= 0 || gpu.GPUMemGB <= 0 || gpu.GPUCount <= 0 {
		return 0
	}

	// Parse weight size into GiB directly (float64)
	weightGiB, ok := ParseSizeToGiB(preset.TotalSafeTensorFileSize)
	if !ok || weightGiB <= 0 {
		return 0
	}

	gpuMemGB := float64(gpu.GPUMemGB) // already GiB
	gpuCount := float64(gpu.GPUCount)
	nodes := float64(nodeCountPerReplica)
	bytesPerToken := float64(preset.BytesPerToken)

	// Check if this is a Falcon model
	isFalconModel := strings.Contains(strings.ToLower(preset.Name), "falcon")

	// Formula (ALL INSIDE PARENTHESES IN GiB):
	//   innerGiB = gpuMemGB*0.82 - (weightGiB*1.02)/(nodes*gpuCount) - 2.3
	term1 := (gpuMemGB * 0.82) / gpuCount
	var term2 float64
	if isFalconModel {
		// Falcon models: don't divide by nodes and gpuCount
		term2 = weightGiB * 1.02
	} else {
		// Other models: divide by nodes and gpuCount
		term2 = (weightGiB * 1.02) / (nodes * gpuCount)
	}
	term3 := 2.3
	innerGiB := term1 - term2 - term3

	if innerGiB <= 0 {
		return 0
	}

	// Convert GiB -> bytes only once after parentheses
	numeratorBytes := innerGiB * (1 << 30)
	var denom float64
	if isFalconModel {
		// Falcon models: don't divide by nodes and gpuCount
		denom = bytesPerToken
	} else {
		// Other models: divide by nodes and gpuCount
		denom = bytesPerToken / (nodes * gpuCount)
	}

	if denom <= 0 {
		return 0
	}
	candidate := int(numeratorBytes / denom)

	if candidate < 0 {
		return 0
	}

	originalCandidate := candidate
	if preset.ModelTokenLimit > 0 && candidate > preset.ModelTokenLimit {
		candidate = preset.ModelTokenLimit
		klog.Infof("computePlannedMaxModelLen: clamped to ModelTokenLimit: %d -> %d", originalCandidate, candidate)
	}

	candidate = (candidate / 256) * 256

	klog.Infof("computePlannedMaxModelLen: final result=%d", candidate)

	return candidate
}

// parseSizeToBytes parses size strings with Gi, Mi, Ti units (binary powers of two).
// parseSizeToGiB converts Ti/Gi/Mi suffix size string to GiB (float64). Kept unexported until integrated.
func ParseSizeToGiB(v string) (float64, bool) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, false
	}
	re := regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)(ti|gi|mi)$`)
	m := re.FindStringSubmatch(strings.ToLower(s))
	if len(m) != 3 {
		return 0, false
	}
	numStr, unit := m[1], m[2]
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false
	}
	var multiplier float64
	switch unit {
	case "ti":
		multiplier = 1024 // TiB -> GiB
	case "gi":
		multiplier = 1
	case "mi":
		multiplier = 1.0 / 1024
	default:
		return 0, false
	}
	valGiB := f * multiplier
	if valGiB <= 0 || valGiB*(1<<30) > float64(math.MaxInt64) {
		return 0, false
	}
	return valGiB, true
}
