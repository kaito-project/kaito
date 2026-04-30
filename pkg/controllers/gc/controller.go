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

package gc

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

const (
	gcInterval = 10 * time.Second
)

// LegacyNodeClaimGCController periodically scans for and deletes orphaned
// legacy gpu-provisioner NodeClaims that have been superseded by Ready
// Karpenter-managed NodeClaims for the same workspace.
type LegacyNodeClaimGCController struct {
	client   client.Client
	recorder record.EventRecorder
	interval time.Duration
}

// NewLegacyNodeClaimGCController creates a new GC controller.
func NewLegacyNodeClaimGCController(c client.Client, recorder record.EventRecorder) *LegacyNodeClaimGCController {
	return &LegacyNodeClaimGCController{
		client:   c,
		recorder: recorder,
		interval: gcInterval,
	}
}

type workspaceKey struct {
	name      string
	namespace string
}

// NeedLeaderElection implements controller-runtime's LeaderElectionRunnable
// so that the GC loop only runs on the leader replica.
func (c *LegacyNodeClaimGCController) NeedLeaderElection() bool {
	return true
}

func (c *LegacyNodeClaimGCController) Start(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	klog.InfoS("Legacy NodeClaim GC controller started", "interval", c.interval)

	for {
		select {
		case <-ctx.Done():
			klog.InfoS("Legacy NodeClaim GC controller stopped")
			return nil
		case <-ticker.C:
			if err := c.runGCCycle(ctx); err != nil {
				klog.ErrorS(err, "GC cycle failed")
			}
		}
	}
}

// runGCCycle performs one full scan: list legacy NodeClaims, group by workspace,
// check Karpenter replacements, and delete superseded legacy NodeClaims.
func (c *LegacyNodeClaimGCController) runGCCycle(ctx context.Context) error {
	// 1. List all legacy NodeClaims (labeled kaito.sh/workspace).
	legacyList := &karpenterv1.NodeClaimList{}
	if err := c.client.List(ctx, legacyList,
		client.HasLabels{kaitov1beta1.LabelWorkspaceName},
	); err != nil {
		return err
	}

	if len(legacyList.Items) == 0 {
		return nil
	}

	// 2. Group by workspace (name, namespace).
	grouped := make(map[workspaceKey][]*karpenterv1.NodeClaim)
	for i := range legacyList.Items {
		nc := &legacyList.Items[i]
		labels := nc.GetLabels()
		name := labels[kaitov1beta1.LabelWorkspaceName]
		ns := labels[kaitov1beta1.LabelWorkspaceNamespace]

		if name == "" || ns == "" {
			klog.InfoS("Legacy NodeClaim missing workspace name or namespace label, skipping",
				"nodeClaim", nc.Name,
				"labels", labels)
			continue
		}

		key := workspaceKey{name: name, namespace: ns}
		grouped[key] = append(grouped[key], nc)
	}

	// 3. For each workspace group, check Karpenter replacements and delete if ready.
	var firstErr error
	for wsKey, legacyNodeClaims := range grouped {
		if err := c.processWorkspace(ctx, wsKey, legacyNodeClaims); err != nil {
			klog.ErrorS(err, "Failed to process workspace for GC",
				"workspace", klog.KRef(wsKey.namespace, wsKey.name))
			if firstErr == nil {
				firstErr = err
			}
			// Continue processing other workspaces.
		}
	}

	return firstErr
}

// isWorkspaceReady returns true if the Workspace has WorkspaceSucceeded=True.
func isWorkspaceReady(ws *kaitov1beta1.Workspace) bool {
	for _, c := range ws.Status.Conditions {
		if c.Type == string(kaitov1beta1.WorkspaceConditionTypeSucceeded) {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// arePodsOnKarpenterNodes checks that all workspace pods are running on nodes
// backed by Karpenter NodeClaims (not legacy nodes). This prevents GC from
// deleting legacy NodeClaims while pods are still running on them.
func (c *LegacyNodeClaimGCController) arePodsOnKarpenterNodes(
	ctx context.Context,
	ws *kaitov1beta1.Workspace,
	karpenterList *karpenterv1.NodeClaimList,
) (bool, error) {
	// Set of node names from Karpenter NodeClaims.
	karpenterNodes := make(map[string]bool, len(karpenterList.Items))
	for i := range karpenterList.Items {
		if nodeName := karpenterList.Items[i].Status.NodeName; nodeName != "" {
			karpenterNodes[nodeName] = true
		}
	}

	if len(karpenterNodes) == 0 {
		return false, nil
	}

	// List pods under the workspace.
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		kaitov1beta1.LabelWorkspaceName:      ws.Name,
		kaitov1beta1.LabelWorkspaceNamespace: ws.Namespace,
	})
	if err := c.client.List(ctx, podList,
		&client.ListOptions{LabelSelector: labelSelector},
	); err != nil {
		return false, err
	}

	if len(podList.Items) == 0 {
		return false, nil
	}

	// All pods must run on Karpenter nodes.
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}
		if !karpenterNodes[pod.Spec.NodeName] {
			return false, nil
		}
	}

	return true, nil
}

// processWorkspace checks if the workspace is fully replaced by Karpenter before
// deleting legacy NodeClaims. Three conditions must hold:
//  1. All Karpenter NodeClaims are Ready and not Deleting.
//  2. The number of Karpenter NodeClaims matches the workspace's TargetNodeCount.
//  3. The workspace is Ready (WorkspaceSucceeded=True).
//
// If the workspace no longer exists, the legacy NodeClaims are definitively orphaned
// and are deleted unconditionally (provided Karpenter replacements are Ready).
func (c *LegacyNodeClaimGCController) processWorkspace(
	ctx context.Context,
	wsKey workspaceKey,
	legacyNodeClaims []*karpenterv1.NodeClaim,
) error {
	karpenterList := &karpenterv1.NodeClaimList{}
	if err := c.client.List(ctx, karpenterList,
		client.MatchingLabels{
			consts.KarpenterWorkspaceNameKey:      wsKey.name,
			consts.KarpenterWorkspaceNamespaceKey: wsKey.namespace,
		},
	); err != nil {
		return err
	}

	if len(karpenterList.Items) == 0 {
		return nil
	}

	// Condition 1: All Karpenter NodeClaims must be Ready and not Deleting.
	for i := range karpenterList.Items {
		if !nodeclaim.IsNodeClaimReadyNotDeleting(&karpenterList.Items[i]) {
			klog.V(4).InfoS("Karpenter replacement not yet ready, skipping GC",
				"workspace", klog.KRef(wsKey.namespace, wsKey.name),
				"nodeClaim", karpenterList.Items[i].Name)
			return nil
		}
	}

	// Look up the Workspace.
	ws := &kaitov1beta1.Workspace{}
	wsFound := true
	if err := c.client.Get(ctx, types.NamespacedName{
		Name:      wsKey.name,
		Namespace: wsKey.namespace,
	}, ws); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		// Workspace deleted — legacy NodeClaims are definitively orphaned.
		wsFound = false
	}

	if wsFound {
		// Condition 2: Karpenter NodeClaim count must match the target.
		if ws.Status.TargetNodeCount > 0 && int(ws.Status.TargetNodeCount) != len(karpenterList.Items) {
			klog.V(4).InfoS("Karpenter NodeClaim count does not match target, skipping GC",
				"workspace", klog.KRef(wsKey.namespace, wsKey.name),
				"karpenterCount", len(karpenterList.Items),
				"targetNodeCount", ws.Status.TargetNodeCount)
			return nil
		}
		// Condition 3: Workspace must be Ready.
		if !isWorkspaceReady(ws) {
			klog.V(4).InfoS("Workspace not yet ready, skipping GC",
				"workspace", klog.KRef(wsKey.namespace, wsKey.name))
			return nil
		}
		// Condition 4: All workspace pods must be running on Karpenter nodes.
		podsOnKarpenter, err := c.arePodsOnKarpenterNodes(ctx, ws, karpenterList)
		if err != nil {
			return err
		}
		if !podsOnKarpenter {
			klog.V(4).InfoS("Workspace pods not yet running on Karpenter nodes, skipping GC",
				"workspace", klog.KRef(wsKey.namespace, wsKey.name))
			return nil
		}
	}

	// All conditions met — delete legacy NodeClaims.
	for _, nc := range legacyNodeClaims {
		klog.V(2).InfoS("Deleting legacy NodeClaim superseded by Karpenter",
			"nodeClaim", nc.Name,
			"workspace", klog.KRef(wsKey.namespace, wsKey.name))
		if err := c.client.Delete(ctx, nc); err != nil {
			if !errors.IsNotFound(err) {
				klog.ErrorS(err, "Failed to delete legacy NodeClaim",
					"nodeClaim", nc.Name)
			}
			continue
		}
		if wsFound {
			c.recorder.Eventf(ws, "Normal", "LegacyNodeClaimGC",
				"Deleted legacy NodeClaim %s superseded by Karpenter", nc.Name)
		}
	}

	return nil
}
