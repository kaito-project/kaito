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

package basicnodesestimator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	estimator "github.com/kaito-project/kaito/pkg/workspace/estimator"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// BasicNodesEstimator calculates node count based on SKU memory and model memory requirement
type BasicNodesEstimator struct {
	// no fields
}

func (e *BasicNodesEstimator) Name() string {
	return "basic"
}

func (e *BasicNodesEstimator) EstimateNodeCount(ctx context.Context, req estimator.NodeEstimateRequest, c client.Client) (int32, error) {
	// If no preset is configured, default to the requested node count or 1.
	if req.ModelProfile.Name == "" {
		if req.ResourceProfile.RequestedNodeCount > 0 {
			return int32(req.ResourceProfile.RequestedNodeCount), nil
		}
		return 1, nil
	}

	model, err := models.GetModelByNameWithToken(ctx, req.ModelProfile.Name, req.ModelProfile.AccessToken)
	if err != nil {
		return 0, fmt.Errorf("failed to get model by name: %w", err)
	}

	var gpuConfig *sku.GPUConfig

	if req.ResourceProfile.DisableNodeAutoProvisioning {
		// NAP is disabled (BYO scenario) — derive GPU config from existing ready nodes.
		var matchLabels client.MatchingLabels
		if req.ResourceProfile.LabelSelector != nil {
			matchLabels = req.ResourceProfile.LabelSelector.MatchLabels
		}
		nodeList, listErr := resources.ListNodes(ctx, c, matchLabels)
		if listErr != nil {
			return 0, fmt.Errorf("failed to list ready nodes: %w", listErr)
		}
		var readyNodes []*corev1.Node
		for i := range nodeList.Items {
			if resources.NodeIsReadyAndNotDeleting(&nodeList.Items[i]) {
				readyNodes = append(readyNodes, &nodeList.Items[i])
			}
		}
		if len(readyNodes) == 0 {
			return 0, fmt.Errorf("no ready nodes found, unable to determine GPU configuration")
		}
		gpuConfig, err = utils.GetGPUConfigFromNodeLabels(readyNodes[0])
		if err != nil {
			return 0, fmt.Errorf("failed to get GPU config from existing nodes: %w", err)
		}
	} else {
		// NAP is enabled — instanceType is required and must be valid.
		gpuConfig, err = utils.GetGPUConfigBySKU(req.ResourceProfile.InstanceType)
		if err != nil {
			return 0, fmt.Errorf("failed to get GPU config for instance type %s: %w", req.ResourceProfile.InstanceType, err)
		}
	}

	// Start with the user-requested node count.
	nodeCountPerReplica := req.ResourceProfile.RequestedNodeCount

	// If GPU memory information is available, calculate the optimal node count.
	if !gpuConfig.GPUMem.IsZero() && gpuConfig.GPUCount > 0 {
		totalGPUMemoryRequired := resource.MustParse(model.GetInferenceParameters().TotalSafeTensorFileSize)
		totalGPUMemoryPerNodeBytes := gpuConfig.GPUMem.Value()

		requiredMemoryBytes := totalGPUMemoryRequired.Value()

		// Calculate minimum nodes needed using ceiling division: (a + b - 1) / b
		minimumNodes := int((requiredMemoryBytes + totalGPUMemoryPerNodeBytes - 1) / totalGPUMemoryPerNodeBytes)

		if minimumNodes < nodeCountPerReplica {
			nodeCountPerReplica = minimumNodes
		}
	}

	return int32(nodeCountPerReplica), nil
}
