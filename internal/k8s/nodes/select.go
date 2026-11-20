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

package nodes

import (
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// SelectNodes sorts qualified nodes by preferred, previously used, and
// provisioner-created, then returns the first count.
func SelectNodes(qualified []*corev1.Node, preferred []string, previous []string, count int) []*corev1.Node {
	priority := func(node *corev1.Node) int {
		if slices.Contains(preferred, node.Name) {
			return 4
		}
		if slices.Contains(previous, node.Name) {
			return 3
		}
		if _, createdByGPUProvisioner := node.Labels[labelGPUProvisionerCustom]; createdByGPUProvisioner {
			return 2
		}
		if _, createdByKarpenter := node.Labels[karpenterv1.NodePoolLabelKey]; createdByKarpenter {
			return 1
		}
		return 0
	}

	sort.Slice(qualified, func(i, j int) bool {
		iPriority := priority(qualified[i])
		jPriority := priority(qualified[j])
		if iPriority != jPriority {
			return iPriority > jPriority
		}
		return qualified[i].Name < qualified[j].Name
	})

	if len(qualified) <= count {
		return qualified
	}

	return qualified[0:count]
}
