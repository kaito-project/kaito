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

import (
	"encoding/json"
	"testing"
)

func TestProcessOpenAIResponse(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		shouldErr      bool
		checkPreserved bool
		checkContent   bool
	}{
		{
			name: "valid single choice",
			input: `{
				"id": "chatcmpl-abc123",
				"object": "chat.completion",
				"created": 1234567890,
				"model": "gpt-4",
				"usage": {"prompt_tokens": 10, "completion_tokens": 20},
				"choices": [{"message": {"role": "assistant", "content": "hello"}}]
			}`,
			shouldErr:      false,
			checkPreserved: true,
			checkContent:   true,
		},
		{
			name: "multiple choices preserved",
			input: `{
				"id": "chatcmpl-multi",
				"model": "gpt-4",
				"choices": [
					{"message": {"content": "response 1"}},
					{"message": {"content": "response 2"}}
				]
			}`,
			shouldErr:      false,
			checkPreserved: true,
			checkContent:   true,
		},
		{
			name:      "malformed JSON",
			input:     `{invalid json}`,
			shouldErr: true,
		},
		{
			name:      "no choices field",
			input:     `{"id": "test", "model": "gpt-4"}`,
			shouldErr: true,
		},
		{
			name:      "empty choices",
			input:     `{"id": "test", "choices": []}`,
			shouldErr: true,
		},
		{
			name: "choice without message - skip gracefully",
			input: `{
				"id": "test",
				"choices": [{"index": 0}]
			}`,
			shouldErr: false, // fail-open: just skip this choice
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processOpenAIResponse([]byte(tt.input))

			if tt.shouldErr && err == nil {
				t.Errorf("expected error, got nil")
				return
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.shouldErr {
				return
			}

			// Verify output is valid JSON
			var output map[string]any
			if err := json.Unmarshal(result, &output); err != nil {
				t.Errorf("output is not valid JSON: %v", err)
				return
			}

			// Check preserved fields
			if tt.checkPreserved {
				if tt.name == "valid single choice" {
					if output["id"] != "chatcmpl-abc123" {
						t.Errorf("id field not preserved")
					}
					if output["model"] != "gpt-4" {
						t.Errorf("model field not preserved")
					}
					if usage, ok := output["usage"].(map[string]any); !ok {
						t.Errorf("usage field not preserved")
					} else {
						if usage["prompt_tokens"] != float64(10) {
							t.Errorf("usage.prompt_tokens corrupted")
						}
					}
				}
			}

			// Check choices array still exists and has content
			if tt.checkContent {
				choices, ok := output["choices"].([]any)
				if !ok {
					t.Errorf("choices field missing or wrong type")
					return
				}
				if len(choices) == 0 {
					t.Errorf("choices array is empty after processing")
					return
				}
				// Verify at least one choice has message.content
				found := false
				for _, c := range choices {
					choice, ok := c.(map[string]any)
					if !ok {
						continue
					}
					message, ok := choice["message"].(map[string]any)
					if !ok {
						continue
					}
					if _, ok := message["content"].(string); ok {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no valid message.content found in choices")
				}
			}
		})
	}
}
