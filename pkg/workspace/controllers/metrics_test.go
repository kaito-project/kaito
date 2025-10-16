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

package controllers

import (
	"reflect"
	"testing"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestCountPresetModels(t *testing.T) {
	tests := []struct {
		name     string
		ws       *kaitov1beta1.Workspace
		expected map[string]float64
	}{
		{
			name: "workspace with only inference preset model",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-3",
						},
					},
				},
			},
			expected: map[string]float64{
				"phi-3": 1,
			},
		},
		{
			name: "workspace with only tuning preset model",
			ws: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "llama-2-7b",
						},
					},
				},
			},
			expected: map[string]float64{
				"llama-2-7b": 1,
			},
		},
		{
			name: "workspace with both inference and tuning preset models (different models)",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-3",
						},
					},
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "llama-2-7b",
						},
					},
				},
			},
			expected: map[string]float64{
				"phi-3":      1,
				"llama-2-7b": 1,
			},
		},
		{
			name: "workspace with both inference and tuning using same model (should count once)",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-3",
						},
					},
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-3",
						},
					},
				},
			},
			expected: map[string]float64{
				"phi-3": 1,
			},
		},
		{
			name: "workspace with no inference or tuning configurations",
			ws:   &kaitov1beta1.Workspace{},
			expected: map[string]float64{},
		},
		{
			name: "workspace with nil inference and tuning",
			ws: &kaitov1beta1.Workspace{
				Inference: nil,
				Tuning:    nil,
			},
			expected: map[string]float64{},
		},
		{
			name: "workspace with inference but nil preset",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: nil,
				},
			},
			expected: map[string]float64{},
		},
		{
			name: "workspace with tuning but nil preset",
			ws: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: nil,
				},
			},
			expected: map[string]float64{},
		},
		{
			name: "workspace with inference preset but empty model name",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "",
						},
					},
				},
			},
			expected: map[string]float64{},
		},
		{
			name: "workspace with tuning preset but empty model name",
			ws: &kaitov1beta1.Workspace{
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "",
						},
					},
				},
			},
			expected: map[string]float64{},
		},
		{
			name: "workspace with mixed valid and invalid configurations",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "phi-3",
						},
					},
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "", // empty name should be ignored
						},
					},
				},
			},
			expected: map[string]float64{
				"phi-3": 1,
			},
		},
		{
			name: "workspace with complex model names",
			ws: &kaitov1beta1.Workspace{
				Inference: &kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "microsoft/DialoGPT-medium",
						},
					},
				},
				Tuning: &kaitov1beta1.TuningSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{
							Name: "meta-llama/Llama-2-7b-chat-hf",
						},
					},
				},
			},
			expected: map[string]float64{
				"microsoft/DialoGPT-medium":     1,
				"meta-llama/Llama-2-7b-chat-hf": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countPresetModels(tt.ws)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("countPresetModels() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCountPresetModels_NilWorkspace(t *testing.T) {
	// Test with nil workspace to ensure no panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("countPresetModels() panicked with nil workspace: %v", r)
		}
	}()

	result := countPresetModels(nil)
	expected := map[string]float64{}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("countPresetModels(nil) = %v, expected %v", result, expected)
	}
}

func TestCountPresetModels_EmptyMap(t *testing.T) {
	// Ensure that the function returns an empty map, not nil
	ws := &kaitov1beta1.Workspace{}
	result := countPresetModels(ws)
	if result == nil {
		t.Error("countPresetModels() returned nil, expected empty map")
	}
	if len(result) != 0 {
		t.Errorf("countPresetModels() returned map with %d items, expected 0", len(result))
	}
}
