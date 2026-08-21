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
	_ "embed"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
)

// samplerScript computes download speed and ETA and serves them as Prometheus
// gauges. Delivered inline as a container arg.
//
//go:embed sampler.py
var samplerScript string

// SamplerPort is the port the sampler serves /metrics on. The operator reaches
// it through the API server's pod-proxy subresource.
const SamplerPort = 9100

// buildSamplerContainer returns the sampler as a native sidecar
//
// envVars is the downloader's own env, which already carries MODEL_ID and the
// optional HF_TOKEN.
func buildSamplerContainer(envVars []corev1.EnvVar) corev1.Container {
	// The sampler's total-size lookup uses HfFileSystem, so it needs the same
	// huggingface-hub pin as the downloader.
	//
	// The `trap` covers the startup window before `exec`. Until exec replaces the
	// shell, PID 1 is /bin/sh, which does not act on SIGTERM while waiting on a
	// non-interactive command.
	shell := fmt.Sprintf(`set -e
trap 'exit 0' TERM
pip install -q "huggingface-hub==%s"
exec python3 -c "$SAMPLER_SCRIPT"`, mmconsts.HuggingFaceHubVersion)

	env := append([]corev1.EnvVar{}, envVars...)
	env = append(env,
		corev1.EnvVar{Name: "EXCLUDE_PATTERNS", Value: strings.Join(mmconsts.DownloadExcludePatterns, ",")},
		corev1.EnvVar{Name: "SAMPLER_SCRIPT", Value: samplerScript},
	)

	return corev1.Container{
		Name:          "sampler",
		Image:         mmconsts.DownloaderImage,
		RestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
		Command:       []string{"/bin/sh", "-c"},
		Args:          []string{shell},
		Env:           env,
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: SamplerPort},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "model-storage", MountPath: "/models", ReadOnly: true},
		},
	}
}
