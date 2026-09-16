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
	corev1 "k8s.io/api/core/v1"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func customEnvValue(env []corev1.EnvVar, name string) (string, bool) {
	for _, e := range env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// The serving pod verifies the streamed bundle against the configuration
// digest. It reaches the pod only through PresetParam.Metadata, which
// GetInferenceParameters rebuilds field by field, so a value dropped upstream
// arrives here as an empty string and the check silently disables itself rather
// than failing.
func TestBuildMainContainerEnvPassesCustomModelIdentity(t *testing.T) {
	const digest = "727c648c8782616485cb141be58c0136c4916a9f2e75e752071d4e01e9f04eda"

	param := &pkgmodel.PresetParam{
		Metadata: pkgmodel.Metadata{
			Name: "custom-" + digest,
		},
	}

	env := buildMainContainerEnv(pkgmodel.RuntimeNameVLLM, param, "", "/workspace/weights")

	gotDigest, ok := customEnvValue(env, consts.ModelConfigSHA256EnvName)
	assert.True(t, ok, "the config digest must be passed to the serving container")
	assert.Equal(t, digest, gotDigest)
}

func TestBuildMainContainerEnvOmitsIdentityForPresetModels(t *testing.T) {
	param := &pkgmodel.PresetParam{
		Metadata: pkgmodel.Metadata{Name: "llama-3.1-8b-instruct"},
	}

	env := buildMainContainerEnv(pkgmodel.RuntimeNameVLLM, param, "", "/workspace/weights")

	_, ok := customEnvValue(env, consts.ModelConfigSHA256EnvName)
	assert.False(t, ok, "a preset model has no operator-supplied config to verify")
}
