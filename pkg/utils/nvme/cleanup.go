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

package nvme

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// CleanupStaleLocalNVMePVCs deletes PVCs backed by local NVMe storage whose PV has
// node affinity to a node that no longer exists. This unblocks the StatefulSet from
// recreating the pod with a fresh PVC on the replacement node after drift.
// Returns the number of PVCs deleted.
func CleanupStaleLocalNVMePVCs(ctx context.Context, kubeClient client.Client, namespace, workspaceName string) (int, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := kubeClient.List(ctx, pvcList,
		client.InNamespace(namespace),
		client.MatchingLabels{kaitov1beta1.LabelWorkspaceName: workspaceName},
	); err != nil {
		return 0, fmt.Errorf("listing PVCs: %w", err)
	}

	deleted := 0
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]

		// Only handle PVCs using local NVMe storage.
		if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != consts.LocalNVMeStorageClass {
			continue
		}
		// Only handle Bound PVCs (they have a backing PV).
		if pvc.Status.Phase != corev1.ClaimBound {
			continue
		}

		// Get the backing PV and check its node affinity.
		pv := &corev1.PersistentVolume{}
		if err := kubeClient.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, pv); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return deleted, fmt.Errorf("getting PV %s: %w", pvc.Spec.VolumeName, err)
		}

		nodeName := NodeNameFromPVAffinity(pv)
		if nodeName == "" {
			continue
		}

		// Check if the node still exists.
		node := &corev1.Node{}
		if err := kubeClient.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
			if !errors.IsNotFound(err) {
				return deleted, fmt.Errorf("checking node %s: %w", nodeName, err)
			}
			// Node is gone — delete the stale PVC.
			klog.V(2).InfoS("Deleting stale local NVMe PVC bound to non-existent node",
				"pvc", klog.KRef(namespace, pvc.Name), "node", nodeName, "pv", pv.Name)
			if err := kubeClient.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
				return deleted, fmt.Errorf("deleting PVC %s/%s: %w", namespace, pvc.Name, err)
			}
			deleted++
		}
	}
	return deleted, nil
}

// NodeNameFromPVAffinity extracts the node name from a PV's node affinity.
// Returns empty string if the affinity doesn't pin to a single node.
func NodeNameFromPVAffinity(pv *corev1.PersistentVolume) string {
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return ""
	}
	for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
		for _, expr := range term.MatchExpressions {
			if expr.Operator == corev1.NodeSelectorOpIn && len(expr.Values) == 1 {
				return expr.Values[0]
			}
		}
	}
	return ""
}
