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

import "testing"

func TestValidateOptIn(t *testing.T) {
	annotationKey := "kaito.sh/enable-speculative-decoding"

	tests := []struct {
		name             string
		annotations      map[string]string
		wantEnabled      bool
		wantInvalidValue string
	}{
		{
			name:        "annotation absent",
			annotations: nil,
		},
		{
			name:        "annotation false",
			annotations: map[string]string{annotationKey: "false"},
		},
		{
			name:             "annotation invalid",
			annotations:      map[string]string{annotationKey: "yes"},
			wantInvalidValue: "yes",
		},
		{
			name:        "valid opt-in",
			annotations: map[string]string{annotationKey: "true"},
			wantEnabled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enabled, invalidValue := ValidateOptIn(tc.annotations, annotationKey)
			if enabled != tc.wantEnabled {
				t.Fatalf("ValidateOptIn() enabled=%v want=%v", enabled, tc.wantEnabled)
			}
			if invalidValue != tc.wantInvalidValue {
				t.Fatalf("ValidateOptIn() invalidValue=%q want=%q", invalidValue, tc.wantInvalidValue)
			}
		})
	}
}
