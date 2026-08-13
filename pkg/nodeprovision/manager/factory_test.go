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

package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNodeClasses(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "empty flag",
			raw:     "",
			wantErr: "--karpenter-node-classes is required",
		},
		{
			name:    "malformed JSON",
			raw:     `[{"name":`,
			wantErr: "parsing --karpenter-node-classes as a JSON array",
		},
		{
			name:    "JSON string instead of array",
			raw:     `"[{\"name\":\"a\"}]"`,
			wantErr: "parsing --karpenter-node-classes as a JSON array",
		},
		{
			name:    "empty array",
			raw:     `[]`,
			wantErr: "declares no NodeClasses",
		},
		{
			name:    "empty name",
			raw:     `[{"name":"","default":true}]`,
			wantErr: "has an empty name",
		},
		{
			name:    "name is not a valid resource name",
			raw:     `[{"name":"Not_A_Name","default":true}]`,
			wantErr: "is not a valid resource name",
		},
		{
			name:    "duplicate names",
			raw:     `[{"name":"a","default":true},{"name":"a"}]`,
			wantErr: `declares "a" more than once`,
		},
		{
			name:    "no default",
			raw:     `[{"name":"a"},{"name":"b"}]`,
			wantErr: "must mark exactly one entry default, got 0",
		},
		{
			name:    "two defaults",
			raw:     `[{"name":"a","default":true},{"name":"b","default":true}]`,
			wantErr: "must mark exactly one entry default, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNodeClasses(tt.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("valid input is parsed and sorted by name", func(t *testing.T) {
		entries, err := ParseNodeClasses(`[
			{"name":"image-family-ubuntu","default":true,"spec":{"imageFamily":"Ubuntu2204","osDiskSizeGB":300}},
			{"name":"image-family-azure-linux","spec":{"imageFamily":"AzureLinux"}}
		]`)
		require.NoError(t, err)
		require.Len(t, entries, 2)

		assert.Equal(t, "image-family-azure-linux", entries[0].Name)
		assert.False(t, entries[0].Default)
		assert.Equal(t, "image-family-ubuntu", entries[1].Name)
		assert.True(t, entries[1].Default)
		assert.Equal(t, map[string]interface{}{
			"imageFamily": "Ubuntu2204", "osDiskSizeGB": float64(300),
		}, entries[1].Spec)
	})

	t.Run("spec is an opaque passthrough for other providers", func(t *testing.T) {
		entries, err := ParseNodeClasses(`[{"name":"ec2","default":true,"spec":{"amiFamily":"AL2023","subnetSelectorTerms":[{"tags":{"karpenter.sh/discovery":"c"}}]}}]`)
		require.NoError(t, err)
		assert.Equal(t, "AL2023", entries[0].Spec["amiFamily"])
		assert.Len(t, entries[0].Spec["subnetSelectorTerms"], 1)
	})
}
