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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/featuregates"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

func customInferenceSpec() *InferenceSpec {
	return &InferenceSpec{
		Preset: &PresetSpec{
			PresetMeta: PresetMeta{Name: ModelName(plugin.PresetNameCustom)},
		},
		Config: "byo-config",
	}
}

func TestValidateCustomPreset(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(i *InferenceSpec)
		errSubstr string
	}{
		{
			name:   "valid custom preset",
			mutate: func(i *InferenceSpec) {},
		},
		{
			name:      "missing config",
			mutate:    func(i *InferenceSpec) { i.Config = "" },
			errSubstr: "config",
		},
		{
			// Weights come from an operator-supplied bundle; a HuggingFace
			// credential here means the user expects a download that never happens.
			name:      "model access secret is not applicable",
			mutate:    func(i *InferenceSpec) { i.Preset.PresetOptions.ModelAccessSecret = "hf-token" },
			errSubstr: "modelAccessSecret",
		},
		{
			name: "image override is not applicable",
			mutate: func(i *InferenceSpec) {
				//nolint:staticcheck //SA1019: exercising the deprecated field is the point
				i.Preset.PresetOptions.Image = "example.com/img:v1"
			},
			errSubstr: "image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := customInferenceSpec()
			tt.mutate(spec)

			err := spec.validateCustomPreset()
			if tt.errSubstr == "" {
				assert.Nil(t, err)
				return
			}
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestValidateCustomPresetIgnoresOtherPresets(t *testing.T) {
	// A normal preset supplies its own weights and must not be subjected to
	// bring-your-own rules.
	spec := &InferenceSpec{
		Preset: &PresetSpec{
			PresetMeta:    PresetMeta{Name: ModelName("phi-4")},
			PresetOptions: PresetOptions{ModelAccessSecret: "hf-token"},
		},
	}
	assert.Nil(t, spec.validateCustomPreset())
}

func TestValidateCustomModelStreaming(t *testing.T) {
	original := featuregates.FeatureGates[consts.FeatureFlagModelStreaming]
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagModelStreaming] = original })

	streamingAnnotations := map[string]string{
		mmconsts.AnnotationModelStreaming: "true",
		consts.AnnotationStreamSourceType: consts.SourceTypeBYO,
	}

	tests := []struct {
		name         string
		gateEnabled  bool
		annotations  map[string]string
		errSubstr    string
		presetName   string
		expectNoRule bool
	}{
		{
			name:        "streaming configured",
			gateEnabled: true,
			annotations: streamingAnnotations,
		},
		{
			// Without streaming there is no path to the weights at all, and a
			// custom model has no HuggingFace identity to fall back on.
			name:        "feature gate disabled",
			gateEnabled: false,
			annotations: streamingAnnotations,
			errSubstr:   consts.FeatureFlagModelStreaming,
		},
		{
			name:        "streaming annotation missing",
			gateEnabled: true,
			annotations: map[string]string{consts.AnnotationStreamSourceType: consts.SourceTypeBYO},
			errSubstr:   mmconsts.AnnotationModelStreaming,
		},
		{
			name:        "wrong source type",
			gateEnabled: true,
			annotations: map[string]string{
				mmconsts.AnnotationModelStreaming: "true",
				consts.AnnotationStreamSourceType: consts.SourceTypePublic,
			},
			errSubstr: consts.AnnotationStreamSourceType,
		},
		{
			name:         "non-custom preset is unaffected",
			gateEnabled:  false,
			annotations:  nil,
			presetName:   "phi-4",
			expectNoRule: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregates.FeatureGates[consts.FeatureFlagModelStreaming] = tt.gateEnabled

			presetName := plugin.PresetNameCustom
			if tt.presetName != "" {
				presetName = tt.presetName
			}
			ws := &Workspace{
				ObjectMeta: metav1.ObjectMeta{Name: "ws", Namespace: "default", Annotations: tt.annotations},
				Inference: &InferenceSpec{
					Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName(presetName)}},
					Config: "byo-config",
				},
			}

			err := ws.validateCustomModelStreaming()
			if tt.errSubstr == "" || tt.expectNoRule {
				assert.Nil(t, err)
				return
			}
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestInferenceSpecUpdateRejectsCustomConfigChange(t *testing.T) {
	// The ConfigMap *is* the model. Repointing it would swap the weights
	// underneath a deployment already sized from the original configuration.
	old := customInferenceSpec()
	updated := customInferenceSpec()
	updated.Config = "byo-config-v2"

	err := updated.validateUpdate(old)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "config")

	assert.Nil(t, customInferenceSpec().validateUpdate(old), "an unchanged custom spec must be accepted")
}

func TestInferenceSpecUpdateAllowsConfigChangeForPresets(t *testing.T) {
	// A normal preset's ConfigMap carries runtime settings only; its model
	// identity comes from the preset name, so this rule must not apply.
	old := &InferenceSpec{
		Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("phi-4")}},
		Config: "tuned-settings",
	}
	updated := &InferenceSpec{
		Preset: &PresetSpec{PresetMeta: PresetMeta{Name: ModelName("phi-4")}},
		Config: "other-settings",
	}

	assert.Nil(t, updated.validateUpdate(old))
}
