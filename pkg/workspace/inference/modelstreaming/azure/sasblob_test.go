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
				modelstreaming.AnnotationStreamURI:         "az://c/model",
				modelstreaming.AnnotationStreamAccount:     "acct",
				modelstreaming.AnnotationStreamDatarefsURL: "https://x/datarefs",
				modelstreaming.AnnotationStreamAssetID:     "azureml://registries/r/models/m/versions/1",
				modelstreaming.AnnotationStreamBlobURI:     "https://acct.blob.core.windows.net/c/prefix",
			},
		},
	}
}

func TestSelectModelStreamer(t *testing.T) {
	ws := wsWithStreamAnnotations()
	_, ok := SelectModelStreamer(ws).(*SASBlobProvider)
	assert.True(t, ok, "expected SASBlobProvider when all five annotations present")

	modelstreaming.StreamingDefaults.ModelStreamer = &AzureBlobProvider{}
	plain := &v1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "w", Namespace: "default"}}
	_, ok = SelectModelStreamer(plain).(*AzureBlobProvider)
	assert.True(t, ok, "expected default provider when no stream annotations")
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

	ic := cfg.InitContainers[0]
	assert.Equal(t, "fetch-sas", ic.Name)
	assert.Equal(t, []string{"python3", "/workspace/vllm/fetch_sas.py"}, ic.Command)
	// Image is intentionally unset here; it is resolved during pod generation to match
	// the inference container image.
	assert.Equal(t, "", ic.Image)
	assert.Equal(t, "/streaming/sas_token", envByName["SAS_TOKEN_PATH"])
	// init container mounts the shared volume at /streaming
	assert.Len(t, ic.VolumeMounts, 1)
	assert.Equal(t, "/streaming", ic.VolumeMounts[0].MountPath)

	assert.NotNil(t, cfg.Volumes[0].EmptyDir)
	assert.Equal(t, corev1.StorageMediumMemory, cfg.Volumes[0].EmptyDir.Medium)
}
