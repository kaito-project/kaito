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

package models

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
	"github.com/kaito-project/kaito/presets/workspace/generator"
)

const testCustomConfigJSON = `{
	"architectures": ["LlamaForCausalLM"],
	"hidden_size": 4096,
	"intermediate_size": 14336,
	"num_hidden_layers": 32,
	"num_attention_heads": 32,
	"num_key_value_heads": 8,
	"max_position_embeddings": 131072,
	"vocab_size": 128256,
	"torch_dtype": "bfloat16"
}`

const testSizeBytes = "16060522496"

func customConfigMap(name string, data map[string]string, immutable bool) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Immutable:  ptr.To(immutable),
		Data:       data,
	}
}

// validCustomData is a complete, resolvable ConfigMap payload.
func validCustomData(configJSON string) map[string]string {
	return map[string]string{
		CustomModelConfigKey: configJSON,
		CustomModelSizeKey:   testSizeBytes,
	}
}

func fakeClientWith(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntimeMustAddCoreV1(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func utilruntimeMustAddCoreV1(scheme *runtime.Scheme) {
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

func TestResolveCustomModelIsContentAddressed(t *testing.T) {
	cm := customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true)

	resolved, err := ResolveCustomModelFromConfigMap(cm)
	require.NoError(t, err)

	assert.Equal(t, ConfigDigest([]byte(testCustomConfigJSON)), resolved.Digest)
	assert.Equal(t, CustomModelName(resolved.Digest), resolved.Name)
	assert.True(t, strings.HasPrefix(resolved.Name, plugin.CustomModelNamePrefix))
	require.NotNil(t, resolved.Model)
	assert.NotEmpty(t, resolved.Model.GetInferenceParameters().TotalSafeTensorFileSize,
		"a custom model must be sizable, or node estimation has nothing to work from")

	// The same bytes under a different ConfigMap name must resolve to the same
	// model identity; identity follows content, not the object holding it.
	renamed := customConfigMap("byo-config-renamed", validCustomData(testCustomConfigJSON), true)
	again, err := ResolveCustomModelFromConfigMap(renamed)
	require.NoError(t, err)
	assert.Equal(t, resolved.Name, again.Name)

	// Different bytes must resolve to a different identity, so that a changed
	// model can never reuse another model's cached parameters.
	altered := customConfigMap("byo-config-2", validCustomData(
		`{"architectures":["LlamaForCausalLM"],"hidden_size":2048,`+
			`"intermediate_size":8192,"num_hidden_layers":16,"num_attention_heads":16,`+
			`"num_key_value_heads":4,"vocab_size":32000,"torch_dtype":"bfloat16"}`), true)
	other, err := ResolveCustomModelFromConfigMap(altered)
	require.NoError(t, err)
	assert.NotEqual(t, resolved.Name, other.Name)
}

func TestResolveCustomModelRebuildsFromConfigAlone(t *testing.T) {
	// The registry is an in-memory cache wiped on controller restart. Because
	// resolution is content-addressed and the ConfigMap is immutable, a cold
	// resolution must reproduce exactly what was cached before, without any
	// durable state - otherwise a restart could change a running deployment's
	// sizing or runtime arguments.
	uniqueConfig := `{
		"architectures": ["LlamaForCausalLM"],
		"hidden_size": 3072,
		"intermediate_size": 8192,
		"num_hidden_layers": 28,
		"num_attention_heads": 24,
		"num_key_value_heads": 8,
		"vocab_size": 128256,
		"torch_dtype": "bfloat16"
	}`
	sizeBytes, err := strconv.ParseInt(testSizeBytes, 10, 64)
	require.NoError(t, err)
	name := CustomModelName(ConfigDigest([]byte(uniqueConfig)))
	cacheKey := customModelCacheKey(name, sizeBytes)
	require.False(t, plugin.KaitoModelRegister.Has(cacheKey), "config must not already be cached")

	cm := customConfigMap("byo-config", validCustomData(uniqueConfig), true)

	cold, err := ResolveCustomModelFromConfigMap(cm)
	require.NoError(t, err)
	require.True(t, plugin.KaitoModelRegister.Has(cacheKey), "a cold resolution must populate the cache")

	// Independently re-derive from the same bytes: what the cache holds must be
	// exactly what a fresh process would compute.
	expected, err := generator.GenerateFromConfig(name, []byte(uniqueConfig), sizeBytes)
	require.NoError(t, err)

	coldParams := cold.Model.GetInferenceParameters()
	assert.Equal(t, expected.Metadata.ModelFileSize, coldParams.TotalSafeTensorFileSize,
		"the weight footprint the estimator sizes from must survive a rebuild")
	assert.Equal(t, expected.Metadata.Architectures, coldParams.Metadata.Architectures)
	assert.Equal(t, expected.Metadata.AttnType, coldParams.Metadata.AttnType)
	assert.Equal(t, expected.Metadata.BytesPerToken, coldParams.BytesPerToken)
	assert.Equal(t, expected.Metadata.ModelTokenLimit, coldParams.ModelTokenLimit)

	// Every run parameter the generator derived must reach the serving command.
	for k, v := range expected.VLLM.ModelRunParams {
		assert.Equal(t, v, coldParams.VLLM.ModelRunParams[k], "run parameter %q", k)
	}

	warm, err := ResolveCustomModelFromConfigMap(cm)
	require.NoError(t, err)
	assert.Equal(t, cold.Name, warm.Name)
	assert.Equal(t, cold.Digest, warm.Digest)
}

func TestResolveCustomModelRequiresImmutableConfigMap(t *testing.T) {
	// A mutable ConfigMap would let the model change underneath a deployment
	// that was already sized and configured from the original contents.
	mutable := customConfigMap("byo-config", validCustomData(testCustomConfigJSON), false)

	_, err := ResolveCustomModelFromConfigMap(mutable)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")

	unset := customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true)
	unset.Immutable = nil
	_, err = ResolveCustomModelFromConfigMap(unset)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")
}

func TestResolveCustomModelRejectsMissingOrEmptyConfig(t *testing.T) {
	tests := []struct {
		name      string
		data      map[string]string
		errSubstr string
	}{
		{
			name:      "no config.json",
			data:      map[string]string{"inference_config.yaml": "vllm: {}", CustomModelSizeKey: testSizeBytes},
			errSubstr: CustomModelConfigKey,
		},
		{
			name:      "blank config.json",
			data:      map[string]string{CustomModelConfigKey: "   \n", CustomModelSizeKey: testSizeBytes},
			errSubstr: "empty",
		},
		{
			name:      "unparsable config.json",
			data:      map[string]string{CustomModelConfigKey: "not json", CustomModelSizeKey: testSizeBytes},
			errSubstr: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveCustomModelFromConfigMap(customConfigMap("byo-config", tt.data, true))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestResolveCustomModelFromCluster(t *testing.T) {
	cm := customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true)
	c := fakeClientWith(cm)

	resolved, err := ResolveCustomModel(context.Background(), c, "byo-config", "default")
	require.NoError(t, err)
	assert.Equal(t, ConfigDigest([]byte(testCustomConfigJSON)), resolved.Digest)

	_, err = ResolveCustomModel(context.Background(), c, "missing-config", "default")
	assert.Error(t, err)

	_, err = ResolveCustomModel(context.Background(), c, "", "default")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inference.config")
}

func TestGetModelByNameRejectsReservedCustomNames(t *testing.T) {
	// The "custom-<digest>" namespace is internal. Accepting it as user input
	// would let a workspace address, or collide with, a cached model identity
	// it never supplied the configuration for.
	_, err := GetModelByName(context.Background(),
		plugin.CustomModelNamePrefix+"deadbeef", "", "", "", nil)
	require.Error(t, err)

	assert.False(t, plugin.IsValidPreset(plugin.CustomModelNamePrefix+"deadbeef"))
	assert.True(t, plugin.IsValidPreset(plugin.PresetNameCustom))
}

func TestGetModelByNameResolvesCustomPreset(t *testing.T) {
	cm := customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true)
	c := fakeClientWith(cm)

	m, err := GetModelByName(context.Background(), plugin.PresetNameCustom, "byo-config", "", "default", c)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.NotEmpty(t, m.GetInferenceParameters().TotalSafeTensorFileSize)
}

func TestParseModelSizeRejectsUnusableValues(t *testing.T) {
	// Every one of these reaches resource.MustParse downstream, which panics on
	// malformed input, so they have to be rejected at this single entry point.
	tests := []struct {
		name      string
		data      map[string]string
		errSubstr string
	}{
		{
			name:      "key absent",
			data:      map[string]string{CustomModelConfigKey: testCustomConfigJSON},
			errSubstr: CustomModelSizeKey,
		},
		{
			name:      "blank",
			data:      map[string]string{CustomModelConfigKey: testCustomConfigJSON, CustomModelSizeKey: "  "},
			errSubstr: CustomModelSizeKey,
		},
		{
			name:      "quantity suffix rather than raw bytes",
			data:      map[string]string{CustomModelConfigKey: testCustomConfigJSON, CustomModelSizeKey: "15Gi"},
			errSubstr: "bytes",
		},
		{
			name:      "zero",
			data:      map[string]string{CustomModelConfigKey: testCustomConfigJSON, CustomModelSizeKey: "0"},
			errSubstr: "greater than zero",
		},
		{
			name:      "negative",
			data:      map[string]string{CustomModelConfigKey: testCustomConfigJSON, CustomModelSizeKey: "-1"},
			errSubstr: "greater than zero",
		},
		{
			name:      "implausibly large",
			data:      map[string]string{CustomModelConfigKey: testCustomConfigJSON, CustomModelSizeKey: "9223372036854775807"},
			errSubstr: "exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveCustomModelFromConfigMap(customConfigMap("byo-config", tt.data, true))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestResolveCustomModelRebuildsWhenDeclaredSizeChanges(t *testing.T) {
	// A ConfigMap that corrects a wrong size must resolve to parameters built
	// from the new size, never the stale one, or the corrected size would be
	// recorded in status while capacity kept being planned from the old value.
	// The cache key is qualified by size, so the two sizes occupy independent
	// slots and neither evicts the other.
	config := `{"architectures":["LlamaForCausalLM"],"hidden_size":3072,` +
		`"intermediate_size":8192,"num_hidden_layers":28,"num_attention_heads":24,` +
		`"num_key_value_heads":8,"vocab_size":128256,"torch_dtype":"bfloat16"}`

	small, err := ResolveCustomModelFromConfigMap(customConfigMap("byo-a", map[string]string{
		CustomModelConfigKey: config,
		CustomModelSizeKey:   "1073741824",
	}, true))
	require.NoError(t, err)
	require.Equal(t, "1Gi", small.Model.GetInferenceParameters().TotalSafeTensorFileSize)

	large, err := ResolveCustomModelFromConfigMap(customConfigMap("byo-b", map[string]string{
		CustomModelConfigKey: config,
		CustomModelSizeKey:   "10737418240",
	}, true))
	require.NoError(t, err)

	assert.Equal(t, small.Name, large.Name, "identical configuration means the same identity")
	assert.Equal(t, int64(10737418240), large.SizeBytes)
	assert.Equal(t, "10Gi", large.Model.GetInferenceParameters().TotalSafeTensorFileSize,
		"the entry for the newly declared size must reflect it, not the cached one")

	// Resolving the original size again must return the entry left standing by
	// the first resolution: the larger size took its own slot rather than
	// evicting the smaller one, so two deployments sharing a config.json but
	// declaring different sizes cannot thrash each other's cache.
	again, err := ResolveCustomModelFromConfigMap(customConfigMap("byo-a", map[string]string{
		CustomModelConfigKey: config,
		CustomModelSizeKey:   "1073741824",
	}, true))
	require.NoError(t, err)
	assert.Equal(t, "1Gi", again.Model.GetInferenceParameters().TotalSafeTensorFileSize)
	assert.Same(t, small.Model, again.Model,
		"the smaller size's cached entry must survive the larger resolution")
}

func TestResolveCustomModelReusesCacheForIdenticalInputs(t *testing.T) {
	cm := customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true)

	first, err := ResolveCustomModelFromConfigMap(cm)
	require.NoError(t, err)
	second, err := ResolveCustomModelFromConfigMap(cm)
	require.NoError(t, err)

	assert.Same(t, first.Model, second.Model, "identical inputs must hit the cache")
}

func TestResolveCustomModelFromConfig(t *testing.T) {
	sizeBytes, err := strconv.ParseInt(testSizeBytes, 10, 64)
	require.NoError(t, err)

	// A caller with no ConfigMap - e.g. a non-Kubernetes system that only wants a
	// node estimate - resolves straight from the bytes and declared size.
	resolved, err := ResolveCustomModelFromConfig([]byte(testCustomConfigJSON), sizeBytes)
	require.NoError(t, err)
	require.NotNil(t, resolved.Model)
	assert.Equal(t, ConfigDigest([]byte(testCustomConfigJSON)), resolved.Digest)
	assert.Equal(t, sizeBytes, resolved.SizeBytes)
	assert.NotEmpty(t, resolved.Model.GetInferenceParameters().TotalSafeTensorFileSize,
		"the resolved model must be sizable by the estimator")

	// Identity is content-addressed, so the ConfigMap path resolves the same
	// bytes and size to the same model.
	viaConfigMap, err := ResolveCustomModelFromConfigMap(
		customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true))
	require.NoError(t, err)
	assert.Equal(t, viaConfigMap.Name, resolved.Name)
	assert.Same(t, viaConfigMap.Model, resolved.Model, "both paths must share the cached entry")
}

func TestResolveCustomModelFromConfigRejectsBadInputs(t *testing.T) {
	sizeBytes, err := strconv.ParseInt(testSizeBytes, 10, 64)
	require.NoError(t, err)

	_, err = ResolveCustomModelFromConfig([]byte("  \n"), sizeBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), CustomModelConfigKey)

	_, err = ResolveCustomModelFromConfig([]byte(testCustomConfigJSON), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")

	_, err = ResolveCustomModelFromConfig([]byte("not json"), sizeBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestCustomModelDigestRoundTrips(t *testing.T) {
	resolved, err := ResolveCustomModelFromConfigMap(
		customConfigMap("byo-config", validCustomData(testCustomConfigJSON), true))
	require.NoError(t, err)

	digest, ok := model.CustomModelDigest(resolved.Name)
	require.True(t, ok)
	assert.Equal(t, resolved.Digest, digest)

	for _, name := range []string{"llama-3.1-8b-instruct", "custom-", "phi-4"} {
		_, ok := model.CustomModelDigest(name)
		assert.False(t, ok, "%q is not a custom model identity", name)
	}
}
