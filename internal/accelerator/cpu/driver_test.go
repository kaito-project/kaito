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

package cpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

func TestDriver(t *testing.T) {
	d := Driver{}
	assert.Equal(t, corev1.ResourceRequirements{}, d.ResourceRequirements(0, ""))
	assert.Equal(t, []corev1.Toleration{
		{Key: "sku", Operator: corev1.TolerationOpEqual, Value: "cpu", Effect: corev1.TaintEffectNoSchedule},
	}, d.Tolerations())
	image, err := d.InferenceBaseImage(pkgmodel.RuntimeNameVLLM)
	assert.NoError(t, err)
	assert.Equal(t, "base-cpu", image.Name)
	_, err = d.InferenceBaseImage(pkgmodel.RuntimeNameHuggingfaceTransformers)
	assert.EqualError(t, err, "CPU preset inference supports only vllm")
	assert.Equal(t, map[string]string{"kaito.sh/cpu-inference-workload": "true"}, d.ExclusiveNodeLabels())
}
