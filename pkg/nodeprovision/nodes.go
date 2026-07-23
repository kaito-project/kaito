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

package nodeprovision

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
)

// WorkspaceNodeSelector returns the label selector that identifies nodes
// belonging to the given Workspace: the user-supplied match labels merged with
// the provisioner's per-workspace ownership requirements from BuildNodeSelector.
//
// This is the canonical way to scope a node listing to a single Workspace.
// It matters when several Workspaces share the same user-supplied selector
// (e.g. replicas created by an InferenceSet): the user selector alone cannot
// tell sibling Workspaces apart, but BuildNodeSelector contributes a
// per-workspace ownership label (e.g. kaito.sh/workspace) that disambiguates.
//
// A nil provisioner is treated as "no extra requirements" (BYO-style), in which
// case the returned selector is just the sanitized user match labels.
func WorkspaceNodeSelector(ctx context.Context, p NodeProvisioner, ws *kaitov1beta1.Workspace) client.MatchingLabels {
	matchLabels := kaitov1beta1.SanitizedMatchLabels(ws.Resource.LabelSelector)
	if p != nil {
		for _, r := range p.BuildNodeSelector(ctx, ws) {
			// BuildNodeSelector always emits In/single-value today;
			// ignore anything that doesn't translate to a label match.
			if r.Operator != corev1.NodeSelectorOpIn || len(r.Values) != 1 {
				continue
			}
			if matchLabels == nil {
				matchLabels = map[string]string{}
			}
			matchLabels[r.Key] = r.Values[0]
		}
	}
	return client.MatchingLabels(matchLabels)
}

// ListWorkspaceNodes lists the nodes that belong to the given Workspace using
// WorkspaceNodeSelector. Callers should prefer this over listing by the raw
// user selector, which can leak nodes owned by sibling Workspaces that share
// the same selector.
func ListWorkspaceNodes(ctx context.Context, c client.Client, p NodeProvisioner, ws *kaitov1beta1.Workspace) (*corev1.NodeList, error) {
	return nodes.ListNodes(ctx, c, WorkspaceNodeSelector(ctx, p, ws))
}

// GetReadyNodes returns all ready nodes that match the workspace's label
// selector AND the provisioner's BuildNodeSelector output. Pinning the listing
// to provisioner-managed nodes mirrors what pods see at scheduling time, so
// callers do not count nodes pods cannot actually land on.
//
// A nil provisioner is treated as "no extra requirements" (BYO-style).
func GetReadyNodes(ctx context.Context, c client.Client, p NodeProvisioner, ws *kaitov1beta1.Workspace) ([]*corev1.Node, error) {
	nodeList, err := ListWorkspaceNodes(ctx, c, p, ws)
	if err != nil {
		return nil, err
	}

	readyNodes := make([]*corev1.Node, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !nodes.NodeIsReadyAndNotDeleting(node) {
			klog.V(4).InfoS("Node is not ready, skipping",
				"node", node.Name,
				"workspace", klog.KObj(ws))
			continue
		}
		readyNodes = append(readyNodes, node)
	}

	klog.V(4).InfoS("Found ready nodes",
		"workspace", klog.KObj(ws),
		"readyNodes", len(readyNodes))

	return readyNodes, nil
}
