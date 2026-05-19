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

package e2e

import (
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var _ = Describe("Karpenter Nightly", Serial, func() {
	BeforeEach(func() {
		if nodeProvisionerName != "azkarpenter" {
			Skip("Nightly Karpenter tests only run with azkarpenter provisioner")
		}
		loadTestEnvVars()
		loadModelVersions()
	})

	It("should scale InferenceSet up and down", utils.GinkgoLabelNightly, func() {
		modelSecret := createAndValidateModelSecret()
		uniqueID := fmt.Sprint("nightly-scale-", rand.Intn(1000))
		inferenceSetObj := utils.GenerateInferenceSetManifest(uniqueID, namespaceName, "",
			1, "Standard_NC4as_T4_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": uniqueID},
			}, PresetPhi4MiniModel, nil, nil, modelSecret.Name)

		defer cleanupResourcesForInferenceSet(inferenceSetObj)

		// Phase 1: Create with replicas=1
		By("Creating InferenceSet with replicas=1")
		createAndValidateInferenceSet(inferenceSetObj)

		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, 1)
		validateInferenceSetNodePools(inferenceSetObj, 1)

		// Phase 2: Scale up to replicas=2
		By("Scaling InferenceSet to replicas=2")
		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: inferenceSetObj.Namespace,
				Name:      inferenceSetObj.Name,
			}, inferenceSetObj)
			if err != nil {
				return err
			}
			inferenceSetObj.Spec.Replicas = 2
			return utils.TestingCluster.KubeClient.Update(ctx, inferenceSetObj)
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to scale InferenceSet to 2")

		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, 2)
		validateInferenceSetNodePools(inferenceSetObj, 2)

		// Phase 3: Scale down to replicas=1
		By("Scaling InferenceSet to replicas=1")
		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: inferenceSetObj.Namespace,
				Name:      inferenceSetObj.Name,
			}, inferenceSetObj)
			if err != nil {
				return err
			}
			inferenceSetObj.Spec.Replicas = 1
			return utils.TestingCluster.KubeClient.Update(ctx, inferenceSetObj)
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to scale InferenceSet to 1")

		// Wait for scale-down: one workspace should be deleted
		By("Waiting for scale-down to complete")
		Eventually(func() int {
			workspaceList := &kaitov1beta1.WorkspaceList{}
			err := utils.TestingCluster.KubeClient.List(ctx, workspaceList,
				client.InNamespace(inferenceSetObj.Namespace),
				client.MatchingLabels{
					consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name,
				})
			if err != nil {
				return -1
			}
			return len(workspaceList.Items)
		}, 10*time.Minute, utils.PollInterval).Should(Equal(1),
			"Should have 1 workspace after scale-down")

		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, 1)
	})

	It("should perform orchestrated drift upgrade one workspace at a time", utils.GinkgoLabelNightly, func() {
		modelSecret := createAndValidateModelSecret()
		uniqueID := fmt.Sprint("nightly-drift-", rand.Intn(1000))
		inferenceSetObj := utils.GenerateInferenceSetManifest(uniqueID, namespaceName, "",
			2, "Standard_NC4as_T4_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": uniqueID},
			}, PresetPhi4MiniModel, nil, nil, modelSecret.Name)

		defer cleanupResourcesForInferenceSet(inferenceSetObj)

		// Phase 1: Create InferenceSet with replicas=2, wait for both ready
		By("Creating InferenceSet with replicas=2")
		createAndValidateInferenceSet(inferenceSetObj)

		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, 2)
		validateInferenceSetNodePools(inferenceSetObj, 2)

		// Phase 2: Verify both NodePools have budget "0" (drift blocked)
		By("Verifying both NodePools have drift budget '0'")
		workspaceList := &kaitov1beta1.WorkspaceList{}
		err := utils.TestingCluster.KubeClient.List(ctx, workspaceList,
			client.InNamespace(inferenceSetObj.Namespace),
			client.MatchingLabels{
				consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name,
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceList.Items).To(HaveLen(2))

		for i := range workspaceList.Items {
			utils.ValidateInferenceSetNodePoolShape(ctx, &workspaceList.Items[i], 1, inferenceSetObj.Name)
		}

		// Phase 3: Trigger drift by updating AKSNodeClass
		By("Triggering drift by updating AKSNodeClass")
		utils.TriggerDrift(ctx)

		// Phase 4: Wait for NodeClaims to be marked Drifted
		By("Waiting for NodeClaims to be marked as Drifted")
		Eventually(func() int {
			driftedCount := 0
			nodeClaimList := &karpenterv1.NodeClaimList{}
			err := utils.TestingCluster.KubeClient.List(ctx, nodeClaimList,
				client.MatchingLabels{
					consts.KarpenterInferenceSetKey:          inferenceSetObj.Name,
					consts.KarpenterInferenceSetNamespaceKey: inferenceSetObj.Namespace,
				})
			if err != nil {
				return 0
			}
			for _, nc := range nodeClaimList.Items {
				for _, cond := range nc.Status.Conditions {
					if cond.Type == karpenterv1.ConditionTypeDrifted && cond.Status == metav1.ConditionTrue {
						driftedCount++
					}
				}
			}
			return driftedCount
		}, 5*time.Minute, 10*time.Second).Should(Equal(2),
			"Both NodeClaims should be marked Drifted")

		// Phase 5: Wait for all drift to complete (both workspaces back to ready, no drifted NodeClaims)
		By("Waiting for drift upgrade to complete for all workspaces")
		Eventually(func() bool {
			// Check no drifted NodeClaims remain
			nodeClaimList := &karpenterv1.NodeClaimList{}
			err := utils.TestingCluster.KubeClient.List(ctx, nodeClaimList,
				client.MatchingLabels{
					consts.KarpenterInferenceSetKey:          inferenceSetObj.Name,
					consts.KarpenterInferenceSetNamespaceKey: inferenceSetObj.Namespace,
				})
			if err != nil {
				return false
			}
			for _, nc := range nodeClaimList.Items {
				for _, cond := range nc.Status.Conditions {
					if cond.Type == karpenterv1.ConditionTypeDrifted && cond.Status == metav1.ConditionTrue {
						return false
					}
				}
			}

			// Check both budgets back to "0"
			nodePoolList := &karpenterv1.NodePoolList{}
			err = utils.TestingCluster.KubeClient.List(ctx, nodePoolList,
				client.MatchingLabels{
					consts.KarpenterInferenceSetKey:          inferenceSetObj.Name,
					consts.KarpenterInferenceSetNamespaceKey: inferenceSetObj.Namespace,
				})
			if err != nil {
				return false
			}
			for _, np := range nodePoolList.Items {
				for _, budget := range np.Spec.Disruption.Budgets {
					for _, reason := range budget.Reasons {
						if reason == karpenterv1.DisruptionReasonDrifted && budget.Nodes != "0" {
							return false
						}
					}
				}
			}
			return true
		}, 30*time.Minute, 30*time.Second).Should(BeTrue(),
			"All drift should complete — no Drifted NodeClaims and all budgets back to '0'")

		// Phase 7: Verify InferenceSet is still healthy
		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, 2)
	})
})
