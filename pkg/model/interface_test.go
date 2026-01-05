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

package model

import (
	"testing"
)

func TestVLLMModel_Validate(t *testing.T) {
	tests := []struct {
		name    string
		model   *VLLMModel
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid model with name and version",
			model: &VLLMModel{
				Name:    "test-model",
				Version: "https://huggingface.co/mistralai/Mistral-7B-v0.3/commit/d8cadc02ac76bd617a919d50b092e59d2d110aff",
			},
			wantErr: false,
		},
		{
			name: "missing name",
			model: &VLLMModel{
				Name:    "",
				Version: "https://huggingface.co/mistralai/Mistral-7B-v0.3/commit/d8cadc02ac76bd617a919d50b092e59d2d110aff",
			},
			wantErr: true,
			errMsg:  "model name is required",
		},
		{
			name: "missing version",
			model: &VLLMModel{
				Name:    "test-model",
				Version: "",
			},
			wantErr: true,
			errMsg:  "model version is required",
		},
		{
			name: "invalid version format",
			model: &VLLMModel{
				Name:    "test-model",
				Version: "invalid-url",
			},
			wantErr: true,
		},
		{
			name: "both name and version missing",
			model: &VLLMModel{
				Name:    "",
				Version: "",
			},
			wantErr: true,
			errMsg:  "model name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.model.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("VLLMModel.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("VLLMModel.Validate() error message = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}
