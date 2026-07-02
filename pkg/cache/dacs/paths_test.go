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

package dacs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelBlobRelativePath(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		modelID  string
		revision string
		expected string
	}{
		{
			name:     "default prefix and revision",
			prefix:   "",
			modelID:  "microsoft/phi-4",
			revision: "",
			expected: "kaito-models/microsoft/phi-4/main",
		},
		{
			name:     "custom prefix with revision",
			prefix:   "my-models",
			modelID:  "meta-llama/Llama-3.3-70B-Instruct",
			revision: "abc123",
			expected: "my-models/meta-llama/Llama-3.3-70B-Instruct/abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ModelBlobRelativePath(tt.prefix, tt.modelID, tt.revision)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelLocalPath(t *testing.T) {
	tests := []struct {
		name        string
		storagePath string
		prefix      string
		modelID     string
		revision    string
		expected    string
	}{
		{
			name:        "default storage path",
			storagePath: "",
			prefix:      "",
			modelID:     "microsoft/phi-4",
			revision:    "main",
			expected:    "/mnt/models/kaito-models/microsoft/phi-4/main",
		},
		{
			name:        "custom storage path",
			storagePath: "/data/cache",
			prefix:      "",
			modelID:     "microsoft/phi-4",
			revision:    "abc123",
			expected:    "/data/cache/kaito-models/microsoft/phi-4/abc123",
		},
		{
			name:        "trailing slash on storage path",
			storagePath: "/mnt/models/",
			prefix:      "",
			modelID:     "meta-llama/Llama-3.3-70B-Instruct",
			revision:    "",
			expected:    "/mnt/models/kaito-models/meta-llama/Llama-3.3-70B-Instruct/main",
		},
		{
			name:        "custom prefix",
			storagePath: "",
			prefix:      "custom",
			modelID:     "microsoft/phi-4",
			revision:    "main",
			expected:    "/mnt/models/custom/microsoft/phi-4/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ModelLocalPath(tt.storagePath, tt.prefix, tt.modelID, tt.revision)
			assert.Equal(t, tt.expected, result)
		})
	}
}
