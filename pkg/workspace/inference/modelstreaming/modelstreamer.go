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
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

// StreamingConfig holds the resolved streaming configuration for an inference pod.
// Returned by ModelStreamer.GetStreamingConfig — callers use the fields directly
// without needing to know provider-specific details.
type StreamingConfig struct {
	// ModelPath is the full streaming URI (e.g. "az://container/modelID").
	ModelPath string
	// ProviderEnvVars are provider-specific env vars for the streaming runtime
	// (e.g. AZURE_STORAGE_ACCOUNT_NAME for Azure, AWS_DEFAULT_REGION for S3).
	ProviderEnvVars []corev1.EnvVar
	// PodLabels are provider-specific labels to add to the pod template
	// (e.g. azure.workload.identity/use for Azure WI; empty for AWS/GCP).
	PodLabels map[string]string
	// InitContainers are provider-contributed init containers (e.g. a SAS-token
	// fetcher for the SAS blob path). Empty for the PVC/mirror path.
	InitContainers []corev1.Container
	// Volumes are provider-contributed pod volumes (e.g. the shared emptyDir the
	// SAS token is written to). Empty for the PVC/mirror path.
	Volumes []corev1.Volume
}

// ModelStreamer abstracts provider-specific logic for resolving streaming configuration
// from a PVC/PV. To add support for a new cloud provider (e.g. S3, GCS):
//  1. Create a new file streaming_provider_<cloud>.go
//  2. Implement the ModelStreamer interface
//  3. Add a case to GetModelStreamer() and consts.CSIDriverNameForCloud()
type ModelStreamer interface {
	// GetStreamingConfig resolves the full streaming model path, provider-specific
	// env vars, pod labels, init containers, and volumes for the given modelID.
	// Provider implementations may read from PVC/PV (WIBlobProvider) or from
	// Workspace annotations (SASBlobProvider).
	GetStreamingConfig(ctx *generator.WorkspaceGeneratorContext, modelID string) (*StreamingConfig, error)

	// ValidateAuth resolves the streaming identity (e.g. ServiceAccount), verifies it exists,
	// and checks provider-specific auth configuration (e.g. Azure WI annotation, AWS IAM role).
	// Returns nil if valid, error with actionable message if not.
	ValidateAuth(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error
}
