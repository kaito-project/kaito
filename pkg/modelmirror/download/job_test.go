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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

			job := BuildDownloadJob(newTestModelMirror(), resources, nil)
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

func TestBuildDownloadJobScript(t *testing.T) {
	cr := newTestModelMirror()
	job := BuildDownloadJob(cr, mmconsts.DefaultDownloadJobResources(), nil)
	script := job.Spec.Template.Spec.Containers[0].Args[0]

	t.Run("does not install or enable hf_transfer", func(t *testing.T) {
		assert.NotContains(t, script, "hf_transfer")
		assert.NotContains(t, script, "HF_HUB_ENABLE_HF_TRANSFER")
	})

	t.Run("emits the download stats marker", func(t *testing.T) {
		assert.Contains(t, script, "KAITO_DOWNLOAD_STATS seconds=")
		assert.Contains(t, script, "bytes=")
	})

	t.Run("measures only the download phase", func(t *testing.T) {
		pipIdx := strings.Index(script, "pip install")
		startIdx := strings.Index(script, "_kaito_start=")
		endIdx := strings.Index(script, "_kaito_end=")
		cleanupIdx := strings.LastIndex(script, "rm -rf")

		require.NotEqual(t, -1, pipIdx)
		require.NotEqual(t, -1, startIdx)
		require.NotEqual(t, -1, endIdx)
		require.NotEqual(t, -1, cleanupIdx)

		assert.Less(t, pipIdx, startIdx, "pip install must precede the timed window")
		assert.Less(t, startIdx, endIdx, "start must precede end")
		assert.Less(t, endIdx, cleanupIdx, "cleanup must follow the timed window")
	})

	t.Run("still downloads and still cleans up", func(t *testing.T) {
		assert.Contains(t, script, `hf download "${MODEL_ID}"`)
		assert.Contains(t, script, "--exclude")
		assert.Contains(t, script, "-mindepth 1 -type d")
	})

	t.Run("date command renders correctly", func(t *testing.T) {
		assert.Contains(t, script, "_kaito_start=$(date +%s)")
		assert.Contains(t, script, "_kaito_end=$(date +%s)")
	})

	t.Run("emits no marker when the size is unknown", func(t *testing.T) {
		assert.Contains(t, script, `if [ -n "${_kaito_bytes}" ]; then`)
	})
}

func TestBuildDownloadJobServiceAccount(t *testing.T) {
	t.Run("empty SA leaves default SA and applies no labels", func(t *testing.T) {
		cr := newTestModelMirror() // ServiceAccountName unset
		job := BuildDownloadJob(cr, mmconsts.DefaultDownloadJobResources(), map[string]string{"azure.workload.identity/use": "true"})

		assert.Empty(t, job.Spec.Template.Spec.ServiceAccountName, "no ServiceAccount should be set")
		assert.NotContains(t, job.Spec.Template.Labels, "azure.workload.identity/use",
			"pod labels must not be applied when no ServiceAccount is set (account-key mount path)")
	})

	t.Run("set SA stamps SA and applies provider pod labels", func(t *testing.T) {
		cr := newTestModelMirror()
		cr.Spec.ServiceAccountName = "kaito-model-streamer"
		job := BuildDownloadJob(cr, mmconsts.DefaultDownloadJobResources(), map[string]string{"azure.workload.identity/use": "true"})

		assert.Equal(t, "kaito-model-streamer", job.Spec.Template.Spec.ServiceAccountName)
		assert.Equal(t, "true", job.Spec.Template.Labels["azure.workload.identity/use"],
			"provider pod labels must be applied so a workload-identity-authenticated StorageClass can mount")
	})

	t.Run("set SA with nil labels stamps SA but adds no labels", func(t *testing.T) {
		cr := newTestModelMirror()
		cr.Spec.ServiceAccountName = "kaito-model-streamer"
		job := BuildDownloadJob(cr, mmconsts.DefaultDownloadJobResources(), nil)

		assert.Equal(t, "kaito-model-streamer", job.Spec.Template.Spec.ServiceAccountName)
		assert.Empty(t, job.Spec.Template.Labels, "no pod labels expected when provider supplies none (non-Azure cloud)")
	})
}
