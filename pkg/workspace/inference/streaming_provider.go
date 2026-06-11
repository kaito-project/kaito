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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

// StreamingConfig holds the resolved streaming configuration for an inference pod.
// Returned by CloudProvider.ResolveStreamingConfig — callers use the fields directly
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
}

// CloudProvider abstracts provider-specific logic for resolving streaming configuration
// from a PVC/PV. To add support for a new cloud provider (e.g. S3, GCS):
//  1. Create a new file streaming_provider_<cloud>.go
//  2. Implement the CloudProvider interface
//  3. Add a case to GetCloudProvider() and consts.CSIDriverNameForCloud()
type CloudProvider interface {
	// ResolveStreamingConfig reads the PVC and its backing PV to resolve
	// the full streaming model path, provider-specific env vars, and pod labels.
	ResolveStreamingConfig(ctx *generator.WorkspaceGeneratorContext, pvcName, pvcNamespace, modelID string) (*StreamingConfig, error)

	// CSIDriverName returns the expected CSI driver name for validation.
	CSIDriverName() string

	// ValidateServiceAccount checks that the SA has the required provider-specific
	// configuration (e.g. Azure WI client-id annotation, AWS IAM role-arn annotation).
	// Returns nil if valid, error with actionable message if not.
	ValidateServiceAccount(sa *corev1.ServiceAccount) error
}

// GetCloudProvider returns the CloudProvider implementation for the given cloud.
// Currently only Azure is supported. To add a new provider, add a case here
// and in consts.CSIDriverNameForCloud().
func GetCloudProvider(cloudName string) (CloudProvider, error) {
	switch cloudName {
	case consts.AzureCloudName, "":
		return &AzureBlobProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported cloud provider %q for model streaming; supported: azure", cloudName)
	}
}
