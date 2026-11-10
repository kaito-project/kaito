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

package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

func TestSearchRawExtension(t *testing.T) {
	tests := []struct {
		name      string
		raw       k8sruntime.RawExtension
		key       string
		wantValue interface{}
		wantFound bool
		wantErr   bool
	}{
		{
			name:      "key found",
			raw:       k8sruntime.RawExtension{Raw: []byte(`{"model": "llama2"}`)},
			key:       "model",
			wantValue: "llama2",
			wantFound: true,
		},
		{
			name:      "key not found",
			raw:       k8sruntime.RawExtension{Raw: []byte(`{"model": "llama2"}`)},
			key:       "version",
			wantFound: false,
		},
		{
			name:    "invalid yaml",
			raw:     k8sruntime.RawExtension{Raw: []byte(`{invalid`)},
			key:     "model",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, found, err := SearchRawExtension(tt.raw, tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				assert.Equal(t, tt.wantValue, val)
			}
		})
	}
}

func TestExtractAndValidateRepoName(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{"lowercase repo", "registry.io/org/mymodel:v1", false},
		{"uppercase repo", "registry.io/org/MyModel:v1", true},
		{"no tag lowercase", "registry.io/org/mymodel", false},
		{"mixed case in org", "registry.io/Org/mymodel:v1", false}, // only repo name checked
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExtractAndValidateRepoName(tt.image)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
