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

package featuregates

import "slices"

// Node provisioner types.
const (
	NodeProvisionerAzureGPU  = "azure-gpu-provisioner"
	NodeProvisionerKarpenter = "karpenter"
	NodeProvisionerBYO       = "byo"
)

// NodeProvisioner holds the resolved provisioner type at runtime.
// Set once during startup in main.go; read by admission validation and
// controllers for provisioner-specific behavior.
var NodeProvisioner string

// IsKarpenterProvisioner returns true if the active node provisioner is karpenter.
func IsKarpenterProvisioner() bool {
	return NodeProvisioner == NodeProvisionerKarpenter
}

// allowedNodeClassNames is the sorted set of NodeClass names declared via
// --karpenter-node-classes. Set once during startup; read by the Workspace
// admission webhook to validate the node-class-name annotation. Unexported so callers
// cannot mutate the allowlist in place.
var allowedNodeClassNames []string

// SetAllowedNodeClassNames publishes the NodeClass allowlist. Startup only.
func SetAllowedNodeClassNames(names []string) {
	allowedNodeClassNames = slices.Clone(names)
}

// AllowedNodeClassNames returns the NodeClass allowlist.
func AllowedNodeClassNames() []string {
	return slices.Clone(allowedNodeClassNames)
}
