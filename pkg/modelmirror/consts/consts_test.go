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

package consts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveDownloadJobResources(t *testing.T) {
	tests := []struct {
		name        string
		cpu         string
		memory      string
		cpuLimit    string
		memoryLimit string
		want        DownloadJobResources
		wantError   string
	}{
		{
			name: "defaults",
			want: DefaultDownloadJobResources(),
		},
		{
			name:   "legacy request overrides also set limits",
			cpu:    "1",
			memory: "4Gi",
			want: DownloadJobResources{
				CPU: "1", Memory: "4Gi", CPULimit: "1", MemoryLimit: "4Gi",
			},
		},
		{
			name:        "explicit limits override request-derived limits",
			cpu:         "2",
			memory:      "6Gi",
			cpuLimit:    "4",
			memoryLimit: "10Gi",
			want: DownloadJobResources{
				CPU: "2", Memory: "6Gi", CPULimit: "4", MemoryLimit: "10Gi",
			},
		},
		{
			name:      "invalid CPU quantity",
			cpuLimit:  "not-a-quantity",
			wantError: "invalid ModelMirror download CPU limit",
		},
		{
			name:        "zero memory request",
			memory:      "0",
			memoryLimit: "1Gi",
			wantError:   "memory request must be greater than zero",
		},
		{
			name:      "CPU limit below request",
			cpu:       "3",
			cpuLimit:  "2",
			wantError: "CPU limit \"2\" must be greater than or equal to request \"3\"",
		},
		{
			name:        "memory limit below request",
			memory:      "8Gi",
			memoryLimit: "6Gi",
			wantError:   "memory limit \"6Gi\" must be greater than or equal to request \"8Gi\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDownloadJobResources(tt.cpu, tt.memory, tt.cpuLimit, tt.memoryLimit)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
