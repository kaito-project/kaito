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

package inference

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

// AzureBlobProvider implements ModelStreamer for Azure Blob Storage.
type AzureBlobProvider struct{}

func (a *AzureBlobProvider) CSIDriverName() string {
	return consts.CSIDriverNameAzureBlob
}

// GetStreamingConfig reads the PVC → PV → CSI volumeHandle to resolve
// the full az:// streaming model path and Azure-specific env vars.
//
// Azure Blob CSI volumeHandle format:
//
//	"resourceGroup#accountName#containerName##secretNamespace##secretName"
//
// Parts[1] = accountName, parts[2] = containerName.
//
// The model path is constructed as az://containerName/modelID because the
// ModelMirror download Job writes to /models/<modelID> inside the PVC,
// and the PVC is backed by a blob container — so the blob path matches the modelID.
func (a *AzureBlobProvider) GetStreamingConfig(ctx *generator.WorkspaceGeneratorContext, pvcName, pvcNamespace, modelID string) (*StreamingConfig, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := ctx.KubeClient.Get(ctx.Ctx, types.NamespacedName{
		Name: pvcName, Namespace: pvcNamespace,
	}, pvc); err != nil {
		return nil, fmt.Errorf("failed to get PVC %s/%s: %w", pvcNamespace, pvcName, err)
	}

	pvName := pvc.Spec.VolumeName
	if pvName == "" {
		return nil, fmt.Errorf("PVC %s/%s has no bound PV (spec.volumeName is empty)", pvcNamespace, pvcName)
	}

	pv := &corev1.PersistentVolume{}
	if err := ctx.KubeClient.Get(ctx.Ctx, client.ObjectKey{Name: pvName}, pv); err != nil {
		return nil, fmt.Errorf("failed to get PV %s: %w", pvName, err)
	}

	if pv.Spec.CSI == nil {
		return nil, fmt.Errorf("PV %s has no CSI spec", pvName)
	}

	if pv.Spec.CSI.Driver != a.CSIDriverName() {
		return nil, fmt.Errorf("PV %s has CSI driver %q, expected %q",
			pvName, pv.Spec.CSI.Driver, a.CSIDriverName())
	}

	parts := strings.Split(pv.Spec.CSI.VolumeHandle, "#")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected Azure Blob volumeHandle format %q (expected resourceGroup#accountName#containerName...)", pv.Spec.CSI.VolumeHandle)
	}

	accountName := parts[1]
	containerName := parts[2]

	if accountName == "" {
		return nil, fmt.Errorf("Azure Blob volumeHandle %q has empty accountName", pv.Spec.CSI.VolumeHandle)
	}
	if containerName == "" {
		return nil, fmt.Errorf("Azure Blob volumeHandle %q has empty containerName", pv.Spec.CSI.VolumeHandle)
	}

	return &StreamingConfig{
		ModelPath: fmt.Sprintf("az://%s/%s", containerName, modelID),
		ProviderEnvVars: []corev1.EnvVar{
			{Name: "AZURE_STORAGE_ACCOUNT_NAME", Value: accountName},
		},
		PodLabels: map[string]string{
			"azure.workload.identity/use": "true",
		},
	}, nil
}

// ValidateAuth resolves the streaming SA name, verifies it exists in the
// workspace namespace, and checks that it has the Azure Workload Identity client-id
// annotation required for blob storage authentication.
func (a *AzureBlobProvider) ValidateAuth(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error {
	saName, err := ResolveStreamingServiceAccount(ws, defaultSA)
	if err != nil {
		return err
	}

	sa := &corev1.ServiceAccount{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: ws.Namespace}, sa); err != nil {
		return fmt.Errorf("ServiceAccount %q not found in namespace %q: %w", saName, ws.Namespace, err)
	}

	if sa.Annotations["azure.workload.identity/client-id"] == "" {
		return fmt.Errorf("ServiceAccount %q is missing annotation azure.workload.identity/client-id; "+
			"workload identity must be configured for model streaming (see docs/proposals/20260520-model-mirror.md)",
			saName)
	}
	return nil
}
