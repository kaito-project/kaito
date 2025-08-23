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
	"os"
	"testing"
)

func TestGetPresetImageName(t *testing.T) {
	type args struct {
		registry string
		name     string
		tag      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "all parameters provided",
			args: args{
				registry: "myregistry.com",
				name:     "phi-3",
				tag:      "v1.0",
			},
			want: "myregistry.com/kaito-phi-3:v1.0",
		},
		{
			name: "empty registry uses environment variable",
			args: args{
				registry: "",
				name:     "llama-2",
				tag:      "latest",
			},
			want: "test-registry.io/kaito-llama-2:latest",
		},
		{
			name: "registry with different format",
			args: args{
				registry: "docker.io/kaito",
				name:     "falcon",
				tag:      "7b",
			},
			want: "docker.io/kaito/kaito-falcon:7b",
		},
		{
			name: "complex model name with hyphens",
			args: args{
				registry: "azurecr.io",
				name:     "phi-3-mini-128k-instruct",
				tag:      "v2.1.3",
			},
			want: "azurecr.io/kaito-phi-3-mini-128k-instruct:v2.1.3",
		},
		{
			name: "minimal case",
			args: args{
				registry: "reg",
				name:     "m",
				tag:      "t",
			},
			want: "reg/kaito-m:t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variable for the test case that needs it
			if tt.name == "empty registry uses environment variable" {
				originalValue := os.Getenv("PRESET_REGISTRY_NAME")
				os.Setenv("PRESET_REGISTRY_NAME", "test-registry.io")
				defer func() {
					if originalValue == "" {
						os.Unsetenv("PRESET_REGISTRY_NAME")
					} else {
						os.Setenv("PRESET_REGISTRY_NAME", originalValue)
					}
				}()
			}

			if got := GetPresetImageName(tt.args.registry, tt.args.name, tt.args.tag); got != tt.want {
				t.Errorf("GetPresetImageName() = %v, want %v", got, tt.want)
			}
		})
	}
}
