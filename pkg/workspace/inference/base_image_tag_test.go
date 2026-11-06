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

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func TestGetBaseImageNameAndTagCatalog(t *testing.T) {
	t.Setenv("PRESET_REGISTRY_NAME", "registry.example")
	for _, name := range []string{"base", "base-cpu"} {
		t.Run(name, func(t *testing.T) {
			baseImage := metadata.MustGet(name)
			got, err := GetBaseImageTag(baseImage)
			require.NoError(t, err)
			assert.Equal(t, baseImage.Tag, got)
			assert.Equal(t, "registry.example/kaito-"+name+":"+got, GetBaseImageName(baseImage))
		})
	}
}

func TestGetBaseImageTagValidation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tt := range []struct {
		name, registry, imageName, tag, want string
		wantErr                              bool
	}{
		{name: "CPU version", registry: "registry.example", imageName: "base-cpu", tag: "0.0.2", want: "0.0.2"},
		{name: "GPU version", registry: "registry.example", imageName: "base", tag: "0.6.4", want: "0.6.4"},
		{name: "registry port and v prefix", registry: "localhost:5000", imageName: "base", tag: "v0.0.3", want: "v0.0.3"},
		{name: "missing tag", registry: "registry.example", imageName: "base", wantErr: true},
		{name: "port is not a tag", registry: "localhost:5000", imageName: "base", wantErr: true},
		{name: "digest only", registry: "registry.example", imageName: "base@" + digest, wantErr: true},
		{name: "tag and digest", registry: "registry.example", imageName: "base", tag: "0.0.2@" + digest, wantErr: true},
		{name: "empty metadata", wantErr: true},
		{name: "invalid registry", registry: "not a registry", imageName: "base", tag: "0.0.2", wantErr: true},
		{name: "invalid label prefix", registry: "registry.example", imageName: "base", tag: "_version", wantErr: true},
		{name: "invalid label suffix", registry: "registry.example", imageName: "base", tag: "version-", wantErr: true},
		{name: "tag exceeds label length", registry: "registry.example", imageName: "base", tag: strings.Repeat("a", 64), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetBaseImageTag(pkgmodel.Metadata{Registry: tt.registry, Name: tt.imageName, Tag: tt.tag})
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got, "invalid base images must not authorize an upgrade")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
