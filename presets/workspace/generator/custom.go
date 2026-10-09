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
	"regexp"
	"strings"

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
// modelName becomes the registry key and the served model identity. The
// generator's model-name heuristics cannot match a content-addressed custom
// name, so by default family settings come from the configuration alone; where
// no reliable default exists the corresponding setting is simply left unset.
// WithReferenceModelID restores them by naming a model the custom model
// shares a family with.
func GenerateFromConfig(modelName string, configJSON []byte, sizeBytes int64, opts ...CustomOption) (*model.PresetParam, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}
	options := ApplyCustomOptions(opts...)
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
	gen.nameHint = options.ReferenceModelID.FamilyName()
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

// ReferenceModelID is a validated HuggingFace model ID ("org/name") whose
// model-family settings a custom model shares. The zero value means none.
type ReferenceModelID string

// referenceModelIDPattern checks only the "org/name" form, using the
// characters HuggingFace permits in repository IDs.
var referenceModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// ParseReferenceModelID trims raw and checks that it is empty or has the
// "org/name" form. An ID naming no known model family is valid and simply
// contributes no settings.
func ParseReferenceModelID(raw string) (ReferenceModelID, error) {
	id := strings.TrimSpace(raw)
	if id == "" || referenceModelIDPattern.MatchString(id) {
		return ReferenceModelID(id), nil
	}
	return "", fmt.Errorf("reference model ID %q must be a HuggingFace model ID of the form \"org/name\"", raw)
}

// FamilyName returns the key the name-keyed runtime tables are looked up by,
// or "" for the zero value.
func (id ReferenceModelID) FamilyName() string {
	if id == "" {
		return ""
	}
	return ModelNameFromRepo(string(id))
}

// CustomOptions holds the optional inputs for generating a custom model.
type CustomOptions struct {
	ReferenceModelID ReferenceModelID
}

// CustomOption configures custom model generation.
type CustomOption func(*CustomOptions)

// WithReferenceModelID sets the reference model whose family settings a custom
// model inherits.
func WithReferenceModelID(id ReferenceModelID) CustomOption {
	return func(o *CustomOptions) {
		o.ReferenceModelID = id
	}
}

// ApplyCustomOptions folds opts into a CustomOptions value.
func ApplyCustomOptions(opts ...CustomOption) CustomOptions {
	var o CustomOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
