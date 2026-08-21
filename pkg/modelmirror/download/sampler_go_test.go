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

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
)

func TestSamplerSidecar(t *testing.T) {
	cr := &kaitov1alpha1.ModelMirror{}
	cr.Name = "mirror-abc123"
	cr.Spec.JobNamespace = "kaito-workspace"
	cr.Spec.Source = &kaitov1alpha1.ModelMirrorSource{ModelID: "meta-llama/Llama-3.1-8B-Instruct"}

	job := BuildDownloadJob(cr, mmconsts.DefaultDownloadJobResources(), nil)

	// A native sidecar is an initContainer with restartPolicy: Always. A plain
	// second container would run forever and the Job would never reach Succeeded.
	require.Len(t, job.Spec.Template.Spec.InitContainers, 1)
	sc := job.Spec.Template.Spec.InitContainers[0]

	require.NotNil(t, sc.RestartPolicy)
	assert.Equal(t, corev1.ContainerRestartPolicyAlways, *sc.RestartPolicy)
	assert.Equal(t, mmconsts.DownloaderImage, sc.Image)

	t.Run("mounts the model PVC read-only", func(t *testing.T) {
		require.Len(t, sc.VolumeMounts, 1)
		assert.Equal(t, "model-storage", sc.VolumeMounts[0].Name)
		assert.Equal(t, "/models", sc.VolumeMounts[0].MountPath)
		assert.True(t, sc.VolumeMounts[0].ReadOnly)
	})

	t.Run("declares the metrics port", func(t *testing.T) {
		require.Len(t, sc.Ports, 1)
		assert.Equal(t, int32(SamplerPort), sc.Ports[0].ContainerPort)
	})

	t.Run("receives MODEL_ID and the exclude patterns", func(t *testing.T) {
		env := map[string]string{}
		for _, e := range sc.Env {
			env[e.Name] = e.Value
		}
		assert.Equal(t, cr.Spec.Source.ModelID, env["MODEL_ID"])
		// The sampler applies the same excludes to its total-size lookup, so a
		// drift here would make the ETA permanently unreachable.
		assert.Equal(t, "original/*", env["EXCLUDE_PATTERNS"])
	})

	t.Run("delivers the sampler script", func(t *testing.T) {
		// The script is passed via an env var rather than as a literal Args
		// element: it contains quotes and newlines that would otherwise need
		// shell escaping inside the wrapper command.
		var script string
		for _, e := range sc.Env {
			if e.Name == "SAMPLER_SCRIPT" {
				script = e.Value
			}
		}
		assert.Contains(t, script, "model_mirror_download_speed_bytes_per_second")
	})

	t.Run("traps SIGTERM before exec", func(t *testing.T) {
		// Until `exec` replaces the shell, PID 1 is /bin/sh, which does not act on
		// SIGTERM while waiting on pip. A download that fails inside that window
		// leaves the kubelet's SIGTERM unhandled and the sidecar is SIGKILLed
		// after the full grace period (exit 137). Observed live on a job that
		// failed 15s in, while three sibling pods that failed at 10s exited 0.
		require.NotEmpty(t, sc.Args)
		wrapper := sc.Args[0]
		assert.Contains(t, wrapper, "trap 'exit 0' TERM")
		assert.Less(t, strings.Index(wrapper, "trap 'exit 0' TERM"), strings.Index(wrapper, "pip install"),
			"the trap must be installed before pip install, which is the window it protects")
	})
}

func TestSamplerSidecarHFToken(t *testing.T) {
	cr := &kaitov1alpha1.ModelMirror{}
	cr.Name = "mirror-abc123"
	cr.Spec.Source = &kaitov1alpha1.ModelMirrorSource{
		ModelID:      "meta-llama/Llama-3.1-8B-Instruct",
		AccessSecret: &corev1.ObjectReference{Name: "hf-token"},
	}

	job := BuildDownloadJob(cr, mmconsts.DefaultDownloadJobResources(), nil)
	sc := job.Spec.Template.Spec.InitContainers[0]

	// The total-size lookup hits the Hub API, which needs the token for gated repos.
	var found bool
	for _, e := range sc.Env {
		if e.Name == "HF_TOKEN" {
			found = true
			require.NotNil(t, e.ValueFrom)
			assert.Equal(t, "hf-token", e.ValueFrom.SecretKeyRef.Name)
		}
	}
	assert.True(t, found, "sampler needs HF_TOKEN for gated repos")
}
