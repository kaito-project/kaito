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

package main

import "testing"

func TestGuardrailsScanContent(t *testing.T) {
	tests := []struct {
		name        string
		banned      []string
		content     string
		wantBlocked bool
	}{
		{
			name:        "clean content passes",
			banned:      []string{"SECRET"},
			content:     "hello world",
			wantBlocked: false,
		},
		{
			name:        "banned substring blocks",
			banned:      []string{"SECRET", "PASSWORD"},
			content:     "my SECRET key",
			wantBlocked: true,
		},
		{
			name:        "case insensitive",
			banned:      []string{"API_KEY"},
			content:     "use api_key here",
			wantBlocked: true,
		},
		{
			name:        "multiple substrings",
			banned:      []string{"INTERNAL", "CONFIDENTIAL"},
			content:     "This is CONFIDENTIAL info",
			wantBlocked: true,
		},
	}

	blockMsg := "Response blocked"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &GuardrailsEngine{
				config: &GuardrailsConfig{
					BannedSubstrings: tt.banned,
					BlockMessage:     blockMsg,
					Enabled:          true,
				},
			}

			result := engine.ScanContent(tt.content)

			isBlocked := result == blockMsg
			if isBlocked != tt.wantBlocked {
				t.Errorf("expected blocked=%v, got blocked=%v, result=%s", tt.wantBlocked, isBlocked, result)
			}
		})
	}
}
