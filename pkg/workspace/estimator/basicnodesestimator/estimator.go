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

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/resource"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

// BasicNodesEstimator calculates node count based on SKU memory and model memory requirement
type BasicNodesEstimator struct {
	// no fields
}

func (e *BasicNodesEstimator) Name() string {
	return "basic"
}

func (e *BasicNodesEstimator) EstimateNodeCount(ctx context.Context, wObj *kaitov1beta1.Workspace) (int32, error) {
	// If inference is not configured, default to resource count or 1
	if wObj.Inference == nil || wObj.Inference.Preset == nil || wObj.Inference.Preset.Name == "" {
		//nolint:staticcheck //SA1019: deprecate Resource.Count field
		if wObj.Resource.Count != nil {
			//nolint:staticcheck //SA1019: deprecate Resource.Count field
			return int32(*wObj.Resource.Count), nil
		}
		return 1, nil
	}

	presetName := string(wObj.Inference.Preset.Name)
	model := plugin.KaitoModelRegister.MustGet(presetName)

	gpuConfig, err := utils.GetGPUConfigBySKU(wObj.Resource.InstanceType)
	if err != nil {
		return 0, fmt.Errorf("failed to get GPU config for instance type %s: %w", wObj.Resource.InstanceType, err)
	}
	if gpuConfig == nil {
		return 0, fmt.Errorf("GPU config is nil for instance type %s", wObj.Resource.InstanceType)
	}

	// Start with the user-requested node count (default is 1)
	//nolint:staticcheck //SA1019: deprecate Resource.Count field
	nodeCountPerReplica := lo.FromPtr(wObj.Resource.Count)

	// If GPU memory information is available, calculate the optimal node count
	if gpuConfig.GPUMemGB > 0 && gpuConfig.GPUCount > 0 {
		totalGPUMemoryRequired := resource.MustParse(model.GetInferenceParameters().TotalGPUMemoryRequirement)
		totalGPUMemoryPerNodeBytes := int64(gpuConfig.GPUMemGB) * consts.GiBToBytes

		requiredMemoryBytes := totalGPUMemoryRequired.Value()

		// Calculate minimum nodes needed using ceiling division: (a + b - 1) / b
		minimumNodes := int((requiredMemoryBytes + totalGPUMemoryPerNodeBytes - 1) / totalGPUMemoryPerNodeBytes)

		if minimumNodes < nodeCountPerReplica {
			nodeCountPerReplica = minimumNodes
		}
	}

	return int32(nodeCountPerReplica), nil
}
