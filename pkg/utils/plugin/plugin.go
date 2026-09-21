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

package plugin

import (
	"strings"
	"sync"

	"github.com/kaito-project/kaito/pkg/model"
)

// Registration is a struct that holds the name and an instance of a struct
// that implements the model.Model interface. It is used to register and manage
// different model instances within the KAITO framework.
type Registration struct {
	// Name is the name of the model. It is used as a key to register and
	// retrieve the model metadata and instance.
	Name string

	// Instance is the actual model instance that implements the model.Model
	// interface. It is used to retrieve the model's compute/storage requirements
	// and runtime parameters.
	Instance model.Model
}

type ModelRegister struct {
	sync.RWMutex
	models map[string]*Registration
}

var KaitoModelRegister ModelRegister

const (
	// PresetNameCustom is the reserved preset name selecting a bring-your-own
	// model whose configuration is supplied through the inference ConfigMap.
	PresetNameCustom = "custom"

	// CustomModelNamePrefix prefixes the internal, content-addressed registry
	// key derived from a custom model's configuration. Users select
	// PresetNameCustom; the suffixed form is produced only by resolution.
	CustomModelNamePrefix = "custom-"
)

// IsCustomPreset reports whether a preset name selects bring-your-own weights.
func IsCustomPreset(presetName string) bool {
	return strings.EqualFold(presetName, PresetNameCustom)
}

// IsReservedCustomModelName reports whether a name occupies the internal
// content-addressed namespace. Such names are produced by resolution and must
// never be accepted as user input: the registry is process-global and not
// namespace-scoped, so honouring one would let a caller address a registration
// populated from a ConfigMap they cannot read.
func IsReservedCustomModelName(presetName string) bool {
	return strings.HasPrefix(strings.ToLower(presetName), CustomModelNamePrefix)
}

// LegacyBuiltinToCatalog maps legacy short preset names to full HuggingFace
// model IDs. Deprecated: use full HuggingFace model IDs in new manifests.
var LegacyBuiltinToCatalog = map[string]string{
	"phi-4":                         "microsoft/phi-4",
	"phi-4-mini-instruct":           "microsoft/phi-4-mini-instruct",
	"llama-3.1-8b-instruct":         "meta-llama/llama-3.1-8b-instruct",
	"llama-3.3-70b-instruct":        "meta-llama/llama-3.3-70b-instruct",
	"deepseek-r1-distill-llama-8b":  "deepseek-ai/deepseek-r1-distill-llama-8b",
	"deepseek-r1-distill-qwen-14b":  "deepseek-ai/deepseek-r1-distill-qwen-14b",
	"deepseek-r1-0528":              "deepseek-ai/deepseek-r1-0528",
	"deepseek-v3-0324":              "deepseek-ai/deepseek-v3-0324",
	"phi-3-mini-4k-instruct":        "microsoft/phi-3-mini-4k-instruct",
	"phi-3-mini-128k-instruct":      "microsoft/phi-3-mini-128k-instruct",
	"phi-3-medium-4k-instruct":      "microsoft/phi-3-medium-4k-instruct",
	"phi-3-medium-128k-instruct":    "microsoft/phi-3-medium-128k-instruct",
	"phi-3.5-mini-instruct":         "microsoft/phi-3.5-mini-instruct",
	"qwen2.5-coder-7b-instruct":     "qwen/qwen2.5-coder-7b-instruct",
	"qwen2.5-coder-32b-instruct":    "qwen/qwen2.5-coder-32b-instruct",
	"gpt-oss-20b":                   "openai/gpt-oss-20b",
	"gpt-oss-120b":                  "openai/gpt-oss-120b",
	"gemma-3-4b-instruct":           "google/gemma-3-4b-it",
	"gemma-3-27b-instruct":          "google/gemma-3-27b-it",
	"mistral-7b":                    "mistralai/mistral-7b-v0.3",
	"mistral-7b-instruct":           "mistralai/mistral-7b-instruct-v0.3",
	"ministral-3-3b-instruct":       "mistralai/ministral-3-3b-instruct-2512",
	"ministral-3-8b-instruct":       "mistralai/ministral-3-8b-instruct-2512",
	"ministral-3-14b-instruct":      "mistralai/ministral-3-14b-instruct-2512",
	"mistral-large-3-675b-instruct": "mistralai/mistral-large-3-675b-instruct-2512",
}

// Register allows model to be added
func (reg *ModelRegister) Register(r *Registration) {
	reg.Lock()
	defer reg.Unlock()
	if r.Name == "" {
		panic("model name is not specified")
	}

	if reg.models == nil {
		reg.models = make(map[string]*Registration)
	}

	reg.models[r.Name] = r
}

func (reg *ModelRegister) MustGet(name string) model.Model {
	reg.Lock()
	defer reg.Unlock()
	r, ok := reg.models[name]
	if !ok {
		return nil
	}
	return r.Instance
}

func (reg *ModelRegister) Has(name string) bool {
	reg.Lock()
	defer reg.Unlock()
	_, ok := reg.models[name]
	return ok
}

// IsValidPreset returns true if:
// 1. the given preset name is the reserved "custom" name for bring-your-own weights.
// 2. the given preset name is registered in the KaitoModelRegister.
// 3. the given preset name is a legacy builtin preset alias.
// 4. the given preset name is a valid huggingface model card ID, e.g. "Qwen/Qwen2.5-Coder-7B-Instruct"
//
// Names in the reserved "custom-" namespace are never valid input: they are
// internal, content-addressed registry keys produced by resolving a custom
// model, and the registry is process-global rather than namespace-scoped.
func IsValidPreset(preset string) bool {
	if IsCustomPreset(preset) {
		return true
	}
	if IsReservedCustomModelName(preset) {
		return false
	}
	if KaitoModelRegister.Has(preset) {
		return true
	}
	if _, ok := LegacyBuiltinToCatalog[strings.ToLower(preset)]; ok {
		return true
	}
	// if preset is like "a/b", consider it as a valid HF model ID
	if strings.Contains(preset, "/") {
		parts := strings.SplitN(preset, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return true
		}
	}
	return false
}

// ResolveHFModelID resolves a legacy preset alias to its HuggingFace model ID.
func ResolveHFModelID(presetName string) string {
	if hfName, ok := LegacyBuiltinToCatalog[strings.ToLower(presetName)]; ok {
		return hfName
	}
	return presetName
}
