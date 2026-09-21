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
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
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
	if w.Resource.Partition != nil {
		switch w.Resource.Partition.Mode {
		case kaitov1beta1.PartitionModeMIG:
			req.ResourceProfile.MIGProfile = w.Resource.Partition.Profile
		case kaitov1beta1.PartitionModeAccelerator:
			req.ResourceProfile.AcceleratorCount = *w.Resource.Partition.Count
		}
	}
	if w.Inference != nil && w.Inference.Preset != nil && w.Inference.Preset.Name != "" {
		m, err := resolveModelForEstimation(ctx, w, kubeClient)
		if err != nil {
			return estimatorpkg.NodeEstimateRequest{}, err
		}
		req.ModelProfile = estimatorpkg.ModelProfile{Model: m}
	}
	return req, nil
}

// resolveModelForEstimation resolves the model the estimator will size. A
// bring-your-own model comes from its configuration ConfigMap; every other
// model is looked up by name, resolving a HuggingFace access token first when
// the name is a HuggingFace card ID. Resolution happens here rather than in the
// estimator so that the estimator stays independent of where the model is read
// from.
func resolveModelForEstimation(ctx context.Context, w *kaitov1beta1.Workspace, kubeClient client.Client) (pkgmodel.Model, error) {
	name := string(w.Inference.Preset.Name)
	if plugin.IsCustomPreset(name) {
		resolved, err := models.ResolveCustomModel(ctx, kubeClient, w.Inference.Config, w.Namespace)
		if err != nil {
			return nil, err
		}
		return resolved.Model, nil
	}

	token := ""
	// Only HuggingFace models (names containing "/") require an access token for model lookup.
	// Standard preset models use ModelAccessSecret only for runtime env-var injection,
	// which is outside the estimator's concern.
	if strings.Contains(name, "/") {
		var err error
		token, err = models.GetHFTokenFromSecret(ctx, kubeClient, w.Inference.Preset.PresetOptions.ModelAccessSecret, w.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve access token for model %q: %w", name, err)
		}
	}
	return models.GetModelByNameWithToken(ctx, name, token)
}
