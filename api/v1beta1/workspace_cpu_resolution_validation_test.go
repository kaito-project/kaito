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

package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/pkg/consts"
	"github.com/kaito-project/kaito/pkg/featuregates"
)

func TestValidateCPUInferenceRuntimeAndCount(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	old := featuregates.FeatureGates[featuregates.FeatureFlagVLLM]
	t.Cleanup(func() { featuregates.FeatureGates[featuregates.FeatureFlagVLLM] = old })
	for _, tc := range []struct {
		name, runtime string
		gate          bool
		count         int
		valid         bool
	}{
		{"default vllm", "", true, 1, true}, {"explicit vllm", "vllm", true, 1, true},
		{"transformers rejected", "transformers", true, 1, false},
		{"unknown runtime rejected", "unknown", true, 1, false},
		{"explicit vllm gate disabled", "vllm", false, 1, false},
		{"vllm gate disabled", "", false, 1, false}, {"multinode", "vllm", true, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featuregates.FeatureGates[featuregates.FeatureFlagVLLM] = tc.gate
			ws := &Workspace{Resource: ResourceSpec{InstanceType: "Standard_D16s_v6", Count: &tc.count}, Inference: &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}}
			ws.Annotations = map[string]string{AnnotationWorkspaceRuntime: tc.runtime}
			assert.Equal(t, tc.valid, ws.validateCPUInferenceConstraints() == nil)
		})
	}
}
