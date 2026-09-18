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
	"fmt"
	"strings"
	"testing"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestValidateOptIn(t *testing.T) {
	annotationKey := "kaito.sh/enable-speculative-decoding"
	annotationPath := fmt.Sprintf("metadata.annotations[%s]", annotationKey)

	newOptions := func() ValidationOptions {
		return ValidationOptions{
			AnnotationKey:          annotationKey,
			AnnotationPath:         annotationPath,
			PresetName:             "deepseek-r1-0528",
			MissingPresetMessage:   "missing preset",
			Runtime:                model.RuntimeNameVLLM,
			RuntimeMismatchMessage: "wrong runtime",
		}
	}

	tests := []struct {
		name        string
		annotations map[string]string
		mutate      func(*ValidationOptions)
		wantErr     bool
		wantContain string
	}{
		{
			name:        "annotation absent",
			annotations: nil,
			wantErr:     false,
		},
		{
			name:        "annotation false",
			annotations: map[string]string{annotationKey: "false"},
			wantErr:     false,
		},
		{
			name:        "annotation invalid",
			annotations: map[string]string{annotationKey: "yes"},
			wantErr:     true,
			wantContain: "expected \"true\" or \"false\"",
		},
		{
			name:        "preset missing",
			annotations: map[string]string{annotationKey: "true"},
			mutate: func(opts *ValidationOptions) {
				opts.PresetName = ""
			},
			wantErr:     true,
			wantContain: "missing preset",
		},
		{
			name:        "runtime mismatch",
			annotations: map[string]string{annotationKey: "true"},
			mutate: func(opts *ValidationOptions) {
				opts.Runtime = model.RuntimeNameHuggingfaceTransformers
			},
			wantErr:     true,
			wantContain: "wrong runtime",
		},
		{
			name:        "valid opt-in",
			annotations: map[string]string{annotationKey: "true"},
			wantErr:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := newOptions()
			if tc.mutate != nil {
				tc.mutate(&opts)
			}
			err := ValidateOptIn(tc.annotations, opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateOptIn() err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantContain != "" && (err == nil || !strings.Contains(err.Error(), tc.wantContain)) {
				t.Fatalf("ValidateOptIn() err=%v, want substring %q", err, tc.wantContain)
			}
		})
	}
}
