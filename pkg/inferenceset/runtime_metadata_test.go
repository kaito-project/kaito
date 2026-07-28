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

package inferenceset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/pkg/workspace/inference"
)

// TestInferenceSetRuntimeMetadata verifies that the runtime metadata written into
// the aggregated benchmark metric's Config resolves the same way the InferenceSet
// controller populates it, using the InferenceSet's own runtime name.
func TestInferenceSetRuntimeMetadata(t *testing.T) {
	iObj := &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-inferenceset"},
		Spec: kaitov1beta1.InferenceSetSpec{
			Template: kaitov1beta1.InferenceSetTemplate{
				Inference: kaitov1beta1.InferenceSpec{
					Preset: &kaitov1beta1.PresetSpec{
						PresetMeta: kaitov1beta1.PresetMeta{Name: kaitov1beta1.ModelName("base")},
					},
				},
			},
		},
	}

	// Mirror the controller: resolve runtime name and preset name, then populate
	// the aggregated metric's Config.
	runtimeName := kaitov1beta1.GetInferenceSetRuntimeName(iObj)
	presetName := string(iObj.Spec.Template.Inference.Preset.Name)

	metric := kaitov1beta1.Metric{
		Value:  "42",
		Config: controllers.RuntimeMetadataConfig(runtimeName, presetName),
	}

	require.NotNil(t, metric.Config)
	assert.Equal(t, string(runtimeName), metric.Config[controllers.ConfigKeyEngine])
	assert.Equal(t, inference.GetBaseRuntimeVersion(runtimeName), metric.Config[controllers.ConfigKeyEngineVersion])
	assert.NotEmpty(t, metric.Config[controllers.ConfigKeyEngineVersion])
	// base is not quantized.
	assert.Empty(t, metric.Config[controllers.ConfigKeyQuantization])
}
