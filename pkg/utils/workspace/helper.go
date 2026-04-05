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

package workspace

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	estimatorpkg "github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// NodeEstimateRequestFromWorkspace builds a NodeEstimateRequest from a Workspace object.
// It fetches the HuggingFace access token from Kubernetes when ModelAccessSecret is set,
// so the returned request carries the resolved token value rather than a secret reference.
// This is a convenience helper for callers that already have a Workspace and a kube client.
func NodeEstimateRequestFromWorkspace(ctx context.Context, w *kaitov1beta1.Workspace, kubeClient client.Client) (estimatorpkg.NodeEstimateRequest, error) {
	req := estimatorpkg.NodeEstimateRequest{
		WorkspaceName: w.Name,
		ResourceProfile: estimatorpkg.ResourceProfile{
			InstanceType:                w.Resource.InstanceType,
			LabelSelector:               w.Resource.LabelSelector,
			DisableNodeAutoProvisioning: featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning],
		},
	}
	//nolint:staticcheck //SA1019: deprecate Resource.Count field
	if w.Resource.Count != nil {
		//nolint:staticcheck //SA1019: deprecate Resource.Count field
		req.ResourceProfile.RequestedNodeCount = int(*w.Resource.Count)
	}
	if w.Inference != nil && w.Inference.Preset != nil {
		name := string(w.Inference.Preset.Name)
		token := ""
		// Only HuggingFace models (names containing "/") require an access token for model lookup.
		// Standard preset models use ModelAccessSecret only for runtime env-var injection,
		// which is outside the estimator's concern.
		if strings.Contains(name, "/") {
			secretName := w.Inference.Preset.PresetOptions.ModelAccessSecret
			var err error
			token, err = models.GetHFTokenFromSecret(ctx, kubeClient, secretName, w.Namespace)
			if err != nil {
				return estimatorpkg.NodeEstimateRequest{}, fmt.Errorf("failed to resolve access token for model %q: %w", name, err)
			}
		}
		req.ModelProfile = estimatorpkg.ModelProfile{
			Name:        name,
			AccessToken: token,
		}
	}
	return req, nil
}
