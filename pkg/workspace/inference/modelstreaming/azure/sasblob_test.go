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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
				modelstreaming.AnnotationStreamDatarefsURL:      "https://x.services.ai.azure.com/api/projects/p/models/m/versions/1/credentials?api-version=2025-11-15-preview",
				modelstreaming.AnnotationStreamIdentityClientID: "11111111-2222-3333-4444-555555555555",
				modelstreaming.AnnotationStreamSourceType:       modelstreaming.SourceTypeBYO,
			},
		},
	}
}

func newSASTestContext(ws *v1beta1.Workspace) *generator.WorkspaceGeneratorContext {
	scheme := runtime.NewScheme()
	_ = v1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ws).Build()
	return &generator.WorkspaceGeneratorContext{Ctx: context.Background(), Workspace: ws, KubeClient: c}
}

func TestSASBlobProvider_GetStreamingConfig(t *testing.T) {
	p := &SASBlobProvider{}
	ws := wsWithStreamAnnotations()
	ctx := newSASTestContext(ws)

	cfg, err := p.GetStreamingConfig(ctx, "microsoft/phi-4")
	assert.NoError(t, err)

	// ModelPath is a runtime shell placeholder; the init container derives the real az:// URI.
	assert.Equal(t, "$"+modelstreaming.SASModelURIEnvVar, cfg.ModelPath)
	// AZURE_STORAGE_ACCOUNT_NAME is no longer a pod-spec env var (derived at runtime).
	assert.Empty(t, cfg.ProviderEnvVars)
	assert.Equal(t, "true", cfg.PodLabels["azure.workload.identity/use"])
	assert.Len(t, cfg.InitContainers, 1)
	assert.Len(t, cfg.Volumes, 2) // shared emptyDir + script ConfigMap

	envByName := map[string]string{}
	for _, e := range cfg.InitContainers[0].Env {
		envByName[e.Name] = e.Value
	}
	// Only the three required inputs (+ env-file path) are passed; blob-derived values are gone.
	assert.Equal(t, ws.Annotations[modelstreaming.AnnotationStreamDatarefsURL], envByName["STREAM_DATAREFS_URL"])
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", envByName["STREAM_IDENTITY_CLIENT_ID"])
	assert.Equal(t, modelstreaming.SourceTypeBYO, envByName["STREAM_SOURCE_TYPE"])
	assert.NotContains(t, envByName, "STREAM_BLOB_URI")
	assert.NotContains(t, envByName, "STREAM_ASSET_ID")
	assert.NotContains(t, envByName, "STREAM_TOKEN_AUDIENCE")
	assert.NotContains(t, envByName, "FETCH_SAS_SCRIPT") // script now via ConfigMap, not env
	assert.Equal(t, modelstreaming.SASSharedMountPath+"/"+modelstreaming.SASEnvFileName, envByName[modelstreaming.SASEnvFileEnvVar])

	ic := cfg.InitContainers[0]
	assert.Equal(t, "fetch-sas", ic.Name)
	assert.Equal(t, "python:3.12-slim", ic.Image)
	assert.Equal(t, []string{"/bin/sh", "-c", initShellCommand}, ic.Command)
	// init container mounts both the shared volume and the script ConfigMap.
	assert.Len(t, ic.VolumeMounts, 2)
	mountByName := map[string]string{}
	for _, m := range ic.VolumeMounts {
		mountByName[m.Name] = m.MountPath
	}
	assert.Equal(t, modelstreaming.SASSharedMountPath, mountByName[modelstreaming.SASSharedVolumeName])
	assert.Equal(t, modelstreaming.SASScriptMountPath, mountByName[modelstreaming.SASScriptVolumeName])

	// The shared volume is a memory-backed emptyDir.
	var sharedVol *corev1.Volume
	for i := range cfg.Volumes {
		if cfg.Volumes[i].Name == modelstreaming.SASSharedVolumeName {
			sharedVol = &cfg.Volumes[i]
		}
	}
	assert.NotNil(t, sharedVol)
	assert.NotNil(t, sharedVol.EmptyDir)
	assert.Equal(t, corev1.StorageMediumMemory, sharedVol.EmptyDir.Medium)

	// The script ConfigMap was created in the workspace namespace with the script content.
	cm := &corev1.ConfigMap{}
	err = ctx.KubeClient.Get(ctx.Ctx, types.NamespacedName{Name: scriptConfigMapName(ws.Name), Namespace: ws.Namespace}, cm)
	assert.NoError(t, err)
	assert.Contains(t, cm.Data[modelstreaming.SASScriptFileName], "WorkloadIdentityCredential")

	// Idempotent: a second call updates rather than errors.
	_, err = p.GetStreamingConfig(ctx, "microsoft/phi-4")
	assert.NoError(t, err)
}
