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

package inference

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetImageTag(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tt := range []struct {
		name, image, want string
		wantErr           bool
	}{
		{name: "CPU version", image: "registry.example/kaito-base-vllm-cpu:0.0.2", want: "0.0.2"},
		{name: "GPU version", image: "registry.example/kaito-base:0.0.1", want: "0.0.1"},
		{name: "registry port", image: "localhost:5000/kaito-base:0.0.3", want: "0.0.3"},
		{name: "short reference", image: "kaito-base:latest", want: "latest"},
		{name: "missing tag", image: "registry.example/kaito-base", wantErr: true},
		{name: "port is not a tag", image: "localhost:5000/kaito-base", wantErr: true},
		{name: "digest only", image: "registry.example/kaito-base@" + digest, wantErr: true},
		{name: "tag and digest", image: "registry.example/kaito-base:0.0.2@" + digest, wantErr: true},
		{name: "empty", wantErr: true},
		{name: "invalid reference", image: "not an image:0.0.2", wantErr: true},
		{name: "invalid label prefix", image: "kaito-base:_version", wantErr: true},
		{name: "invalid label suffix", image: "kaito-base:version-", wantErr: true},
		{name: "tag exceeds label length", image: "kaito-base:" + strings.Repeat("a", 64), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetImageTag(tt.image)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got, "invalid images must not authorize an upgrade")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
