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
	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestIsCPUInferenceTarget(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	preset := &InferenceSpec{Preset: &PresetSpec{PresetMeta: PresetMeta{Name: "test-model"}}}
	for _, tc := range []struct {
		sku  string
		want bool
	}{
		{"Standard_D16s_v6", true}, {"Standard_E8s_v5", true}, {"Standard_D2s", true},
		{"Standard_D16ps_v5", false}, {"Standard_D16ps", false}, {"Standard_E16pds_v6", false},
		{"Standard_NC6s_v3", false}, {"Standard_ND999s_v9", false}, {"", false},
		{"m6i.2xlarge", false}, {"unknown", false},
	} {
		t.Run(tc.sku, func(t *testing.T) { assert.Equal(t, tc.want, IsCPUInferenceTarget(preset, tc.sku)) })
	}
	assert.False(t, IsCPUInferenceTarget(nil, "Standard_D16s_v6"))
	assert.False(t, IsCPUInferenceTarget(&InferenceSpec{Template: &corev1.PodTemplateSpec{}}, "Standard_D16s_v6"))
	t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)
	assert.False(t, IsCPUInferenceTarget(preset, "Standard_D16s_v6"))
}

func TestCPUInstanceTypeForNAPRejectsUnsupportedTargets(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	for _, instanceType := range []string{"Standard_D16ps_v5", "Standard_D16ps", "Standard_NC6s_v3", "Standard_ND999s_v9", "m6i.2xlarge", "unknown", ""} {
		t.Run(instanceType, func(t *testing.T) { assert.NotNil(t, ValidateCPUInstanceTypeForNAP(instanceType)) })
	}
	assert.Nil(t, ValidateCPUInstanceTypeForNAP("Standard_D16s_v6"))
	t.Setenv("CLOUD_PROVIDER", consts.AWSCloudName)
	assert.NotNil(t, ValidateCPUInstanceTypeForNAP("Standard_D16s_v6"))
}
