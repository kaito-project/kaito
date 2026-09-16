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

package v1beta1

import (
	"fmt"

	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/featuregates"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// validateCustomPreset checks the inputs that only apply to a bring-your-own
// model. The model configuration itself is validated when the preset is
// resolved; these are the surrounding constraints that resolution cannot see.
func (i *InferenceSpec) validateCustomPreset() *apis.FieldError {
	if i.Preset == nil || !plugin.IsCustomPreset(string(i.Preset.Name)) {
		return nil
	}

	if i.Config == "" {
		return apis.ErrMissingField(fmt.Sprintf(
			"'config' is required by preset %q and must reference an immutable ConfigMap containing %q and %q",
			plugin.PresetNameCustom, models.CustomModelConfigKey, models.CustomModelSizeKey))
	}

	// Weights come from an operator-supplied bundle, never from HuggingFace, so
	// a download credential here signals a misunderstanding of the model source.
	if i.Preset.PresetOptions.ModelAccessSecret != "" {
		return apis.ErrGeneric(fmt.Sprintf(
			"'modelAccessSecret' does not apply to preset %q: weights are served from the configured model source, not downloaded from HuggingFace",
			plugin.PresetNameCustom), "preset.presetOptions.modelAccessSecret")
	}

	//nolint:staticcheck //SA1019: the deprecated image override cannot describe a custom model
	if i.Preset.PresetOptions.Image != "" {
		return apis.ErrGeneric(fmt.Sprintf(
			"'image' does not apply to preset %q", plugin.PresetNameCustom), "preset.presetOptions.image")
	}

	return nil
}

// validateCustomModelStreaming verifies that the streaming prerequisites a
// custom model depends on are actually in place. Without them the deployment
// has no way to obtain weights, and there is deliberately no HuggingFace
// fallback for a model that does not exist there.
func (w *Workspace) validateCustomModelStreaming() *apis.FieldError {
	if w.Inference == nil || w.Inference.Preset == nil || !plugin.IsCustomPreset(string(w.Inference.Preset.Name)) {
		return nil
	}

	if !featuregates.FeatureGates[consts.FeatureFlagModelStreaming] {
		return apis.ErrGeneric(fmt.Sprintf(
			"preset %q requires the %s feature gate to be enabled",
			plugin.PresetNameCustom, consts.FeatureFlagModelStreaming), "inference.preset.name")
	}

	annotations := w.GetAnnotations()
	if annotations[mmconsts.AnnotationModelStreaming] != "true" {
		return apis.ErrMissingField(fmt.Sprintf(
			"preset %q requires annotation %s=\"true\"",
			plugin.PresetNameCustom, mmconsts.AnnotationModelStreaming))
	}
	// The source type itself is validated by the streaming configuration, but a
	// custom model has no HuggingFace identity to fall back on, so the
	// operator-owned source flavour is required rather than merely expected.
	if annotations[consts.AnnotationStreamSourceType] != consts.SourceTypeBYO {
		return apis.ErrInvalidValue(fmt.Sprintf(
			"preset %q requires annotation %s=%q: the model bundle must come from a versioned, operator-owned source",
			plugin.PresetNameCustom, consts.AnnotationStreamSourceType, consts.SourceTypeBYO), consts.AnnotationStreamSourceType)
	}

	return nil
}
