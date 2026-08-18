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
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/nodeprovision"
	byoprovisioner "github.com/kaito-project/kaito/pkg/nodeprovision/byo-provisioner"
	gpuprovisioner "github.com/kaito-project/kaito/pkg/nodeprovision/gpu-provisioner"
	karpenterprov "github.com/kaito-project/kaito/pkg/nodeprovision/karpenter"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/workspace/resource"
)

// ProvisionerConfig holds all parameters needed to create a NodeProvisioner.
type ProvisionerConfig struct {
	KClient                client.Client
	DirectClient           client.Client
	Recorder               record.EventRecorder
	DefaultNodeImageFamily string
	ProvisionerType        string
	NodeClassGroup         string
	NodeClassKind          string
	NodeClassVersion       string
	NodeClassResourceName  string
	NodeClasses            []karpenterprov.NodeClassSpec
	NodeClassNames         []string
}

// ParseNodeClasses decodes the --karpenter-node-classes JSON array and validates it,
// returning the entries and their names, both sorted by name.
func ParseNodeClasses(raw string) ([]karpenterprov.NodeClassSpec, []string, error) {
	if raw == "" {
		return nil, nil, fmt.Errorf("--karpenter-node-classes is required when node-provisioner=%s", consts.NodeProvisionerKarpenter)
	}

	var entries []karpenterprov.NodeClassSpec
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, nil, fmt.Errorf("parsing --karpenter-node-classes as a JSON array: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("--karpenter-node-classes declares no NodeClasses")
	}

	seen := make(map[string]struct{}, len(entries))
	names := make([]string, 0, len(entries))
	defaults := make([]string, 0, 1)
	for i, e := range entries {
		if e.Name == "" {
			return nil, nil, fmt.Errorf("--karpenter-node-classes entry %d has an empty name", i)
		}
		if errs := validation.IsDNS1123Subdomain(e.Name); len(errs) > 0 {
			return nil, nil, fmt.Errorf("--karpenter-node-classes entry %d name %q is not a valid resource name: %s", i, e.Name, errs[0])
		}
		if _, dup := seen[e.Name]; dup {
			return nil, nil, fmt.Errorf("--karpenter-node-classes declares %q more than once", e.Name)
		}
		seen[e.Name] = struct{}{}
		names = append(names, e.Name)
		if e.Default {
			defaults = append(defaults, e.Name)
		}
	}
	if len(defaults) != 1 {
		return nil, nil, fmt.Errorf("--karpenter-node-classes must mark exactly one entry default, got %d %v", len(defaults), defaults)
	}

	slices.SortFunc(entries, func(a, b karpenterprov.NodeClassSpec) int {
		return strings.Compare(a.Name, b.Name)
	})
	slices.Sort(names)
	return entries, names, nil
}

// NewNodeProvisioner creates and returns a NodeProvisioner based on the provisionerType parameter.
//
//   - karpenter: KarpenterProvisioner (cloud-agnostic karpenter NodePool CRUD).
//   - byo: BYOProvisioner (all provisioning ops are no-ops).
//   - azure-gpu-provisioner (default): AzureGPUProvisioner (creates/deletes NodeClaims).
func NewNodeProvisioner(cfg ProvisionerConfig) nodeprovision.NodeProvisioner {
	switch cfg.ProvisionerType {
	case consts.NodeProvisionerKarpenter:
		ncCfg := karpenterprov.NodeClassConfig{
			Group:          cfg.NodeClassGroup,
			Kind:           cfg.NodeClassKind,
			Version:        cfg.NodeClassVersion,
			ResourceName:   cfg.NodeClassResourceName,
			NodeClasses:    cfg.NodeClasses,
			NodeClassNames: cfg.NodeClassNames,
		}
		return karpenterprov.NewKarpenterProvisioner(cfg.DirectClient, ncCfg)
	case consts.NodeProvisionerBYO:
		return byoprovisioner.NewBYOProvisioner(cfg.KClient)
	default: // consts.NodeProvisionerAzureGPU
		expectations := utils.NewControllerExpectations()
		ncm := resource.NewNodeClaimManager(cfg.KClient, cfg.Recorder, expectations)
		ncm.SetDefaultNodeImageFamily(cfg.DefaultNodeImageFamily)
		nm := resource.NewNodeManager(cfg.KClient)
		return gpuprovisioner.NewAzureGPUProvisioner(ncm, nm)
	}
}
