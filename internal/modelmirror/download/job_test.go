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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/internal/image"
)

func newTestModelMirror() *kaitov1alpha1.ModelMirror {
	return &kaitov1alpha1.ModelMirror{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-1", Namespace: "default"},
		Spec: kaitov1alpha1.ModelMirrorSpec{
			Source: &kaitov1alpha1.ModelMirrorSource{
				ModelID: "Qwen/Qwen3-8B-AWQ",
			},
		},
	}
}

func downloadJobContainer(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, container := range containers {
		if container.Name == name {
			return container
		}
	}
	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}

func TestDownloadJobPrepareModelCache(t *testing.T) {
	cr := newTestModelMirror()
	cr.Spec.Source.AccessSecretName = "hf-access"
	job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)
	pod := job.Spec.Template.Spec
	require.Len(t, pod.InitContainers, 2)
	assert.Equal(t, "prepare-model-cache", pod.InitContainers[0].Name)
	assert.Equal(t, "sampler", pod.InitContainers[1].Name)
	prepare := downloadJobContainer(t, pod.InitContainers, "prepare-model-cache")
	assert.Nil(t, prepare.RestartPolicy, "ordinary init must finish before the native sidecar starts")
	assert.Equal(t, image.PythonHelperImage, prepare.Image)
	assert.Equal(t, []string{"/bin/sh", "-c"}, prepare.Command)
	assert.Equal(t, []string{`mkdir -p "/models/${MODEL_ID}/.cache/huggingface"`}, prepare.Args)
	assert.Equal(t, []corev1.EnvVar{{Name: "MODEL_ID", Value: cr.Spec.Source.ModelID}}, prepare.Env,
		"preparation needs no HF token or other credentials")
	assert.Empty(t, prepare.EnvFrom)
	assert.Nil(t, prepare.SecurityContext, "preparation needs no extra privileges")
	assert.Equal(t, []corev1.VolumeMount{{Name: "model-storage", MountPath: "/models"}}, prepare.VolumeMounts,
		"only mount the PVC read-write, never the nested cache before creating its destination")
	assert.True(t, prepare.Resources.Requests.Cpu().Equal(resource.MustParse("10m")))
	assert.True(t, prepare.Resources.Requests.Memory().Equal(resource.MustParse("16Mi")))
	sampler := downloadJobContainer(t, pod.InitContainers, "sampler")
	require.NotNil(t, sampler.RestartPolicy)
	assert.Equal(t, corev1.ContainerRestartPolicyAlways, *sampler.RestartPolicy)
	require.Len(t, pod.Containers, 1)
	assert.Equal(t, "downloader", pod.Containers[0].Name)
}

func TestDownloadJobPrepareModelCacheScript(t *testing.T) {
	for _, modelID := range []string{"gpt2", "Qwen/Qwen3-Coder-30B-A3B-Instruct", "org/nested/model", "org/model $(touch injected); 'quoted'"} {
		t.Run(modelID, func(t *testing.T) {
			root := t.TempDir()
			cr := newTestModelMirror()
			cr.Spec.Source.ModelID = modelID
			job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)
			prepare := downloadJobContainer(t, job.Spec.Template.Spec.InitContainers, "prepare-model-cache")
			require.Equal(t, []corev1.EnvVar{{Name: "MODEL_ID", Value: modelID}}, prepare.Env)
			// Run the rendered script on an empty stand-in PVC, changing only its
			// mount root via an environment variable (no shell path interpolation).
			script := strings.ReplaceAll(prepare.Args[0], "/models/", "${TEST_MODEL_ROOT}/")
			run := func() {
				t.Helper()
				cmd := exec.Command(prepare.Command[0], append(prepare.Command[1:], script)...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "TEST_MODEL_ROOT="+root, "MODEL_ID="+prepare.Env[0].Value)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
			}
			run()
			// The exact destination of the later read-only nested mount now exists.
			sampler := downloadJobContainer(t, job.Spec.Template.Spec.InitContainers, "sampler")
			for _, mount := range sampler.VolumeMounts {
				assert.True(t, mount.ReadOnly)
				assert.DirExists(t, filepath.Join(root, strings.TrimPrefix(mount.MountPath, "/models")))
			}
			cache := filepath.Join(root, modelID, ".cache", "huggingface")
			assert.DirExists(t, cache)
			// Re-running preparation must preserve both final payload and cache data.
			for _, path := range []string{filepath.Join(root, modelID, "model.safetensors"), filepath.Join(cache, "shard.incomplete")} {
				require.NoError(t, os.WriteFile(path, []byte("keep existing data"), 0600))
			}
			run()
			for _, path := range []string{filepath.Join(root, modelID, "model.safetensors"), filepath.Join(cache, "shard.incomplete")} {
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "keep existing data", string(data))
			}
			assert.NoFileExists(t, filepath.Join(root, "injected"))
		})
	}

	t.Run("mkdir failure prevents successful initialization", func(t *testing.T) {
		root := t.TempDir()
		cr := newTestModelMirror()
		cr.Spec.Source.ModelID = "org/model"
		job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)
		prepare := downloadJobContainer(t, job.Spec.Template.Spec.InitContainers, "prepare-model-cache")
		// A file blocking the parent path fails even when tests run as root.
		blocker := filepath.Join(root, "org")
		require.NoError(t, os.WriteFile(blocker, []byte("keep"), 0600))
		script := strings.ReplaceAll(prepare.Args[0], "/models/", "${TEST_MODEL_ROOT}/")
		cmd := exec.Command(prepare.Command[0], append(prepare.Command[1:], script)...)
		cmd.Env = append(os.Environ(), "TEST_MODEL_ROOT="+root, "MODEL_ID="+prepare.Env[0].Value)
		output, err := cmd.CombinedOutput()
		require.Error(t, err, "%s", output)
		data, err := os.ReadFile(blocker)
		require.NoError(t, err)
		assert.Equal(t, "keep", string(data))
	})
}

func TestBuildDownloadJobResources(t *testing.T) {
	cases := []struct {
		name            string
		cpu             string
		memory          string
		cpuLimit        string
		memoryLimit     string
		wantCPU         string
		wantMemory      string
		wantCPULimit    string
		wantMemoryLimit string
	}{
		{
			name:            "defaults",
			wantCPU:         DefaultDownloadJobCPU,
			wantMemory:      DefaultDownloadJobMemory,
			wantCPULimit:    DefaultDownloadJobCPULimit,
			wantMemoryLimit: DefaultDownloadJobMemoryLimit,
		},
		{
			name:            "separate requests and limits",
			cpu:             "1",
			memory:          "4Gi",
			cpuLimit:        "3",
			memoryLimit:     "8Gi",
			wantCPU:         "1",
			wantMemory:      "4Gi",
			wantCPULimit:    "3",
			wantMemoryLimit: "8Gi",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resources := DefaultDownloadJobResources()
			if tc.cpu != "" {
				resources.CPU = tc.cpu
			}
			if tc.memory != "" {
				resources.Memory = tc.memory
			}
			if tc.cpuLimit != "" {
				resources.CPULimit = tc.cpuLimit
			}
			if tc.memoryLimit != "" {
				resources.MemoryLimit = tc.memoryLimit
			}

			job := BuildDownloadJob(newTestModelMirror(), resources, nil)
			containers := job.Spec.Template.Spec.Containers
			assert.Len(t, containers, 1)
			res := containers[0].Resources

			wantCPU := resource.MustParse(tc.wantCPU)
			wantMemory := resource.MustParse(tc.wantMemory)
			wantCPULimit := resource.MustParse(tc.wantCPULimit)
			wantMemoryLimit := resource.MustParse(tc.wantMemoryLimit)

			assert.True(t, res.Requests[corev1.ResourceCPU].Equal(wantCPU), "CPU request: got %s want %s", res.Requests.Cpu(), &wantCPU)
			assert.True(t, res.Requests[corev1.ResourceMemory].Equal(wantMemory), "memory request: got %s want %s", res.Requests.Memory(), &wantMemory)
			assert.True(t, res.Limits[corev1.ResourceCPU].Equal(wantCPULimit), "CPU limit: got %s want %s", res.Limits.Cpu(), &wantCPULimit)
			assert.True(t, res.Limits[corev1.ResourceMemory].Equal(wantMemoryLimit), "memory limit: got %s want %s", res.Limits.Memory(), &wantMemoryLimit)
		})
	}
}

func TestBuildDownloadJobScript(t *testing.T) {
	cr := newTestModelMirror()
	job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)
	script := job.Spec.Template.Spec.Containers[0].Args[0]

	t.Run("does not install or enable hf_transfer", func(t *testing.T) {
		assert.NotContains(t, script, "hf_transfer")
		assert.NotContains(t, script, "HF_HUB_ENABLE_HF_TRANSFER")
	})

	t.Run("still downloads and still cleans up", func(t *testing.T) {
		assert.Contains(t, script, `hf download "${MODEL_ID}"`)
		assert.Contains(t, script, "--exclude")
		assert.Contains(t, script, "-mindepth 1")
		assert.NotContains(t, script, "HF_DOWNLOAD_SCRIPT")
		assert.NotContains(t, script, "filelock==")
	})

	t.Run("pins pip before installing dependencies", func(t *testing.T) {
		pinIdx := strings.Index(script, image.PipBootstrapCommand)
		require.NotEqual(t, -1, pinIdx)
		assert.Less(t, pinIdx, strings.Index(script, `pip install -q "huggingface-hub==`))
	})

	t.Run("cache cleanup runs after the download", func(t *testing.T) {
		downloadIdx := strings.Index(script, `hf download "${MODEL_ID}"`)
		cleanupIdx := strings.Index(script, `find "/models/${MODEL_ID}/.cache/huggingface"`)
		require.NotEqual(t, -1, downloadIdx)
		require.NotEqual(t, -1, cleanupIdx)
		// .cache holds the *.incomplete files sampler.py reads to detect an
		// in-flight download. Cleaning it before the download finishes would make
		// the sampler report "finished" while bytes were still arriving.
		assert.Less(t, downloadIdx, cleanupIdx)
	})
}

func TestBuildDownloadJobXetTrust(t *testing.T) {
	for _, secretName := range []string{"", "hf-access"} {
		t.Run("accessSecret="+secretName, func(t *testing.T) {
			cr := newTestModelMirror()
			cr.Spec.Source.AccessSecretName = secretName
			job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)
			require.Len(t, job.Spec.Template.Spec.Containers, 1)
			require.Len(t, job.Spec.Template.Spec.InitContainers, 2)

			for _, tc := range []struct {
				container corev1.Container
				client    string
			}{
				{job.Spec.Template.Spec.Containers[0], `hf download "${MODEL_ID}"`},
				{downloadJobContainer(t, job.Spec.Template.Spec.InitContainers, "sampler"), `exec python3 -c "$SAMPLER_SCRIPT"`},
			} {
				t.Run(tc.container.Name, func(t *testing.T) {
					for _, env := range tc.container.Env {
						assert.NotEqual(t, "HF_HUB_DISABLE_XET", env.Name)
					}
					require.Len(t, tc.container.Args, 1)
					script := tc.container.Args[0]
					assert.Contains(t, script, `"huggingface-hub==2.3.0"`)
					install := strings.Index(script, `"certifi==`+CertifiVersion+`"`)
					resolve := strings.Index(script, `SSL_CERT_FILE="$(python3 -m certifi)"`)
					export := strings.Index(script, "export SSL_CERT_FILE")
					client := strings.Index(script, tc.client)
					require.NotEqual(t, -1, install)
					require.NotEqual(t, -1, resolve)
					require.NotEqual(t, -1, export)
					require.NotEqual(t, -1, client)
					assert.Less(t, install, resolve)
					assert.Less(t, resolve, export)
					assert.Less(t, export, client)
					// Combining assignment and export would mask a failed lookup under set -e.
					assert.NotContains(t, script, "export SSL_CERT_FILE=$(")
					assert.NotContains(t, script, `export SSL_CERT_FILE="$(`)
				})
			}
		})
	}
}

func TestBuildDownloadJobServiceAccount(t *testing.T) {
	t.Run("empty SA leaves default SA and applies no labels", func(t *testing.T) {
		cr := newTestModelMirror() // ServiceAccountName unset
		job := BuildDownloadJob(cr, DefaultDownloadJobResources(), map[string]string{"azure.workload.identity/use": "true"})

		assert.Empty(t, job.Spec.Template.Spec.ServiceAccountName, "no ServiceAccount should be set")
		assert.NotContains(t, job.Spec.Template.Labels, "azure.workload.identity/use",
			"pod labels must not be applied when no ServiceAccount is set (account-key mount path)")
	})

	t.Run("set SA stamps SA and applies provider pod labels", func(t *testing.T) {
		cr := newTestModelMirror()
		cr.Spec.ServiceAccountName = "kaito-model-streamer"
		job := BuildDownloadJob(cr, DefaultDownloadJobResources(), map[string]string{"azure.workload.identity/use": "true"})

		assert.Equal(t, "kaito-model-streamer", job.Spec.Template.Spec.ServiceAccountName)
		assert.Equal(t, "true", job.Spec.Template.Labels["azure.workload.identity/use"],
			"provider pod labels must be applied so a workload-identity-authenticated StorageClass can mount")
	})

	t.Run("set SA with nil labels stamps SA but adds no labels", func(t *testing.T) {
		cr := newTestModelMirror()
		cr.Spec.ServiceAccountName = "kaito-model-streamer"
		job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)

		assert.Equal(t, "kaito-model-streamer", job.Spec.Template.Spec.ServiceAccountName)
		assert.Empty(t, job.Spec.Template.Labels, "no pod labels expected when provider supplies none (non-Azure cloud)")
	})
}

func TestDownloadJobStillCompletesWithSidecar(t *testing.T) {
	cr := &kaitov1alpha1.ModelMirror{}
	cr.Name = "mirror-abc123"
	cr.Spec.Source = &kaitov1alpha1.ModelMirrorSource{ModelID: "some/model"}
	job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)

	// The sampler must be an initContainer with restartPolicy Always. If it were
	// a regular container it would run forever and the Job would hang at 0/1.
	assert.Len(t, job.Spec.Template.Spec.Containers, 1,
		"the sampler must not be a regular container")
	require.Len(t, job.Spec.Template.Spec.InitContainers, 2)
	require.NotNil(t, downloadJobContainer(t, job.Spec.Template.Spec.InitContainers, "sampler").RestartPolicy)
}

func TestDownloadJobLocalCache(t *testing.T) {
	for _, modelID := range []string{"gpt2", "Qwen/Qwen3-Coder-30B-A3B-Instruct", "org/nested/model"} {
		t.Run(modelID, func(t *testing.T) {
			cr := newTestModelMirror()
			cr.Spec.Source.ModelID = modelID
			job := BuildDownloadJob(cr, DefaultDownloadJobResources(), nil)
			volumes := job.Spec.Template.Spec.Volumes
			require.Len(t, volumes, 2)
			assert.Equal(t, "hf-local-cache", volumes[0].Name)
			require.NotNil(t, volumes[0].EmptyDir)
			assert.Empty(t, volumes[0].EmptyDir.Medium, "incomplete weights need disk, not tmpfs")
			assert.Nil(t, volumes[0].EmptyDir.SizeLimit, "do not cap weight staging at a lock-only size")
			assert.Equal(t, cr.Name, volumes[1].PersistentVolumeClaim.ClaimName)
			mount := corev1.VolumeMount{Name: "hf-local-cache", MountPath: fmt.Sprintf("/models/%s/.cache/huggingface", modelID)}
			container := job.Spec.Template.Spec.Containers[0]
			assert.Contains(t, container.VolumeMounts, mount)
			assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "model-storage", MountPath: "/models"})
			mount.ReadOnly = true
			assert.Contains(t, downloadJobContainer(t, job.Spec.Template.Spec.InitContainers, "sampler").VolumeMounts, mount)
			assert.Contains(t, container.Env, corev1.EnvVar{Name: "MODEL_ID", Value: modelID})
			assert.Contains(t, container.Args[0], `hf download "${MODEL_ID}"`)
			assert.Contains(t, container.Args[0], `--local-dir "/models/${MODEL_ID}"`)
			assert.Contains(t, container.Args[0], "--max-workers 4")
			assert.Contains(t, container.Args[0], "HF_HUB_DOWNLOAD_TIMEOUT=300")
			for _, pattern := range DownloadExcludePatterns {
				assert.Contains(t, container.Args[0], pattern)
			}
			for _, env := range container.Env {
				assert.NotEqual(t, "HF_DOWNLOAD_SCRIPT", env.Name)
			}
		})
	}
}

func TestDownloadJobCleanupPreservesCacheMount(t *testing.T) {
	for _, downloadSucceeds := range []bool{true, false} {
		t.Run(fmt.Sprintf("downloadSucceeds=%t", downloadSucceeds), func(t *testing.T) {
			root := t.TempDir()
			modelID := "org/model"
			modelDir := filepath.Join(root, modelID)
			cache := filepath.Join(modelDir, ".cache", "huggingface")
			files := []string{"model.safetensors", ".cache/huggingface/.gitignore", ".cache/huggingface/download/shard.incomplete", "other/nested/unused"}
			for _, name := range files {
				path := filepath.Join(modelDir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte("data"), 0600))
			}
			job := BuildDownloadJob(newTestModelMirror(), DefaultDownloadJobResources(), nil)
			script := job.Spec.Template.Spec.Containers[0].Args[0]
			// Run the actual post-download shell with an isolated fake download result.
			cleanup := script[strings.Index(script, "# Empty the cache"):]
			result := "true"
			if !downloadSucceeds {
				result = "false"
			}
			cmd := exec.Command("/bin/sh", "-c", "set -e\n"+result+"\n"+strings.ReplaceAll(cleanup, "/models/", root+"/"))
			cmd.Env = append(os.Environ(), "MODEL_ID="+modelID)
			output, err := cmd.CombinedOutput()
			if downloadSucceeds {
				require.NoError(t, err, "%s", output)
			} else {
				require.Error(t, err)
			}
			assert.DirExists(t, cache)
			assert.DirExists(t, filepath.Dir(cache))
			assert.FileExists(t, filepath.Join(modelDir, "model.safetensors"))
			entries, err := os.ReadDir(cache)
			require.NoError(t, err)
			if downloadSucceeds {
				assert.Empty(t, entries)
				assert.NoDirExists(t, filepath.Join(modelDir, "other"))
			} else {
				assert.NotEmpty(t, entries)
				assert.FileExists(t, filepath.Join(cache, "download", "shard.incomplete"))
				assert.DirExists(t, filepath.Join(modelDir, "other"))
			}
		})
	}
}
