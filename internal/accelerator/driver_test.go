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

package accelerator

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/accelerator"
	"github.com/kaito-project/kaito/pkg/consts"
	"github.com/kaito-project/kaito/pkg/sku"
)

func TestFor(t *testing.T) {
	tests := []struct {
		name string
		cfg  *sku.GPUConfig
		want accelerator.Type
	}{
		{name: "nil config is CPU", cfg: nil, want: accelerator.TypeCPU},
		{name: "NVIDIA", cfg: &sku.GPUConfig{Accelerator: accelerator.TypeNVIDIA}, want: accelerator.TypeNVIDIA},
		{name: "unset accelerator keeps NVIDIA", cfg: &sku.GPUConfig{}, want: accelerator.TypeNVIDIA},
		{name: "AMD keeps NVIDIA", cfg: &sku.GPUConfig{Accelerator: accelerator.TypeAMD}, want: accelerator.TypeNVIDIA},
		{name: "Neuron keeps NVIDIA", cfg: &sku.GPUConfig{Accelerator: accelerator.TypeNeuron}, want: accelerator.TypeNVIDIA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, For(tt.cfg).Type())
		})
	}
}

func TestForWorkspace(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	preset := &v1beta1.InferenceSpec{Preset: &v1beta1.PresetSpec{PresetMeta: v1beta1.PresetMeta{Name: "test-model"}}}
	for _, tt := range []struct {
		instanceType string
		want         accelerator.Type
	}{
		{instanceType: "Standard_D8s_v5", want: accelerator.TypeCPU},
		{instanceType: "Standard_NC6s_v3", want: accelerator.TypeNVIDIA},
		{instanceType: "Standard_NG32ads_V620_v1", want: accelerator.TypeNVIDIA},
		{instanceType: "", want: accelerator.TypeNVIDIA},
	} {
		t.Run(tt.instanceType, func(t *testing.T) {
			ws := &v1beta1.Workspace{Resource: v1beta1.ResourceSpec{InstanceType: tt.instanceType}, Inference: preset}
			assert.Equal(t, tt.want, ForWorkspace(t.Context(), ws).Type())
		})
	}
	assert.Equal(t, accelerator.TypeNVIDIA, ForWorkspace(t.Context(), nil).Type())
}
