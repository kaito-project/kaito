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

package tolerations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestSetFromJSONAndAppend(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, SetFromJSON("")) })

	require.NoError(t, SetFromJSON(`[
		{"key":"accelerator","operator":"Equal","value":"h100","effect":"NoSchedule"},
		{"key":"tenant","operator":"Exists","effect":"NoExecute","tolerationSeconds":60}
	]`))

	base := []corev1.Toleration{{Key: "provider", Operator: corev1.TolerationOpExists}}
	result := Append(base)

	assert.Equal(t, base, result[:1])
	assert.Equal(t, corev1.Toleration{
		Key:      "accelerator",
		Operator: corev1.TolerationOpEqual,
		Value:    "h100",
		Effect:   corev1.TaintEffectNoSchedule,
	}, result[1])
	assert.NotNil(t, result[2].TolerationSeconds)
	assert.Equal(t, int64(60), *result[2].TolerationSeconds)
	assert.Equal(t, base, []corev1.Toleration{{Key: "provider", Operator: corev1.TolerationOpExists}})
}

func TestSetFromJSONRejectsInvalidInput(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, SetFromJSON("")) })

	err := SetFromJSON(`{"key":"missing-array"}`)
	require.Error(t, err)
	assert.ErrorContains(t, err, "parsing custom workload tolerations as JSON")
	assert.Empty(t, Append(nil))
}
