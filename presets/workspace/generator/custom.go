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

package generator

import (
	"encoding/json"
	"fmt"

	"github.com/kaito-project/kaito/pkg/model"
)

// GenerateFromConfig builds preset parameters for a bring-your-own model from
// its config.json plus the on-disk size of its weight bundle.
//
// Only the generator's remote front half is skipped. The file listing that
// normally supplies the weight footprint is replaced by sizeBytes, and
// everything downstream - architecture, dtype, parsers, context limit and vLLM
// run parameters - is derived by the same source-independent code that serves
// preset and HuggingFace models, so a custom model cannot drift from the shared
// behaviour.
//
// sizeBytes is the size of the bundle as it sits in storage, which is what the
// operator supplied and what can be checked against the real artifact. Deriving
// it from the declared dimensions instead would mean re-implementing every
// architecture's tensor layout, and getting that subtly wrong produces a
// plausible number rather than an error.
//
// modelName becomes the registry key and the served model identity. Note that
// the generator's model-name heuristics cannot match a content-addressed custom
// name, so defaults come from the configuration alone; where no reliable
// default exists the corresponding parser is simply left unset.
func GenerateFromConfig(modelName string, configJSON []byte, sizeBytes int64) (*model.PresetParam, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	if sizeBytes <= 0 {
		return nil, fmt.Errorf("model size must be a positive number of bytes, got %d", sizeBytes)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(sanitizeJSON(configJSON), &parsed); err != nil {
		return nil, fmt.Errorf("config.json is not valid JSON: %w", err)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("config.json is empty")
	}

	gen := &Generator{
		ModelRepo:     modelName,
		LoadFormat:    "auto",
		ConfigFormat:  "auto",
		TokenizerMode: "auto",
		ModelConfig:   parsed,
	}
	gen.Param.Metadata.Name = modelName
	gen.Param.VLLM.ModelRunParams = make(map[string]string)

	// Multimodal checkpoints nest the language-model dimensions.
	gen.mergeTextConfig()

	if err := validateSupportedConfig(gen.ModelConfig); err != nil {
		return nil, err
	}

	gen.Param.Metadata.ModelFileSize = bytesToGiB(sizeBytes)

	gen.ParseModelMetadata()
	if len(gen.Param.Metadata.Architectures) == 0 {
		return nil, fmt.Errorf("config.json does not declare an architecture")
	}
	gen.FinalizeParams()

	// Custom models are served from an operator-supplied bundle, never pulled
	// from HuggingFace, so no download credentials are involved.
	gen.Param.Metadata.DownloadAuthRequired = false

	return &gen.Param, nil
}

// validateSupportedConfig checks the model representation itself. Weight
// quantization is deliberately not restricted here: the runtime handles the
// common formats natively, KV-cache sizing is independent of weight dtype, and
// the weight footprint is supplied rather than derived - so there is nothing
// left for this feature to get wrong about a quantized checkpoint.
//
// A declared method is still required, because it is what selects the runtime's
// automatic dtype handling; an unnamed quantization would be loaded as though
// the weights were dense.
func validateSupportedConfig(config map[string]interface{}) error {
	qc, ok := config["quantization_config"].(map[string]interface{})
	if !ok {
		return nil
	}

	if getString(qc, []string{"quant_method", "quant_algo", "format"}) == "" {
		return fmt.Errorf("quantization_config does not declare a quantization method")
	}
	return nil
}

// bytesToGiB renders a byte count in the "<N>Gi" form the rest of the pipeline
// expects. The suffix is not cosmetic: calculateStorageSize parses this value by
// trimming "Gi" and discards the parse error, so any other form would silently
// size the model at zero.
//
// Rounding is upward because the two directions are not equally bad: a partial
// gibibyte dropped here under-sizes both the disk and the GPU estimate, while
// rounding up costs at most one gibibyte.
func bytesToGiB(b int64) string {
	const giB = 1 << 30
	return fmt.Sprintf("%dGi", (b+giB-1)/giB)
}
