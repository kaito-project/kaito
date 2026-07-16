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

package modelstreaming

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

// sasCfg returns a StreamingConfig shaped like SASBlobProvider.GetStreamingConfig returns:
// one slim-python init container, one memory-backed emptyDir volume, one provider env var.
func sasCfg() *StreamingConfig {
	return &StreamingConfig{
		ModelPath: "az://container/model",
		ProviderEnvVars: []corev1.EnvVar{
			{Name: "AZURE_STORAGE_ACCOUNT_NAME", Value: "myacct"},
		},
		PodLabels: map[string]string{"azure.workload.identity/use": "true"},
		InitContainers: []corev1.Container{
			{
				Name:    "fetch-sas",
				Image:   "python:3.12-slim",
				Command: []string{"/bin/sh", "-c", "pip install ... && python3 -c \"$FETCH_SAS_SCRIPT\""},
				Env: []corev1.EnvVar{
					{Name: "FETCH_SAS_SCRIPT", Value: "# embedded script"},
					{Name: "STREAM_DATAREFS_URL", Value: "https://example.com/datarefs"},
					{Name: "STREAM_ASSET_ID", Value: "azureml://registries/r/models/m/versions/1"},
					{Name: "STREAM_BLOB_URI", Value: "https://myacct.blob.core.windows.net/c/prefix"},
					{Name: SASEnvFileEnvVar, Value: SASSharedMountPath + "/" + SASEnvFileName},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: SASSharedVolumeName, MountPath: SASSharedMountPath},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: SASSharedVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
				},
			},
		},
	}
}

// TestSetStreamingConfig_SASWiring verifies that when a SAS-style StreamingConfig (with init
// containers and a shared volume) is applied, SetStreamingConfig:
//   - appends the init container as-is (image already set by the provider)
//   - appends the shared volume to the pod
//   - mounts the shared volume in the main container
//   - prepends the transparent entrypoint wrapper to the main container command (unchanged cmd)
//   - sets STREAM_ENV_FILE on the main container so the wrapper can source the SAS env file
//   - still sets ServiceAccountName and provider env vars (existing behaviour)
func TestSetStreamingConfig_SASWiring(t *testing.T) {
	const wsName = "phi-4-ws"
	const defaultSA = "streaming-sa"

	ws := &v1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wsName,
			Namespace: "default",
		},
	}
	ctx := &generator.WorkspaceGeneratorContext{Workspace: ws}

	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:    wsName,
				Image:   "some-existing-image:tag",
				Command: []string{"/bin/sh", "-c", "python3 foo"},
			},
		},
	}

	cfg := sasCfg()

	err := SetStreamingConfig(cfg, "microsoft/phi-4", defaultSA)(ctx, spec)
	require.NoError(t, err)

	// 1. Init container appended as-is (image already set by the provider)
	require.Len(t, spec.InitContainers, 1)
	ic := spec.InitContainers[0]
	assert.Equal(t, "fetch-sas", ic.Name)
	assert.Equal(t, "python:3.12-slim", ic.Image, "init container keeps the slim-python image from the provider")

	// 2. Shared volume appended to pod
	volumeNames := make([]string, 0, len(spec.Volumes))
	for _, v := range spec.Volumes {
		volumeNames = append(volumeNames, v.Name)
	}
	assert.Contains(t, volumeNames, SASSharedVolumeName, "pod volumes should include %q", SASSharedVolumeName)

	// 3. Main container has volume mount at the shared mount path
	mainIdx := -1
	for i := range spec.Containers {
		if spec.Containers[i].Name == wsName {
			mainIdx = i
			break
		}
	}
	require.NotEqual(t, -1, mainIdx, "main container must still exist")
	main := spec.Containers[mainIdx]

	mountPaths := make([]string, 0, len(main.VolumeMounts))
	for _, vm := range main.VolumeMounts {
		mountPaths = append(mountPaths, vm.MountPath)
	}
	assert.Contains(t, mountPaths, SASSharedMountPath, "main container must mount %q", SASSharedMountPath)

	// 4. Main container command is the ORIGINAL command, unchanged, with the wrapper prepended.
	assert.Equal(t, []string{sasTokenExportWrapper, "/bin/sh", "-c", "python3 foo"}, main.Command,
		"wrapper must be prepended and the original command left intact")

	// 5. ServiceAccountName set
	assert.Equal(t, defaultSA, spec.ServiceAccountName)

	// 6. Provider env var + STREAM_ENV_FILE on the main container
	envByName := map[string]string{}
	for _, e := range main.Env {
		envByName[e.Name] = e.Value
	}
	assert.Equal(t, "myacct", envByName["AZURE_STORAGE_ACCOUNT_NAME"])
	assert.Equal(t, SASSharedMountPath+"/"+SASEnvFileName, envByName[SASEnvFileEnvVar],
		"main container must know where to source the SAS env file")
}

// TestSetStreamingConfig_AzurePathUnchanged verifies that when a config has NO init containers
// or volumes (the existing Azure/PVC path), SetStreamingConfig does NOT add volumes, mounts,
// or command prefix — existing behaviour is preserved exactly.
func TestSetStreamingConfig_AzurePathUnchanged(t *testing.T) {
	const wsName = "phi-4-ws"
	const defaultSA = "streaming-sa"

	ws := &v1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wsName,
			Namespace: "default",
		},
	}
	ctx := &generator.WorkspaceGeneratorContext{Workspace: ws}

	originalCmd := []string{"/bin/sh", "-c", "python3 bar"}
	spec := &corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:    wsName,
				Command: originalCmd,
			},
		},
	}

	// Azure-style config: no InitContainers, no Volumes
	azureCfg := &StreamingConfig{
		ModelPath: "az://container/model",
		ProviderEnvVars: []corev1.EnvVar{
			{Name: "AZURE_STORAGE_ACCOUNT_NAME", Value: "myacct"},
		},
	}

	err := SetStreamingConfig(azureCfg, "microsoft/phi-4", defaultSA)(ctx, spec)
	require.NoError(t, err)

	// No init containers added
	assert.Empty(t, spec.InitContainers)

	// No volumes added
	assert.Empty(t, spec.Volumes)

	// Main container has no extra volume mounts
	assert.Empty(t, spec.Containers[0].VolumeMounts)

	// Command unchanged
	assert.Equal(t, []string{"/bin/sh", "-c", "python3 bar"}, spec.Containers[0].Command)

	// ServiceAccountName still set (existing behaviour)
	assert.Equal(t, defaultSA, spec.ServiceAccountName)
}
