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

package nvidia

import (
	"strings"
	"unicode"
)

// Canonical GPU model names, as used by SKU tables and hardware policy maps.
const (
	GPUModelA10  = "NVIDIA A10"
	GPUModelA100 = "NVIDIA A100"
	GPUModelH100 = "NVIDIA H100"
)

var (
	// gpuMemoryUtilizationByGPUModel reserves additional vLLM runtime headroom
	// on tight-VRAM GPUs.
	gpuMemoryUtilizationByGPUModel = map[string]string{
		GPUModelA10: "0.82",
	}

	// baseOverheadGiBByGPUModel overrides the default fixed per-GPU vLLM
	// overhead reserve. The 24 GiB A10 measures less fixed runtime overhead in
	// practice than the default assumes, so a lower value lets ~16-17 GiB
	// models fit a single A10 instead of being pushed to an extra node.
	baseOverheadGiBByGPUModel = map[string]float64{
		GPUModelA10: 1.5,
	}
)

// CanonicalGPUModel maps GPU Feature Discovery product labels to the canonical
// GPU model names above. Unrecognized models are returned unchanged.
func CanonicalGPUModel(gpuModel string) string {
	for token := range strings.FieldsFuncSeq(gpuModel, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		switch strings.ToUpper(token) {
		case "H100":
			return GPUModelH100
		case "A100":
			return GPUModelA100
		case "A10":
			return GPUModelA10
		}
	}
	return gpuModel
}

// GPUMemoryUtilization returns the vLLM gpu-memory-utilization for a canonical
// GPU model, if the model needs a non-default value.
func GPUMemoryUtilization(gpuModel string) (string, bool) {
	v, ok := gpuMemoryUtilizationByGPUModel[gpuModel]
	return v, ok
}

// BaseOverheadGiB returns the fixed per-GPU vLLM overhead reserve for a GPU
// model, if the model needs a non-default value.
func BaseOverheadGiB(gpuModel string) (float64, bool) {
	v, ok := baseOverheadGiBByGPUModel[gpuModel]
	return v, ok
}
