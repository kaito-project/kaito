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
	"strings"
	"time"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/test/e2e/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Workspace Preset AzureLinux", utils.GinkgoLabelAzureLinux, func() {
	BeforeEach(func() {
		loadTestEnvVars()
		loadModelVersions()
	})

	It("should create a phi4 workspace with vllm on azure linux successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createPhi4WorkspaceWithVLLMOnAzureLinux(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)

		validateNodeOSImage(workspaceObj, []string{"Azure", "Linux"})

		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})
})

func createPhi4WorkspaceWithVLLMOnAzureLinux(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with phi4 mini preset public mode and vLLM on azure linux node", func() {
		uniqueID := fmt.Sprint("preset-phi4-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi4-adapter-vllm-azure-linux"},
			}, nil, PresetPhi4MiniModel, nil, nil, nil, "", "")
		if workspaceObj.Annotations == nil {
			workspaceObj.Annotations = make(map[string]string)
		}
		workspaceObj.Annotations[kaitov1beta1.AnnotationNodeImageFamily] = "AzureLinux"
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func validateNodeOSImage(workspaceObj *kaitov1beta1.Workspace, expectedImageSubStrs []string) {
	By("Checking node OS image", func() {
		Eventually(func() bool {
			nodeClaimList, err := utils.GetAllValidNodeClaims(ctx, workspaceObj)
			if err != nil {
				GinkgoWriter.Printf("Error listing NodeClaims for workspace %s/%s: %v\n", workspaceObj.Namespace, workspaceObj.Name, err)
				return false
			}

			if len(nodeClaimList.Items) == 0 {
				GinkgoWriter.Printf("No NodeClaims found yet for workspace %s/%s\n", workspaceObj.Namespace, workspaceObj.Name)
				return false
			}

			for _, nodeClaim := range nodeClaimList.Items {
				nodeName := nodeClaim.Status.NodeName
				if nodeName == "" {
					GinkgoWriter.Printf("NodeClaim %s has empty status.nodeName, waiting\n", nodeClaim.Name)
					return false
				}

				node := &corev1.Node{}
				if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: nodeName}, node, &client.GetOptions{}); err != nil {
					GinkgoWriter.Printf("Error fetching node %s for NodeClaim %s: %v\n", nodeName, nodeClaim.Name, err)
					return false
				}

				osImage := node.Status.NodeInfo.OSImage
				if osImage == "" {
					GinkgoWriter.Printf("Node %s OSImage is empty, waiting\n", node.Name)
					return false
				}

				for _, family := range expectedImageSubStrs {
					if !strings.Contains(osImage, family) {
						Fail(fmt.Sprintf("Node %s OSImage mismatch. expected contains Azure and Linux (target family: %q), actual: %s", node.Name, expectedImageSubStrs, osImage))
						return false
					}
				}
			}

			return true
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to validate node OS image contains Azure and Linux")
	})
}
