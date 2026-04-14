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

package manager

import (
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	gpuprovisioner "github.com/kaito-project/kaito/pkg/nodeprovision/gpu-provisioner"
	noprovisioner "github.com/kaito-project/kaito/pkg/nodeprovision/no-provisioner"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

// NewNodeProvisioner creates and returns a NodeProvisioner based on feature gates.
//
//   - NAP disabled (BYO mode): NopProvisioner (all provisioning ops are no-ops).
//   - Default (NAP enabled): AzureGPUProvisioner (creates/deletes NodeClaims).
func NewNodeProvisioner(kClient client.Client, recorder record.EventRecorder, defaultNodeImageFamily string) nodeprovision.NodeProvisioner {
	if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		return noprovisioner.NewNopProvisioner(kClient)
	}
	expectations := utils.NewControllerExpectations()
	ncm := resource.NewNodeClaimManager(kClient, recorder, expectations)
	ncm.SetDefaultNodeImageFamily(defaultNodeImageFamily)
	nm := resource.NewNodeManager(kClient)
	return gpuprovisioner.NewAzureGPUProvisioner(ncm, nm)
}
