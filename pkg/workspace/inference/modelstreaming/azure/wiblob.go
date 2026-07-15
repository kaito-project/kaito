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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

// WIBlobProvider streams model weights from an Azure Blob container that KAITO's model
// mirror downloaded into (a PVC-backed blob KAITO owns). The inference pod authenticates via
// Azure Workload Identity (AAD)
type WIBlobProvider struct{}

func (a *WIBlobProvider) CSIDriverName() string {
	return consts.CSIDriverNameAzureBlob
}

// GetStreamingConfig derives the ModelMirror CR name from modelID, fetches the CR
// to get the PVC namespace, then reads the PVC → PV → CSI volumeHandle to resolve
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
func (a *WIBlobProvider) GetStreamingConfig(ctx *generator.WorkspaceGeneratorContext, modelID string) (*modelstreaming.StreamingConfig, error) {
	crName := modelstreaming.ModelMirrorCRName(modelID)
	mmCR := &kaitov1alpha1.ModelMirror{}
	if err := ctx.KubeClient.Get(ctx.Ctx, client.ObjectKey{Name: crName}, mmCR); err != nil {
		return nil, fmt.Errorf("failed to get ModelMirror CR %s for streaming config: %w", crName, err)
	}
	pvcName := crName
	pvcNamespace := mmCR.Spec.JobNamespace

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

	return &modelstreaming.StreamingConfig{
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
func (a *WIBlobProvider) ValidateAuth(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error {
	return ValidateStreamingServiceAccount(ctx, ws, kubeClient, defaultSA)
}
