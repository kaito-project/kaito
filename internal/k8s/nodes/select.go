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
	sort.Slice(qualified, func(i, j int) bool {
		iPreferred := slices.Contains(preferred, qualified[i].Name)
		jPreferred := slices.Contains(preferred, qualified[j].Name)

		if iPreferred && !jPreferred {
			return true
		} else if !iPreferred && jPreferred {
			return false
		} else { // either all are preferred, or none is preferred
			iPrevious := slices.Contains(previous, qualified[i].Name)
			jPrevious := slices.Contains(previous, qualified[j].Name)

			if iPrevious && !jPrevious {
				return true
			} else if !iPrevious && jPrevious {
				return false
			} else { // either all are previous, or none is previous
				// Nodes created by gpu-provisioner or karpenter are a single
				// preference tier since they are more likely to be empty to use.
				// These must be collapsed into one boolean: ranking the two label
				// dimensions independently is not a valid strict weak ordering (a
				// gpu-provisioner-only node and a karpenter-only node each compare
				// as less than the other), which makes the sort non-deterministic
				// and skips the alphabetical tie-break below.
				_, iProvisioned := qualified[i].Labels[labelGPUProvisionerCustom]
				if !iProvisioned {
					_, iProvisioned = qualified[i].Labels[karpenterv1.NodePoolLabelKey]
				}
				_, jProvisioned := qualified[j].Labels[labelGPUProvisionerCustom]
				if !jProvisioned {
					_, jProvisioned = qualified[j].Labels[karpenterv1.NodePoolLabelKey]
				}

				if iProvisioned && !jProvisioned {
					return true
				} else if !iProvisioned && jProvisioned {
					return false
				} else {
					return qualified[i].Name < qualified[j].Name
				}
			}
		}
	})

	if len(qualified) <= count {
		return qualified
	}

	return qualified[0:count]
}
