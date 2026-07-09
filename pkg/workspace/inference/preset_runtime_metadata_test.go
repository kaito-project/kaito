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
	"testing"

	"github.com/stretchr/testify/assert"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

func TestGetBaseRuntimeVersion(t *testing.T) {
	base := metadata.MustGet("base")

	// The base entry must define both engine versions so the status can surface them.
	assert.NotEmpty(t, base.RuntimeVersion.VLLM, "base vllm runtime version should be set")
	assert.NotEmpty(t, base.RuntimeVersion.Transformers, "base transformers runtime version should be set")

	assert.Equal(t, base.RuntimeVersion.VLLM, GetBaseRuntimeVersion(pkgmodel.RuntimeNameVLLM))
	assert.Equal(t, base.RuntimeVersion.Transformers, GetBaseRuntimeVersion(pkgmodel.RuntimeNameHuggingfaceTransformers))
	assert.Empty(t, GetBaseRuntimeVersion(pkgmodel.RuntimeName("unknown")))
}

func TestGetPresetQuantization(t *testing.T) {
	assert.Empty(t, GetPresetQuantization(""), "empty preset name yields empty quantization")
	assert.Empty(t, GetPresetQuantization("does-not-exist"), "unknown preset yields empty quantization")
	// The base model is not quantized.
	assert.Empty(t, GetPresetQuantization("base"))
}
