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

package azure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

func wsWithStreamAnnotations() *v1beta1.Workspace {
	return &v1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws1",
			Namespace: "default",
			Annotations: map[string]string{
				modelstreaming.AnnotationStreamURI:              "az://c/model",
				modelstreaming.AnnotationStreamAccount:          "acct",
				modelstreaming.AnnotationStreamDatarefsURL:      "https://x/datarefs",
				modelstreaming.AnnotationStreamAssetID:          "azureml://registries/r/models/m/versions/1",
				modelstreaming.AnnotationStreamBlobURI:          "https://acct.blob.core.windows.net/c/prefix",
				modelstreaming.AnnotationStreamIdentityClientID: "11111111-2222-3333-4444-555555555555",
				modelstreaming.AnnotationStreamTokenAudience:    "https://ai.azure.com",
			},
		},
	}
}

func TestSASBlobProvider_GetStreamingConfig(t *testing.T) {
	p := &SASBlobProvider{}
	ctx := &generator.WorkspaceGeneratorContext{Workspace: wsWithStreamAnnotations()}

	cfg, err := p.GetStreamingConfig(ctx, "microsoft/phi-4")
	assert.NoError(t, err)
	assert.Equal(t, "az://c/model", cfg.ModelPath)

	var accountVal string
	for _, e := range cfg.ProviderEnvVars {
		if e.Name == "AZURE_STORAGE_ACCOUNT_NAME" {
			accountVal = e.Value
		}
	}
	assert.Equal(t, "acct", accountVal)
	assert.Equal(t, "true", cfg.PodLabels["azure.workload.identity/use"])
	assert.Len(t, cfg.InitContainers, 1)
	assert.Len(t, cfg.Volumes, 1)

	envByName := map[string]string{}
	for _, e := range cfg.InitContainers[0].Env {
		envByName[e.Name] = e.Value
	}
	assert.Equal(t, "https://x/datarefs", envByName["STREAM_DATAREFS_URL"])
	assert.Equal(t, "azureml://registries/r/models/m/versions/1", envByName["STREAM_ASSET_ID"])
	assert.Equal(t, "https://acct.blob.core.windows.net/c/prefix", envByName["STREAM_BLOB_URI"])
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", envByName["STREAM_IDENTITY_CLIENT_ID"])
	assert.Equal(t, "https://ai.azure.com", envByName["STREAM_TOKEN_AUDIENCE"])

	ic := cfg.InitContainers[0]
	assert.Equal(t, "fetch-sas", ic.Name)
	// Slim-python init container: azure-identity is pip-installed at runtime and the
	// fetch_sas.py script is passed as an argument (env var), NOT baked into the image.
	assert.Equal(t, "python:3.12-slim", ic.Image)
	assert.Equal(t, []string{"/bin/sh", "-c", initShellCommand}, ic.Command)
	assert.Contains(t, envByName["FETCH_SAS_SCRIPT"], "WorkloadIdentityCredential",
		"the embedded fetch_sas.py script must be passed via FETCH_SAS_SCRIPT")
	assert.Equal(t, modelstreaming.SASSharedMountPath+"/"+modelstreaming.SASEnvFileName, envByName[modelstreaming.SASEnvFileEnvVar])
	// init container mounts the shared volume at the shared mount path
	assert.Len(t, ic.VolumeMounts, 1)
	assert.Equal(t, modelstreaming.SASSharedMountPath, ic.VolumeMounts[0].MountPath)

	assert.NotNil(t, cfg.Volumes[0].EmptyDir)
	assert.Equal(t, corev1.StorageMediumMemory, cfg.Volumes[0].EmptyDir.Medium)
}
