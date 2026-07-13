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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

// SASBlobProvider streams weights from a pre-existing external blob using a short-lived
// SAS token minted at pod start via Workload Identity. Configuration comes entirely from
// Workspace annotations (not a PVC).
type SASBlobProvider struct{}

// GetStreamingConfig builds the streaming configuration from the five stream-* annotations
// on the workspace: the az:// model path, the storage-account env var, the Workload
// Identity pod label, a SAS-fetch init container, and the shared memory-backed volume the
// token is written to.
func (s *SASBlobProvider) GetStreamingConfig(ctx *generator.WorkspaceGeneratorContext, modelID string) (*modelstreaming.StreamingConfig, error) {
	ann := ctx.Workspace.Annotations

	// Image is intentionally left unset here: the init container must run the SAME image
	// as the main inference container (so fetch_sas.py + azure-identity are present), and
	// that image is only resolved during pod generation. SetStreamingConfig sets it.
	initContainer := corev1.Container{
		Name:    modelstreaming.SASFetchInitContainerName,
		Command: []string{"python3", "/workspace/vllm/fetch_sas.py"},
		Env: []corev1.EnvVar{
			{Name: "STREAM_DATAREFS_URL", Value: ann[modelstreaming.AnnotationStreamDatarefsURL]},
			{Name: "STREAM_ASSET_ID", Value: ann[modelstreaming.AnnotationStreamAssetID]},
			{Name: "STREAM_BLOB_URI", Value: ann[modelstreaming.AnnotationStreamBlobURI]},
			{Name: "SAS_TOKEN_PATH", Value: modelstreaming.SASSharedMountPath + "/" + modelstreaming.SASTokenFileName},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: modelstreaming.SASSharedVolumeName, MountPath: modelstreaming.SASSharedMountPath},
		},
	}

	return &modelstreaming.StreamingConfig{
		ModelPath: ann[modelstreaming.AnnotationStreamURI],
		ProviderEnvVars: []corev1.EnvVar{
			{Name: "AZURE_STORAGE_ACCOUNT_NAME", Value: ann[modelstreaming.AnnotationStreamAccount]},
		},
		PodLabels: map[string]string{
			"azure.workload.identity/use": "true",
		},
		InitContainers: []corev1.Container{initContainer},
		Volumes: []corev1.Volume{
			{
				Name: modelstreaming.SASSharedVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
				},
			},
		},
	}, nil
}

// ValidateAuth enforces the no-fallback contract (all five annotations present) and the
// Workload Identity ServiceAccount requirement.
func (s *SASBlobProvider) ValidateAuth(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error {
	if err := modelstreaming.RequireSASBlobStreamingAnnotations(ws.Annotations); err != nil {
		return err
	}
	return modelstreaming.ValidateStreamingServiceAccount(ctx, ws, kubeClient, defaultSA)
}
