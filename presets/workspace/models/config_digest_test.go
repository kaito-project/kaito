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
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configDigestVectors mirrors testdata/config_digest_vectors.json, which
// fetch_sas_test.py also consumes so the operator and the SAS-fetch init
// container are held to the same digest.
type configDigestVectors struct {
	Equivalent []struct {
		Name      string   `json:"name"`
		Inputs    []string `json:"inputs"`
		Canonical string   `json:"canonical"`
		SHA256    string   `json:"sha256"`
	} `json:"equivalent"`
	Invalid []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
	} `json:"invalid"`
}

func loadConfigDigestVectors(t *testing.T) configDigestVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/config_digest_vectors.json")
	require.NoError(t, err)
	var v configDigestVectors
	require.NoError(t, json.Unmarshal(raw, &v))
	require.NotEmpty(t, v.Equivalent)
	require.NotEmpty(t, v.Invalid)
	return v
}

func TestConfigDigestSharedVectors(t *testing.T) {
	v := loadConfigDigestVectors(t)
	for _, group := range v.Equivalent {
		t.Run(group.Name, func(t *testing.T) {
			require.GreaterOrEqual(t, len(group.Inputs), 2, "an equivalence group needs at least two spellings")
			for _, input := range group.Inputs {
				digest, err := ConfigDigest([]byte(input))
				require.NoError(t, err, "input: %s", input)
				assert.Equal(t, group.SHA256, digest, "input: %s", input)
			}
		})
	}
	for _, c := range v.Invalid {
		t.Run("invalid/"+c.Name, func(t *testing.T) {
			_, err := ConfigDigest([]byte(c.Input))
			assert.Error(t, err, "input must be rejected: %s", c.Input)
		})
	}
}

func TestResolveCustomModelFromConfigRejectsNonCanonicalizableConfig(t *testing.T) {
	_, err := ResolveCustomModelFromConfig([]byte(`{"hidden_size":1,"hidden_size":2}`), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), CustomModelConfigKey)
}

func mustConfigDigest(t *testing.T, configJSON string) string {
	t.Helper()
	digest, err := ConfigDigest([]byte(configJSON))
	require.NoError(t, err)
	return digest
}
