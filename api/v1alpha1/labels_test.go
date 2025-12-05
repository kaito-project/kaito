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

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestGetWorkspaceRuntimeName(t *testing.T) {
	tests := []struct {
		name           string
		workspace      *Workspace
		featureEnabled bool
		annotation     string
		expected       model.RuntimeName
		shouldPanic    bool
	}{
		{
			name:        "nil workspace should panic",
			workspace:   nil,
			shouldPanic: true,
		},
		{
			name:           "feature gate disabled returns HuggingfaceTransformers",
			workspace:      &Workspace{},
			featureEnabled: false,
			expected:       model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name:           "feature gate enabled with no annotation returns VLLM",
			workspace:      &Workspace{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			featureEnabled: true,
			expected:       model.RuntimeNameVLLM,
		},
		{
			name: "feature gate enabled with HuggingfaceTransformers annotation",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers),
					},
				},
			},
			featureEnabled: true,
			expected:       model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name: "feature gate enabled with VLLM annotation",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
					},
				},
			},
			featureEnabled: true,
			expected:       model.RuntimeNameVLLM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set feature gate
			featuregates.FeatureGates = map[string]bool{
				consts.FeatureFlagVLLM: tt.featureEnabled,
			}

			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected panic but didn't get one")
					}
				}()
			}

			result := GetWorkspaceRuntimeName(tt.workspace)
			if !tt.shouldPanic && result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetInferenceSetRuntimeName(t *testing.T) {
	tests := []struct {
		name           string
		inferenceSet   *InferenceSet
		featureEnabled bool
		annotation     string
		expected       model.RuntimeName
		shouldPanic    bool
	}{
		{
			name:         "nil inference set should panic",
			inferenceSet: nil,
			shouldPanic:  true,
		},
		{
			name:           "feature gate disabled returns HuggingfaceTransformers",
			inferenceSet:   &InferenceSet{},
			featureEnabled: false,
			expected:       model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name:           "feature gate enabled with no annotation returns VLLM",
			inferenceSet:   &InferenceSet{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			featureEnabled: true,
			expected:       model.RuntimeNameVLLM,
		},
		{
			name: "feature gate enabled with HuggingfaceTransformers annotation",
			inferenceSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers),
					},
				},
			},
			featureEnabled: true,
			expected:       model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name: "feature gate enabled with VLLM annotation",
			inferenceSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
					},
				},
			},
			featureEnabled: true,
			expected:       model.RuntimeNameVLLM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set feature gate
			featuregates.FeatureGates = map[string]bool{
				consts.FeatureFlagVLLM: tt.featureEnabled,
			}

			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("Expected panic but didn't get one")
					}
				}()
			}

			result := GetInferenceSetRuntimeName(tt.inferenceSet)
			if !tt.shouldPanic && result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
