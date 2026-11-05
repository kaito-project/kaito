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

package nodes

import (
	"testing"

	"github.com/go-logr/logr"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

var spotTaint = corev1.Taint{Key: consts.SpotInstanceKey, Value: consts.SpotInstanceValue, Effect: corev1.TaintEffectNoSchedule}

func toleratesTaint(tolerations []corev1.Toleration, taint corev1.Taint) bool {
	for i := range tolerations {
		if tolerations[i].ToleratesTaint(logr.Discard(), &taint, false) {
			return true
		}
	}
	return false
}

func TestGPUTolerations(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)

	taints := []corev1.Taint{
		// Common taint on GPU nodes, e.g. EKS GPU node groups or the NVIDIA GPU operator.
		{Key: CapacityNvidiaGPU, Value: "present", Effect: corev1.TaintEffectNoSchedule},
		{Key: CapacityNvidiaGPU, Effect: corev1.TaintEffectNoSchedule},
		// Taint KAITO puts on the GPU nodes it provisions.
		{Key: consts.SKUString, Value: consts.GPUString, Effect: corev1.TaintEffectNoSchedule},
	}

	for _, tolerations := range [][]corev1.Toleration{GPUTolerations(), WorkspaceTolerations()} {
		for _, taint := range taints {
			assert.Assert(t, toleratesTaint(tolerations, taint), "taint %s=%s:%s is not tolerated", taint.Key, taint.Value, taint.Effect)
		}
	}

	// GPUTolerations is used for RAG and never includes spot, regardless of cloud provider.
	assert.Assert(t, !toleratesTaint(GPUTolerations(), spotTaint))
}

func TestWorkspaceTolerations(t *testing.T) {
	testcases := map[string]struct {
		cloudProvider string
		expectSpot    bool
	}{
		"azure includes spot toleration": {
			cloudProvider: consts.AzureCloudName,
			expectSpot:    true,
		},
		"aws excludes spot toleration": {
			cloudProvider: consts.AWSCloudName,
			expectSpot:    false,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLOUD_PROVIDER", tc.cloudProvider)

			assert.Equal(t, tc.expectSpot, toleratesTaint(WorkspaceTolerations(), spotTaint))
		})
	}
}
