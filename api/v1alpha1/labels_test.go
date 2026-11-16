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

	"github.com/kaito-project/kaito/pkg/model"
)

func TestGetWorkspaceRuntimeName(t *testing.T) {
	tests := []struct {
		name        string
		workspace   *Workspace
		annotation  string
		expected    model.RuntimeName
		shouldPanic bool
	}{
		{
			name:        "nil workspace should panic",
			workspace:   nil,
			shouldPanic: true,
		},
		{
			name:      "no annotation returns VLLM",
			workspace: &Workspace{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			expected:  model.RuntimeNameVLLM,
		},
		{
			name: "HuggingfaceTransformers annotation",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers),
					},
				},
			},
			expected: model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name: "VLLM annotation",
			workspace: &Workspace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
					},
				},
			},
			expected: model.RuntimeNameVLLM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		name         string
		inferenceSet *InferenceSet
		annotation   string
		expected     model.RuntimeName
		shouldPanic  bool
	}{
		{
			name:         "nil inference set should panic",
			inferenceSet: nil,
			shouldPanic:  true,
		},
		{
			name:         "no annotation returns VLLM",
			inferenceSet: &InferenceSet{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			expected:     model.RuntimeNameVLLM,
		},
		{
			name: "HuggingfaceTransformers annotation",
			inferenceSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers),
					},
				},
			},
			expected: model.RuntimeNameHuggingfaceTransformers,
		},
		{
			name: "VLLM annotation",
			inferenceSet: &InferenceSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
					},
				},
			},
			expected: model.RuntimeNameVLLM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func TestInferenceSetBenchmarkHelpers(t *testing.T) {
	is := &InferenceSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
	}
	// Verify both helpers are callable (prevents deadcode false positives
	// during v1alpha1 → v1beta1 transition).
	_ = IsRunBenchmarkEnabled(is)
	_ = ShouldRunBenchmark(is)
}
