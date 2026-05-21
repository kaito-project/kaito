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

package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision/karpenter"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// ValidateWorkspaceTargetNodeCount verifies workspace.status.targetNodeCount
// matches the expected value.
func ValidateWorkspaceTargetNodeCount(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, expectedCount int) {
	ginkgo.By(fmt.Sprintf("Checking workspace %s has targetNodeCount=%d", workspaceObj.Name, expectedCount), func() {
		gomega.Eventually(func() int32 {
			err := TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, workspaceObj)
			if err != nil {
				return -1
			}
			return workspaceObj.Status.TargetNodeCount
		}, 2*time.Minute, PollInterval).Should(gomega.Equal(int32(expectedCount)),
			"workspace targetNodeCount should match expected")
	})
}

// ValidateNodePoolShape verifies the NodePool created for a workspace has the
// expected structure: name, labels, nodeClassRef, requirements, taints,
// disruption config, and replicas.
func ValidateNodePoolShape(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, expectedReplicas int) {
	nodePoolName := karpenter.NodePoolName(workspaceObj.Namespace, workspaceObj.Name)
	instanceType := workspaceObj.Resource.InstanceType

	ginkgo.By(fmt.Sprintf("Validating NodePool %s shape", nodePoolName), func() {
		np := &karpenterv1.NodePool{}
		gomega.Eventually(func() error {
			return TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: nodePoolName}, np)
		}, 2*time.Minute, PollInterval).Should(gomega.Succeed(),
			fmt.Sprintf("NodePool %s should exist", nodePoolName))

		// --- Metadata ---
		gomega.Expect(np.Name).To(gomega.Equal(nodePoolName), "NodePool name mismatch")

		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterLabelManagedBy, consts.KarpenterManagedByValue),
			"NodePool should have managed-by label")
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNameKey, workspaceObj.Name),
			"NodePool should have workspace-name label")
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNamespaceKey, workspaceObj.Namespace),
			"NodePool should have workspace-namespace label")

		// --- Replicas ---
		gomega.Expect(np.Spec.Replicas).NotTo(gomega.BeNil(), "NodePool replicas should be set")
		gomega.Expect(*np.Spec.Replicas).To(gomega.Equal(int64(expectedReplicas)),
			"NodePool replicas mismatch")

		// --- Template Labels ---
		templateLabels := np.Spec.Template.Labels
		gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNameKey, workspaceObj.Name),
			"Template should have workspace-name label")
		gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNamespaceKey, workspaceObj.Namespace),
			"Template should have workspace-namespace label")

		if workspaceObj.Resource.LabelSelector != nil {
			for k, v := range workspaceObj.Resource.LabelSelector.MatchLabels {
				gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(k, v),
					fmt.Sprintf("Template should have matchLabel %s=%s", k, v))
			}
		}

		// --- NodeClassRef ---
		ref := np.Spec.Template.Spec.NodeClassRef
		gomega.Expect(ref).NotTo(gomega.BeNil(), "NodeClassRef should be set")
		gomega.Expect(ref.Group).To(gomega.Equal("karpenter.azure.com"), "NodeClassRef group mismatch")
		gomega.Expect(ref.Kind).To(gomega.Equal("AKSNodeClass"), "NodeClassRef kind mismatch")
		gomega.Expect(ref.Name).To(gomega.Equal(consts.AKSNodeClassUbuntuName),
			"NodeClassRef name should be default ubuntu")

		// --- Requirements ---
		reqs := np.Spec.Template.Spec.Requirements
		gomega.Expect(reqs).To(gomega.HaveLen(2), "Should have 2 requirements")

		gomega.Expect(reqs[0].Key).To(gomega.Equal(corev1.LabelInstanceTypeStable))
		gomega.Expect(reqs[0].Operator).To(gomega.Equal(corev1.NodeSelectorOpIn))
		gomega.Expect(reqs[0].Values).To(gomega.ConsistOf(instanceType))

		gomega.Expect(reqs[1].Key).To(gomega.Equal(consts.AzurePlacementScopeLabel))
		gomega.Expect(reqs[1].Operator).To(gomega.Equal(corev1.NodeSelectorOpIn))
		gomega.Expect(reqs[1].Values).To(gomega.ConsistOf(consts.AzurePlacementRegional))

		// --- Taints ---
		gomega.Expect(np.Spec.Template.Spec.Taints).To(gomega.HaveLen(1), "Should have 1 taint")
		gomega.Expect(np.Spec.Template.Spec.Taints[0].Key).To(gomega.Equal(consts.SKUString))
		gomega.Expect(np.Spec.Template.Spec.Taints[0].Value).To(gomega.Equal(consts.GPUString))
		gomega.Expect(np.Spec.Template.Spec.Taints[0].Effect).To(gomega.Equal(corev1.TaintEffectNoSchedule))

		// --- Disruption ---
		gomega.Expect(np.Spec.Disruption.ConsolidateAfter.Duration).NotTo(gomega.BeNil(),
			"ConsolidateAfter should be set")
		expected := karpenterv1.MustParseNillableDuration("0s")
		gomega.Expect(*np.Spec.Disruption.ConsolidateAfter.Duration).To(
			gomega.Equal(*expected.Duration), "ConsolidateAfter should be 0s")

		gomega.Expect(np.Spec.Disruption.Budgets).To(gomega.HaveLen(1), "Should have 1 budget")
		gomega.Expect(np.Spec.Disruption.Budgets[0].Nodes).To(gomega.Equal("1"),
			"Standalone budget should be '1'")
		gomega.Expect(np.Spec.Disruption.Budgets[0].Reasons).To(
			gomega.ConsistOf(karpenterv1.DisruptionReasonDrifted),
			"Budget should be for Drifted reason only")
	})
}

// ValidateInferenceSetNodePoolShape verifies NodePool shape for an
// InferenceSet-managed workspace, including InferenceSet-specific labels
// and drift budget "0".
func ValidateInferenceSetNodePoolShape(ctx context.Context, workspaceObj *kaitov1beta1.Workspace,
	expectedReplicas int, inferenceSetName string) {

	nodePoolName := karpenter.NodePoolName(workspaceObj.Namespace, workspaceObj.Name)
	instanceType := workspaceObj.Resource.InstanceType

	ginkgo.By(fmt.Sprintf("Validating InferenceSet NodePool %s shape", nodePoolName), func() {
		np := &karpenterv1.NodePool{}
		gomega.Eventually(func() error {
			return TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: nodePoolName}, np)
		}, 2*time.Minute, PollInterval).Should(gomega.Succeed(),
			fmt.Sprintf("NodePool %s should exist", nodePoolName))

		// --- Metadata labels ---
		gomega.Expect(np.Name).To(gomega.Equal(nodePoolName))
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterLabelManagedBy, consts.KarpenterManagedByValue))
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNameKey, workspaceObj.Name))
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNamespaceKey, workspaceObj.Namespace))

		// InferenceSet labels on NodePool metadata
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterInferenceSetKey, inferenceSetName),
			"NodePool should have InferenceSet label")
		gomega.Expect(np.Labels).To(gomega.HaveKeyWithValue(
			consts.KarpenterInferenceSetNamespaceKey, workspaceObj.Namespace),
			"NodePool should have InferenceSet namespace label")

		// --- Replicas ---
		gomega.Expect(np.Spec.Replicas).NotTo(gomega.BeNil())
		gomega.Expect(*np.Spec.Replicas).To(gomega.Equal(int64(expectedReplicas)))

		// --- Template labels ---
		templateLabels := np.Spec.Template.Labels
		gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNameKey, workspaceObj.Name))
		gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(
			consts.KarpenterWorkspaceNamespaceKey, workspaceObj.Namespace))
		gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(
			consts.KarpenterInferenceSetKey, inferenceSetName),
			"Template should have InferenceSet label")
		gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(
			consts.KarpenterInferenceSetNamespaceKey, workspaceObj.Namespace),
			"Template should have InferenceSet namespace label")

		if workspaceObj.Resource.LabelSelector != nil {
			for k, v := range workspaceObj.Resource.LabelSelector.MatchLabels {
				gomega.Expect(templateLabels).To(gomega.HaveKeyWithValue(k, v))
			}
		}

		// --- NodeClassRef ---
		ref := np.Spec.Template.Spec.NodeClassRef
		gomega.Expect(ref).NotTo(gomega.BeNil())
		gomega.Expect(ref.Group).To(gomega.Equal("karpenter.azure.com"))
		gomega.Expect(ref.Kind).To(gomega.Equal("AKSNodeClass"))
		gomega.Expect(ref.Name).To(gomega.Equal(consts.AKSNodeClassUbuntuName))

		// --- Requirements ---
		reqs := np.Spec.Template.Spec.Requirements
		gomega.Expect(reqs).To(gomega.HaveLen(2))
		gomega.Expect(reqs[0].Key).To(gomega.Equal(corev1.LabelInstanceTypeStable))
		gomega.Expect(reqs[0].Operator).To(gomega.Equal(corev1.NodeSelectorOpIn))
		gomega.Expect(reqs[0].Values).To(gomega.ConsistOf(instanceType))
		gomega.Expect(reqs[1].Key).To(gomega.Equal(consts.AzurePlacementScopeLabel))
		gomega.Expect(reqs[1].Operator).To(gomega.Equal(corev1.NodeSelectorOpIn))
		gomega.Expect(reqs[1].Values).To(gomega.ConsistOf(consts.AzurePlacementRegional))

		// --- Taints ---
		gomega.Expect(np.Spec.Template.Spec.Taints).To(gomega.HaveLen(1))
		gomega.Expect(np.Spec.Template.Spec.Taints[0].Key).To(gomega.Equal(consts.SKUString))
		gomega.Expect(np.Spec.Template.Spec.Taints[0].Value).To(gomega.Equal(consts.GPUString))
		gomega.Expect(np.Spec.Template.Spec.Taints[0].Effect).To(gomega.Equal(corev1.TaintEffectNoSchedule))

		// --- Disruption — InferenceSet uses budget "0" ---
		gomega.Expect(np.Spec.Disruption.ConsolidateAfter.Duration).NotTo(gomega.BeNil())
		expected := karpenterv1.MustParseNillableDuration("0s")
		gomega.Expect(*np.Spec.Disruption.ConsolidateAfter.Duration).To(
			gomega.Equal(*expected.Duration))

		gomega.Expect(np.Spec.Disruption.Budgets).To(gomega.HaveLen(1))
		gomega.Expect(np.Spec.Disruption.Budgets[0].Nodes).To(gomega.Equal("0"),
			"InferenceSet budget should be '0'")
		gomega.Expect(np.Spec.Disruption.Budgets[0].Reasons).To(
			gomega.ConsistOf(karpenterv1.DisruptionReasonDrifted))
	})
}

// ValidateNodeLabels verifies that karpenter-provisioned Nodes have the
// expected labels propagated from the NodePool template.
func ValidateNodeLabels(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) {
	ginkgo.By(fmt.Sprintf("Validating node labels for workspace %s", workspaceObj.Name), func() {
		nodeClaimList, err := GetAllValidNodeClaims(ctx, workspaceObj)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to list NodeClaims")
		gomega.Expect(nodeClaimList.Items).NotTo(gomega.BeEmpty(), "Should have at least one NodeClaim")

		for _, nc := range nodeClaimList.Items {
			nodeName := nc.Status.NodeName
			gomega.Expect(nodeName).NotTo(gomega.BeEmpty(),
				fmt.Sprintf("NodeClaim %s should have a nodeName", nc.Name))

			node := &corev1.Node{}
			err := TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: nodeName}, node)
			gomega.Expect(err).NotTo(gomega.HaveOccurred(),
				fmt.Sprintf("Node %s should exist", nodeName))

			gomega.Expect(node.Labels).To(gomega.HaveKeyWithValue(
				consts.KarpenterWorkspaceNameKey, workspaceObj.Name),
				fmt.Sprintf("Node %s should have workspace-name label", nodeName))
			gomega.Expect(node.Labels).To(gomega.HaveKeyWithValue(
				consts.KarpenterWorkspaceNamespaceKey, workspaceObj.Namespace),
				fmt.Sprintf("Node %s should have workspace-namespace label", nodeName))

			if workspaceObj.Resource.LabelSelector != nil {
				for k, v := range workspaceObj.Resource.LabelSelector.MatchLabels {
					gomega.Expect(node.Labels).To(gomega.HaveKeyWithValue(k, v),
						fmt.Sprintf("Node %s should have matchLabel %s=%s", nodeName, k, v))
				}
			}
		}
	})
}

// ValidateNodePoolNodeClassRef verifies that the NodePool for a workspace
// references the expected NodeClass name.
func ValidateNodePoolNodeClassRef(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, expectedNodeClassName string) {
	nodePoolName := karpenter.NodePoolName(workspaceObj.Namespace, workspaceObj.Name)
	ginkgo.By(fmt.Sprintf("Validating NodePool %s references NodeClass %s", nodePoolName, expectedNodeClassName), func() {
		np := &karpenterv1.NodePool{}
		gomega.Eventually(func() error {
			return TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: nodePoolName}, np)
		}, 2*time.Minute, PollInterval).Should(gomega.Succeed(),
			fmt.Sprintf("NodePool %s should exist", nodePoolName))

		gomega.Expect(np.Spec.Template.Spec.NodeClassRef).NotTo(gomega.BeNil())
		gomega.Expect(np.Spec.Template.Spec.NodeClassRef.Name).To(gomega.Equal(expectedNodeClassName),
			"NodeClassRef name should match annotation override")
	})
}

// ValidateNodePoolDeletion verifies that the NodePool for a workspace
// has been deleted (via workspace finalizer).
func ValidateNodePoolDeletion(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) {
	nodePoolName := karpenter.NodePoolName(workspaceObj.Namespace, workspaceObj.Name)
	ginkgo.By(fmt.Sprintf("Validating NodePool %s is deleted", nodePoolName), func() {
		gomega.Eventually(func() bool {
			np := &karpenterv1.NodePool{}
			err := TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: nodePoolName}, np)
			if err == nil {
				return false // still exists
			}
			gomega.Expect(client.IgnoreNotFound(err)).NotTo(gomega.HaveOccurred(),
				fmt.Sprintf("unexpected error checking NodePool %s", nodePoolName))
			return true // NotFound
		}, 5*time.Minute, PollInterval).Should(gomega.BeTrue(),
			fmt.Sprintf("NodePool %s should be deleted after workspace deletion", nodePoolName))
	})
}

// ValidateNodePoolIsolation verifies that multiple workspaces have distinct
// NodePools with no shared NodeClaims.
func ValidateNodePoolIsolation(ctx context.Context, workspaces []*kaitov1beta1.Workspace) {
	ginkgo.By("Validating NodePool isolation across workspaces", func() {
		nodePoolNames := make(map[string]string)     // nodePoolName -> workspaceName
		allNodeClaimNames := make(map[string]string) // nodeClaimName -> nodePoolName

		for _, ws := range workspaces {
			npName := karpenter.NodePoolName(ws.Namespace, ws.Name)

			existing, exists := nodePoolNames[npName]
			gomega.Expect(exists).To(gomega.BeFalse(),
				fmt.Sprintf("NodePool %s is shared by workspaces %s and %s", npName, existing, ws.Name))
			nodePoolNames[npName] = ws.Name

			nodeClaimList, err := GetAllValidNodeClaims(ctx, ws)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, nc := range nodeClaimList.Items {
				prevNP, shared := allNodeClaimNames[nc.Name]
				gomega.Expect(shared).To(gomega.BeFalse(),
					fmt.Sprintf("NodeClaim %s shared between NodePools %s and %s", nc.Name, prevNP, npName))
				allNodeClaimNames[nc.Name] = npName
			}
		}
	})
}
