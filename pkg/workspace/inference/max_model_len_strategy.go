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
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

// computeMaxModelLen calculates the optimal max model length for GPU memory efficiency.
func computeMaxModelLen(preset *pkgmodel.PresetParam, gpu *sku.GPUConfig, numRequiredNodes int) int {
	// Validate input parameters
	if preset == nil || gpu == nil || numRequiredNodes <= 0 {
		return 0
	}
	if preset.ModelTokenLimit <= 0 || preset.BytesPerToken <= 0 || gpu.GPUMemGiB <= 0 || gpu.GPUCount <= 0 {
		return 0
	}

	// Parse model weight size using Kubernetes resource.Quantity
	weightGiB, ok := parseModelWeight(preset.TotalSafeTensorFileSize)
	if !ok {
		return 0
	}

	// Calculate available GPU memory and adjusted bytes per token
	availableBytes, adjustedBytesPerToken := calculateMemoryParameters(preset, gpu, numRequiredNodes, weightGiB)
	if availableBytes <= 0 || adjustedBytesPerToken <= 0 {
		return 0
	}

	// Calculate raw token candidate
	candidate := int(availableBytes / (adjustedBytesPerToken))
	if candidate <= 0 {
		return 0
	}

	// Apply constraints and finalize result
	finalResult := applyConstraintsAndAlignment(candidate, preset.ModelTokenLimit)

	klog.Infof("computeMaxModelLen: final result=%d", finalResult)
	return finalResult
}

// parseModelWeight parses the model weight string and returns the weight in GiB.
// It handles Kubernetes resource.Quantity format strings like "25.63Gi", etc.
func parseModelWeight(totalSafeTensorFileSize string) (float64, bool) {
	s := strings.TrimSpace(totalSafeTensorFileSize)
	if s == "" {
		return 0, false
	}

	// Use Kubernetes resource.Quantity to parse the size string
	quantity, err := resource.ParseQuantity(s)
	if err != nil {
		return 0, false
	}

	bytes := quantity.Value()
	if bytes <= 0 {
		return 0, false
	}

	// Convert bytes to GiB (1 GiB = 2^30 bytes)
	weightGiB := float64(bytes) / (1 << 30)
	if weightGiB <= 0 {
		return 0, false
	}

	return weightGiB, true
}

// calculateMemoryParameters computes available GPU memory and adjusted bytes per token.
// Returns the available memory in bytes and the adjusted bytes per token for the calculation.
func calculateMemoryParameters(preset *pkgmodel.PresetParam, gpu *sku.GPUConfig, numRequiredNodes int, weightGiB float64) (float64, float64) {
	gpuMemGB := float64(gpu.GPUMemGiB)
	gpuCount := float64(gpu.GPUCount)
	nodes := float64(numRequiredNodes)
	bytesPerToken := float64(preset.BytesPerToken)

	// Calculate available GPU memory using the formula:
	// availableMemoryGiB = (gpuMemGB * 0.84) / gpuCount - (weightGiB * 1.02) / (nodes * gpuCount) - 2.3

	// Available GPU memory per GPU with 84% utilization factor
	usableMemoryPerGPU := (gpuMemGB * 0.84) / gpuCount

	// Model weight overhead per GPU with 2% safety margin
	var modelWeightOverhead float64
	if preset.DisableTensorParallelism {
		// Falcon models: don't divide by nodes and gpuCount
		modelWeightOverhead = weightGiB * 1.02
	} else {
		// Other models: distribute weight across nodes and GPUs
		modelWeightOverhead = (weightGiB * 1.02) / (nodes * gpuCount)
	}

	// Static overhead for activations and non-torch components
	// Sum of max activations (1.7 GiB) and max non-torch overhead (0.6 GiB)
	staticOverhead := 2.3

	availableMemoryGiB := usableMemoryPerGPU - modelWeightOverhead - staticOverhead
	availableMemoryBytes := availableMemoryGiB * (1 << 30)

	// Calculate adjusted bytes per token for distribution
	var adjustedBytesPerToken float64
	if preset.DisableTensorParallelism {
		// Falcon models: no distribution adjustment
		adjustedBytesPerToken = bytesPerToken
	} else {
		// Other models: distribute across GPUs
		adjustedBytesPerToken = bytesPerToken / gpuCount * float64(numRequiredNodes)
	}

	return availableMemoryBytes, adjustedBytesPerToken
}

// applyConstraintsAndAlignment applies token limit constraints and 256-token boundary alignment.
// This ensures the result stays within model limits and is optimally aligned for efficiency.
func applyConstraintsAndAlignment(candidate, modelTokenLimit int) int {
	// Clamp to model's token limit if necessary
	originalCandidate := candidate
	if modelTokenLimit > 0 && candidate > modelTokenLimit {
		candidate = modelTokenLimit
		klog.Infof("computeMaxModelLen: clamped to ModelTokenLimit: %d -> %d", originalCandidate, candidate)
	}

	// Align down to 256-token boundary for efficiency
	// This helps with memory allocation patterns and performance optimization
	candidate = (candidate / 256) * 256

	return candidate
}
