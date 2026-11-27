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
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

const (
	// labelGPUProvisionerCustom marks nodes created by the Azure GPU provisioner.
	labelGPUProvisionerCustom = "kaito.sh/machine-type"
)

// GetNode get kubernetes node object with a provided name
func GetNode(ctx context.Context, nodeName string, kubeClient client.Client) (*corev1.Node, error) {
	node := &corev1.Node{}

	err := kubeClient.Get(ctx, client.ObjectKey{Name: nodeName}, node, &client.GetOptions{})
	if err != nil {
		return nil, err
	}
	return node, nil
}

// ListNodes get list of kubernetes nodes
func ListNodes(ctx context.Context, kubeClient client.Client, labelSelector client.MatchingLabels) (*corev1.NodeList, error) {
	nodeList := &corev1.NodeList{}

	err := kubeClient.List(ctx, nodeList, labelSelector)
	if err != nil {
		return nil, err
	}

	return nodeList, nil
}

// HasLabels reports whether node carries every label in labels.
func HasLabels(node *corev1.Node, labels map[string]string) bool {
	for k, v := range labels {
		if node.Labels[k] != v {
			return false
		}
	}
	return true
}

// EnsureLabels adds labels to node and updates it, unless node already has them.
func EnsureLabels(ctx context.Context, node *corev1.Node, labels map[string]string, kubeClient client.Client) error {
	if HasLabels(node, labels) {
		return nil
	}
	klog.InfoS("EnsureLabels", "nodeName", node.Name, "labels", labels)
	node.Labels = lo.Assign(node.Labels, labels)
	if err := kubeClient.Update(ctx, node); err != nil {
		klog.ErrorS(err, "cannot update node labels", "node", node.Name)
		return err
	}
	return nil
}

func ExtractObjFields(obj client.Object) (instanceType, namespace, name string, labelSelector *metav1.LabelSelector,
	nameLabel, namespaceLabel string, err error) {
	switch o := obj.(type) {
	case *kaitov1beta1.Workspace:
		instanceType = o.Resource.InstanceType
		namespace = o.Namespace
		name = o.Name
		labelSelector = o.Resource.LabelSelector
		nameLabel = kaitov1beta1.LabelWorkspaceName
		namespaceLabel = kaitov1beta1.LabelWorkspaceNamespace
	case *kaitov1beta1.RAGEngine:
		if o.Spec.Compute != nil {
			instanceType = o.Spec.Compute.InstanceType
			labelSelector = o.Spec.Compute.LabelSelector
		}
		namespace = o.Namespace
		name = o.Name
		nameLabel = kaitov1beta1.LabelRAGEngineName
		namespaceLabel = kaitov1beta1.LabelRAGEngineNamespace
	default:
		err = fmt.Errorf("unsupported object type: %T", obj)
	}
	return
}

func NodeIsReadyAndNotDeleting(node *corev1.Node) bool {
	if node.DeletionTimestamp != nil {
		return false
	}

	_, statusRunning := lo.Find(node.Status.Conditions, func(condition corev1.NodeCondition) bool {
		return condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue
	})

	return statusRunning
}
