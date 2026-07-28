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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
)

func TestRuntimeMetadataConfig(t *testing.T) {
	t.Run("vllm engine", func(t *testing.T) {
		cfg := RuntimeMetadataConfig(pkgmodel.RuntimeNameVLLM, "base")
		require.NotNil(t, cfg)
		assert.Equal(t, string(pkgmodel.RuntimeNameVLLM), cfg[ConfigKeyEngine])
		assert.Equal(t, inference.GetBaseRuntimeVersion(pkgmodel.RuntimeNameVLLM), cfg[ConfigKeyEngineVersion])
		assert.NotEmpty(t, cfg[ConfigKeyEngineVersion])
		// base is not quantized.
		assert.Empty(t, cfg[ConfigKeyQuantization])
	})

	t.Run("transformers engine", func(t *testing.T) {
		cfg := RuntimeMetadataConfig(pkgmodel.RuntimeNameHuggingfaceTransformers, "base")
		assert.Equal(t, string(pkgmodel.RuntimeNameHuggingfaceTransformers), cfg[ConfigKeyEngine])
		assert.Equal(t, inference.GetBaseRuntimeVersion(pkgmodel.RuntimeNameHuggingfaceTransformers), cfg[ConfigKeyEngineVersion])
		assert.NotEmpty(t, cfg[ConfigKeyEngineVersion])
	})

	t.Run("unknown preset yields empty quantization", func(t *testing.T) {
		cfg := RuntimeMetadataConfig(pkgmodel.RuntimeNameVLLM, "does-not-exist")
		assert.Empty(t, cfg[ConfigKeyQuantization])
	})
}
