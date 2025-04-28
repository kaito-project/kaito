// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

const (
	LabelKeyNvidia    = "accelerator"
	LabelValueNvidia  = "nvidia"
	CapacityNvidiaGPU = "nvidia.com/gpu"
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

// UpdateNodeWithLabel update the node object with the label key/value
func UpdateNodeWithLabel(ctx context.Context, nodeName, labelKey, labelValue string, kubeClient client.Client) error {
	klog.InfoS("UpdateNodeWithLabel", "nodeName", nodeName, "labelKey", labelKey, "labelValue", labelValue)

	// get fresh node object
	freshNode, err := GetNode(ctx, nodeName, kubeClient)
	if err != nil {
		klog.ErrorS(err, "cannot get node", "node", nodeName)
		return err
	}

	freshNode.Labels = lo.Assign(freshNode.Labels, map[string]string{labelKey: labelValue})
	opt := &client.UpdateOptions{}

	err = kubeClient.Update(ctx, freshNode, opt)
	if err != nil {
		klog.ErrorS(err, "cannot update node label", "node", nodeName, labelKey, labelValue)
		return err
	}
	return nil
}

func CheckNvidiaPlugin(ctx context.Context, nodeObj *corev1.Node) bool {
	// check if label accelerator=nvidia exists in the node
	var foundLabel, foundCapacity bool
	if nvidiaLabelVal, found := nodeObj.Labels[LabelKeyNvidia]; found {
		if nvidiaLabelVal == LabelValueNvidia {
			foundLabel = true
		}
	}

	// check Status.Capacity.nvidia.com/gpu has value
	capacity := nodeObj.Status.Capacity
	if capacity != nil && !capacity.Name(CapacityNvidiaGPU, "").IsZero() {
		foundCapacity = true
	}

	if foundLabel && foundCapacity {
		return true
	}
	return false
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
	case *kaitov1alpha1.RAGEngine:
		instanceType = o.Spec.Compute.InstanceType
		namespace = o.Namespace
		name = o.Name
		labelSelector = o.Spec.Compute.LabelSelector
		nameLabel = kaitov1alpha1.LabelRAGEngineName
		namespaceLabel = kaitov1alpha1.LabelRAGEngineNamespace
	default:
		err = fmt.Errorf("unsupported object type: %T", obj)
	}
	return
}
