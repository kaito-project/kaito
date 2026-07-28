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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

// fetchSASScript is the SAS-minting script, delivered to the init container via a per-workspace
// ConfigMap.
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

// initShellCommand pip-installs azure-identity and runs fetch_sas.py, which is mounted from a
// per-workspace ConfigMap at SASScriptMountPath.
const initShellCommand = "pip install --no-cache-dir -q azure-identity==" + azureIdentityVersion +
	" && python3 " + modelstreaming.SASScriptMountPath + "/" + modelstreaming.SASScriptFileName

// SASBlobProvider streams weights from a pre-existing external blob using a short-lived
// SAS token minted at pod start via Workload Identity. Configuration comes entirely from
// Workspace annotations (not a PVC).
type SASBlobProvider struct{}

// GetStreamingConfig builds the streaming configuration for the SAS path: it ensures the
// per-workspace fetch_sas.py ConfigMap exists, then wires the Workload Identity pod label, a
// SAS-fetch init container (slim-python, script mounted from the ConfigMap), and the shared
// memory-backed volume the init container writes the SAS env file to. The init container derives
// the blob URI, storage account, and model streaming URI at runtime (by resolving the model), writing
// the account and model URI into the SAS env file, so ModelPath here is a runtime shell placeholder
// the entrypoint wrapper resolves.
func (s *SASBlobProvider) GetStreamingConfig(ctx *generator.WorkspaceGeneratorContext, modelID string) (*modelstreaming.StreamingConfig, error) {
	ann := ctx.Workspace.Annotations

	if err := s.ensureScriptConfigMap(ctx); err != nil {
		return nil, err
	}

	envFilePath := modelstreaming.SASSharedMountPath + "/" + modelstreaming.SASEnvFileName
	initContainer := corev1.Container{
		Name:    modelstreaming.SASFetchInitContainerName,
		Image:   sasInitImage,
		Command: []string{"/bin/sh", "-c", initShellCommand},
		Env: []corev1.EnvVar{
			{Name: "STREAM_DATAREFS_URL", Value: ann[modelstreaming.AnnotationStreamDatarefsURL]},
			{Name: "STREAM_IDENTITY_CLIENT_ID", Value: ann[modelstreaming.AnnotationStreamIdentityClientID]},
			{Name: "STREAM_SOURCE_TYPE", Value: ann[modelstreaming.AnnotationStreamSourceType]},
			{Name: modelstreaming.SASEnvFileEnvVar, Value: envFilePath},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: modelstreaming.SASSharedVolumeName, MountPath: modelstreaming.SASSharedMountPath},
			{Name: modelstreaming.SASScriptVolumeName, MountPath: modelstreaming.SASScriptMountPath, ReadOnly: true},
		},
	}

	return &modelstreaming.StreamingConfig{
		// The model streaming URI is derived at runtime by the init container and exported by the
		// wrapper as STREAM_MODEL_URI; the command runs under /bin/sh -c so this expands at runtime.
		ModelPath: "$" + modelstreaming.SASModelURIEnvVar,
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
			{
				Name: modelstreaming.SASScriptVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: scriptConfigMapName(ctx.Workspace.Name)},
					},
				},
			},
		},
	}, nil
}

// scriptConfigMapName derives the fetch_sas.py ConfigMap name for a workspace.
func scriptConfigMapName(workspaceName string) string {
	return workspaceName + "-fetch-sas-script"
}

// ensureScriptConfigMap creates (or updates) the per-workspace ConfigMap holding fetch_sas.py in
// the workspace namespace, owned by the workspace so it is garbage-collected.
func (s *SASBlobProvider) ensureScriptConfigMap(ctx *generator.WorkspaceGeneratorContext) error {
	name := scriptConfigMapName(ctx.Workspace.Name)
	desired := map[string]string{modelstreaming.SASScriptFileName: fetchSASScript}
	ownerRef := *metav1.NewControllerRef(ctx.Workspace, v1beta1.GroupVersion.WithKind("Workspace"))

	existing := &corev1.ConfigMap{}
	err := ctx.KubeClient.Get(ctx.Ctx, types.NamespacedName{Name: name, Namespace: ctx.Workspace.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				Namespace:       ctx.Workspace.Namespace,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
			Data: desired,
		}
		if createErr := ctx.KubeClient.Create(ctx.Ctx, cm); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("failed to create fetch_sas.py ConfigMap: %w", createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get fetch_sas.py ConfigMap: %w", err)
	}
	// Update in place if the script content drifted.
	if existing.Data[modelstreaming.SASScriptFileName] == fetchSASScript {
		return nil
	}
	existing.Data = desired
	if updateErr := ctx.KubeClient.Update(ctx.Ctx, existing); updateErr != nil {
		return fmt.Errorf("failed to update fetch_sas.py ConfigMap: %w", updateErr)
	}
	return nil
}

// ValidateAuth enforces the static-mirror contract (all core SAS annotations present) and the
// Workload Identity ServiceAccount requirement.
func (s *SASBlobProvider) ValidateAuth(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error {
	if err := modelstreaming.ValidateStaticModelMirrorAnnotations(ws.Annotations); err != nil {
		return err
	}
	return ValidateStreamingServiceAccount(ctx, ws, kubeClient, defaultSA)
}
