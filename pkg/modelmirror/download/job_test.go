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

package download

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
)

func newTestModelMirror() *kaitov1alpha1.ModelMirror {
	return &kaitov1alpha1.ModelMirror{
		Spec: kaitov1alpha1.ModelMirrorSpec{
			JobNamespace: "default",
			Source: &kaitov1alpha1.ModelMirrorSource{
				ModelID: "Qwen/Qwen3-8B-AWQ",
			},
		},
	}
}

func TestBuildDownloadJobResources(t *testing.T) {
	cases := []struct {
		name       string
		cpu        string
		memory     string
		wantCPU    string
		wantMemory string
	}{
		{
			name:       "defaults",
			wantCPU:    mmconsts.DefaultDownloadJobCPU,
			wantMemory: mmconsts.DefaultDownloadJobMemory,
		},
		{
			name:       "overridden for constrained clusters",
			cpu:        "2",
			memory:     "4Gi",
			wantCPU:    "2",
			wantMemory: "4Gi",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resources := mmconsts.DefaultDownloadJobResources()
			if tc.cpu != "" {
				resources.CPU = tc.cpu
			}
			if tc.memory != "" {
				resources.Memory = tc.memory
			}

			job := BuildDownloadJob(newTestModelMirror(), resources)
			containers := job.Spec.Template.Spec.Containers
			assert.Len(t, containers, 1)
			res := containers[0].Resources

			wantCPU := resource.MustParse(tc.wantCPU)
			wantMemory := resource.MustParse(tc.wantMemory)

			assert.True(t, res.Requests[corev1.ResourceCPU].Equal(wantCPU), "CPU request: got %s want %s", res.Requests.Cpu(), &wantCPU)
			assert.True(t, res.Requests[corev1.ResourceMemory].Equal(wantMemory), "memory request: got %s want %s", res.Requests.Memory(), &wantMemory)

			// request == limit is an invariant for the download Job.
			assert.True(t, res.Limits[corev1.ResourceCPU].Equal(res.Requests[corev1.ResourceCPU]), "CPU limit must equal request")
			assert.True(t, res.Limits[corev1.ResourceMemory].Equal(res.Requests[corev1.ResourceMemory]), "memory limit must equal request")
		})
	}
}
