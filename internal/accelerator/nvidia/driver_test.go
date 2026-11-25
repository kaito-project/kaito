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

package nvidia

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

func TestResourceRequirements(t *testing.T) {
	two := *resource.NewQuantity(2, resource.DecimalSI)
	assert.Equal(t, corev1.ResourceRequirements{
		Requests: corev1.ResourceList{"nvidia.com/gpu": two},
		Limits:   corev1.ResourceList{"nvidia.com/gpu": two},
	}, Driver{}.ResourceRequirements(2, ""))

	one := *resource.NewQuantity(1, resource.DecimalSI)
	assert.Equal(t, corev1.ResourceRequirements{
		Requests: corev1.ResourceList{"nvidia.com/mig-1g.10gb": one},
		Limits:   corev1.ResourceList{"nvidia.com/mig-1g.10gb": one},
	}, Driver{}.ResourceRequirements(1, "1g.10gb"))
}

func TestTolerations(t *testing.T) {
	assert.Equal(t, []corev1.Toleration{
		{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
		{Key: "sku", Operator: corev1.TolerationOpEqual, Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
	}, Driver{}.Tolerations())
	assert.Equal(t, []corev1.Toleration{
		{Key: "nvidia.com/mig-1g.10gb", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
	}, Driver{}.PartitionTolerations("1g.10gb"))
}

func TestInferenceBaseImage(t *testing.T) {
	for _, runtime := range []pkgmodel.RuntimeName{pkgmodel.RuntimeNameVLLM, pkgmodel.RuntimeNameHuggingfaceTransformers} {
		image, err := Driver{}.InferenceBaseImage(runtime)
		assert.NoError(t, err)
		assert.Equal(t, "base", image.Name)
	}
}
