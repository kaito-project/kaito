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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseExplicitMaxModelLen(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantVal   int
		wantFound bool
	}{
		{
			name:      "found under vllm section",
			content:   "vllm:\n  max-model-len: 4096\n",
			wantVal:   4096,
			wantFound: true,
		},
		{
			name:      "large value",
			content:   "vllm:\n  max-model-len: 131072\n",
			wantVal:   131072,
			wantFound: true,
		},
		{
			name:      "no vllm section",
			content:   "other:\n  key: value\n",
			wantFound: false,
		},
		{
			name:      "vllm section without max-model-len",
			content:   "vllm:\n  tensor-parallel-size: 4\n",
			wantFound: false,
		},
		{
			name:      "empty max-model-len value",
			content:   "vllm:\n  max-model-len:\n",
			wantFound: false,
		},
		{
			name:      "zero value is not valid",
			content:   "vllm:\n  max-model-len: 0\n",
			wantFound: false,
		},
		{
			name:      "negative value is not valid",
			content:   "vllm:\n  max-model-len: -1\n",
			wantFound: false,
		},
		{
			name:      "non-integer value is skipped",
			content:   "vllm:\n  max-model-len: abc\n",
			wantFound: false,
		},
		{
			name:      "max-model-len in different section is ignored",
			content:   "vllm:\n  tensor-parallel-size: 4\ntransformers:\n  max-model-len: 1024\n",
			wantFound: false,
		},
		{
			name:      "empty content",
			content:   "",
			wantFound: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, found := ParseExplicitMaxModelLen(tt.content)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantVal, val)
			}
		})
	}
}
