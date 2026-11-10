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

package speculativedecoding

import (
	"testing"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestValidateOptIn(t *testing.T) {
	annotationKey := "kaito.sh/enable-speculative-decoding"

	tests := []struct {
		name             string
		annotations      map[string]string
		presetName       string
		runtime          model.RuntimeName
		wantStatus       OptInStatus
		wantInvalidValue string
	}{
		{
			name:        "annotation absent",
			annotations: nil,
			wantStatus:  OptInDisabled,
		},
		{
			name:        "annotation false",
			annotations: map[string]string{annotationKey: "false"},
			wantStatus:  OptInDisabled,
		},
		{
			name:             "annotation invalid",
			annotations:      map[string]string{annotationKey: "yes"},
			wantStatus:       OptInInvalidValue,
			wantInvalidValue: "yes",
		},
		{
			name:        "missing preset",
			annotations: map[string]string{annotationKey: "true"},
			runtime:     model.RuntimeNameVLLM,
			wantStatus:  OptInMissingPreset,
		},
		{
			name:        "runtime mismatch",
			annotations: map[string]string{annotationKey: "true"},
			presetName:  "deepseek-r1-0528",
			runtime:     model.RuntimeNameHuggingfaceTransformers,
			wantStatus:  OptInRuntimeMismatch,
		},
		{
			name:        "valid opt-in",
			annotations: map[string]string{annotationKey: "true"},
			presetName:  "deepseek-r1-0528",
			runtime:     model.RuntimeNameVLLM,
			wantStatus:  OptInEnabled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, invalidValue := ValidateOptIn(tc.annotations, annotationKey, tc.presetName, tc.runtime)
			if status != tc.wantStatus {
				t.Fatalf("ValidateOptIn() status=%v want=%v", status, tc.wantStatus)
			}
			if invalidValue != tc.wantInvalidValue {
				t.Fatalf("ValidateOptIn() invalidValue=%q want=%q", invalidValue, tc.wantInvalidValue)
			}
		})
	}
}
