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

package estimator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveGPUMemoryUtilization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		modelName string
		gpuModel  string
		gpuCount  int
		want      float64
	}{
		{"default", "other-model", "NVIDIA H100", 8, 0.92},
		{"GPU policy", "other-model", "NVIDIA A10", 1, 0.82},
		{"model policy on 8xH100", "deepseek-v4-flash-0731", "NVIDIA H100", 8, 0.90},
		{"model policy does not apply to 2xH100", "deepseek-v4-flash-0731", "NVIDIA H100", 2, 0.92},
		{"model policy does not apply to other GPUs", "deepseek-v4-flash-0731", "NVIDIA A100", 8, 0.92},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ResolveGPUMemoryUtilization(tc.modelName, tc.gpuModel, tc.gpuCount))
		})
	}
}
