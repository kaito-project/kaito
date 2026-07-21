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
	_ "embed"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

// fetchSASScript is the SAS-minting script.
//
//go:embed fetch_sas.py
var fetchSASScript string

const (
	// sasInitImage is the minimal Python image the SAS-fetch init container runs on. It carries
	// no cloud SDKs; azure-identity is pip-installed at container start (see initShellCommand).
	sasInitImage = "python:3.12-slim"
	// azureIdentityVersion is the azure-identity version pip-installed at runtime by the init
	// container. Kept in sync with the pin previously in the base image requirements.txt.
	azureIdentityVersion = "1.19.0"
)

// initShellCommand pip-installs azure-identity and runs the embedded fetch_sas.py script,
// which is provided via the FETCH_SAS_SCRIPT env var (not a file in the image).
const initShellCommand = "pip install --no-cache-dir -q azure-identity==" + azureIdentityVersion +
	` && python3 -c "$FETCH_SAS_SCRIPT"`

// SASBlobProvider streams weights from a pre-existing external blob using a short-lived
// SAS token minted at pod start via Workload Identity. Configuration comes entirely from
// Workspace annotations (not a PVC).
type SASBlobProvider struct{}

// GetStreamingConfig builds the streaming configuration from the five stream-* annotations
// on the workspace: the az:// model path, the storage-account env var, the Workload Identity
// pod label, a SAS-fetch init container (slim-python, script passed as an argument), and the
// shared memory-backed volume the SAS env file is written to.
func (s *SASBlobProvider) GetStreamingConfig(ctx *generator.WorkspaceGeneratorContext, modelID string) (*modelstreaming.StreamingConfig, error) {
	ann := ctx.Workspace.Annotations

	envFilePath := modelstreaming.SASSharedMountPath + "/" + modelstreaming.SASEnvFileName
	initContainer := corev1.Container{
		Name:    modelstreaming.SASFetchInitContainerName,
		Image:   sasInitImage,
		Command: []string{"/bin/sh", "-c", initShellCommand},
		Env: []corev1.EnvVar{
			// FETCH_SAS_SCRIPT carries the ENTIRE fetch_sas.py source (embedded via //go:embed)
			// as an env var, which the init container runs with `python3 -c "$FETCH_SAS_SCRIPT"`.
			// This avoids baking the script (and its azure-identity dependency) into the base
			// image. The Linux env block is capped (ARG_MAX, typically ~2 MiB total across argv +
			// env); fetch_sas.py is a few KiB, so it fits comfortably with large headroom.
			{Name: "FETCH_SAS_SCRIPT", Value: fetchSASScript},
			{Name: "STREAM_DATAREFS_URL", Value: ann[modelstreaming.AnnotationStreamDatarefsURL]},
			{Name: "STREAM_ASSET_ID", Value: ann[modelstreaming.AnnotationStreamAssetID]},
			{Name: "STREAM_BLOB_URI", Value: ann[modelstreaming.AnnotationStreamBlobURI]},
			{Name: "STREAM_IDENTITY_CLIENT_ID", Value: ann[modelstreaming.AnnotationStreamIdentityClientID]},
			{Name: "STREAM_TOKEN_AUDIENCE", Value: ann[modelstreaming.AnnotationStreamTokenAudience]},
			{Name: modelstreaming.SASEnvFileEnvVar, Value: envFilePath},
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

// ValidateAuth enforces the static-mirror contract (all core SAS annotations present) and the
// Workload Identity ServiceAccount requirement.
func (s *SASBlobProvider) ValidateAuth(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error {
	if err := modelstreaming.ValidateStaticModelMirrorAnnotations(ws.Annotations); err != nil {
		return err
	}
	return ValidateStreamingServiceAccount(ctx, ws, kubeClient, defaultSA)
}
