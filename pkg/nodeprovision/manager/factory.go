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

	"github.com/kaito-project/kaito/pkg/nodeprovision"
	azurekarpenter "github.com/kaito-project/kaito/pkg/nodeprovision/azure-karpenter"
	byoprovisioner "github.com/kaito-project/kaito/pkg/nodeprovision/byo-provisioner"
	gpuprovisioner "github.com/kaito-project/kaito/pkg/nodeprovision/gpu-provisioner"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

// NewNodeProvisioner creates and returns a NodeProvisioner based on the provisionerType parameter.
//
//   - azure-karpenter: AzureKarpenterProvisioner (uses directClient for
//     CRD verification and global AKSNodeClass bootstrap at Start time).
//   - byo: BYOProvisioner (all provisioning ops are no-ops).
//   - azure-gpu-provisioner (default): AzureGPUProvisioner (creates/deletes NodeClaims).
func NewNodeProvisioner(kClient, directClient client.Client, recorder record.EventRecorder, defaultNodeImageFamily string, provisionerType string) nodeprovision.NodeProvisioner {
	switch provisionerType {
	case consts.NodeProvisionerAzureKarpenter:
		return azurekarpenter.NewAzureKarpenterProvisioner(directClient)
	case consts.NodeProvisionerBYO:
		return byoprovisioner.NewBYOProvisioner(kClient)
	default: // consts.NodeProvisionerAzureGPU
		expectations := utils.NewControllerExpectations()
		ncm := resource.NewNodeClaimManager(kClient, recorder, expectations)
		ncm.SetDefaultNodeImageFamily(defaultNodeImageFamily)
		nm := resource.NewNodeManager(kClient)
		return gpuprovisioner.NewAzureGPUProvisioner(ncm, nm)
	}
}
