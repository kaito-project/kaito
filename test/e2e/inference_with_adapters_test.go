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
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/workspace/image"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var DefaultStrength = "1.0"

var imageName1 = "e2e-adapter"
var fullImageName1 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + imageName1 + ":0.0.1"
var imageName2 = "e2e-adapter2"
var fullImageName2 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + imageName2 + ":0.0.1"

var testAdapters1 = []kaitov1beta1.AdapterSpec{
	{
		Source: &kaitov1beta1.DataSource{
			Name:  imageName1,
			Image: fullImageName1,
			ImagePullSecrets: []string{
				utils.GetEnv("E2E_ACR_REGISTRY_SECRET"),
			},
		},
		Strength: &DefaultStrength,
	},
}

var testAdapters2 = []kaitov1beta1.AdapterSpec{
	{
		Source: &kaitov1beta1.DataSource{
			Name:  imageName2,
			Image: fullImageName2,
			ImagePullSecrets: []string{
				utils.GetEnv("E2E_ACR_REGISTRY_SECRET"),
			},
		},
		Strength: &DefaultStrength,
	},
}

var phi4AdapterName = "adapter-phi-3-mini-pycoder"
var phi4Adapter = []kaitov1beta1.AdapterSpec{
	{
		Source: &kaitov1beta1.DataSource{
			Name:  phi4AdapterName,
			Image: utils.GetEnv("E2E_ACR_REGISTRY") + "/" + phi4AdapterName + ":0.0.1",
			ImagePullSecrets: []string{
				utils.GetEnv("E2E_ACR_REGISTRY_SECRET"),
			},
		},
	},
}

var baseInitContainer = image.NewPullerContainer("", "")

func validateInitContainers(workspaceObj *kaitov1beta1.Workspace, expectedInitContainers []corev1.Container) {
	By("Checking the InitContainers", func() {
		Eventually(func() bool {
			var err error
			var initContainers []corev1.Container

			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workspaceObj.Name,
					Namespace: workspaceObj.Namespace,
				},
			}
			err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, dep)
			initContainers = dep.Spec.Template.Spec.InitContainers

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			for _, initContainer := range expectedInitContainers {
				found := false
				for _, gotContainer := range initContainers {
					if initContainer.Name == gotContainer.Name && initContainer.Image == gotContainer.Image {
						// Found a matching init container
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}

			return true
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for initContainers to be ready")
	})
}

func validateImagePullSecrets(workspaceObj *kaitov1beta1.Workspace, expectedImagePullSecrets []string) {
	By("Checking the ImagePullSecrets", func() {
		Eventually(func() bool {
			var err error

			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workspaceObj.Name,
					Namespace: workspaceObj.Namespace,
				},
			}
			err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, dep)

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}
			if dep.Spec.Template.Spec.ImagePullSecrets == nil {
				return false
			}

			return utils.CompareSecrets(dep.Spec.Template.Spec.ImagePullSecrets, expectedImagePullSecrets)
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for ImagePullSecrets to be ready")
	})
}

func validateAdapterAdded(workspaceObj *kaitov1beta1.Workspace, deploymentName string, adapterName string) {
	By("Checking the Adapters", func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				GinkgoWriter.Printf("Failed to create core client: %v\n", err)
				return false
			}

			namespace := workspaceObj.Namespace
			podName, err := utils.GetPodNameForDeployment(coreClient, namespace, deploymentName)
			if err != nil {
				GinkgoWriter.Printf("Failed to get pod name for deployment %s: %v\n", deploymentName, err)
				return false
			}

			logs, err := utils.GetPodLogs(coreClient, namespace, podName, "")
			if err != nil {
				GinkgoWriter.Printf("Failed to get logs from pod %s: %v\n", podName, err)
				return false
			}

			searchStringAdapter := fmt.Sprintf("Adapter added: %s", adapterName)
			searchStringModelSuccess := "Model loaded successfully"

			return strings.Contains(logs, searchStringAdapter) && strings.Contains(logs, searchStringModelSuccess)
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for adapter resource to be ready")
	})
}

func validateAdapterLoadedInVLLM(workspaceObj *kaitov1beta1.Workspace, adapterName string) {
	deploymentName := workspaceObj.Name
	execOption := corev1.PodExecOptions{
		Command:   []string{"bash", "-c", "apt-get update && apt-get install curl -y; curl -s 127.0.0.1:5000/v1/models | grep " + adapterName},
		Container: deploymentName,
		Stdout:    true,
		Stderr:    true,
	}

	By("Checking the loaded Adapters", func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				GinkgoWriter.Printf("Failed to create core client: %v\n", err)
				return false
			}

			namespace := workspaceObj.Namespace
			podName, err := utils.GetPodNameForDeployment(coreClient, namespace, deploymentName)
			if err != nil {
				GinkgoWriter.Printf("Failed to get pod name for deployment %s: %v\n", deploymentName, err)
				return false
			}

			k8sConfig, err := utils.GetK8sConfig()
			if err != nil {
				GinkgoWriter.Printf("Failed to get k8s config: %v\n", err)
				return false
			}

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			_, err = utils.ExecSync(ctx, k8sConfig, coreClient, namespace, podName, execOption)
			if err != nil {
				GinkgoWriter.Printf("validate command fails: %v\n", err)
				return false
			}
			return true
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for adapter to be loaded")
	})
}

var _ = Describe("Workspace Preset", func() {
	BeforeEach(func() {
		loadTestEnvVars()
		loadModelVersions()
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			utils.PrintPodLogsOnFailure(namespaceName, "")     // The Preset Pod
			utils.PrintPodLogsOnFailure("kaito-workspace", "") // The Kaito Workspace Pod
			if !*skipGPUProvisionerCheck {
				utils.PrintPodLogsOnFailure("gpu-provisioner", "") // The gpu-provisioner Pod
			}
			Fail("Fail threshold reached")
		}
	})

	It("should create a falcon workspace with adapter, and update the workspace with another adapter", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
		workspaceObj := createCustomWorkspaceWithAdapter(numOfNode, testAdapters1)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		utils.ValidateNodeClaimCreation(ctx, workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)

		validateRevision(workspaceObj, "1")

		var expectedInitContainers1 = []corev1.Container{
			{
				Name:  baseInitContainer.Name + "-" + imageName1,
				Image: baseInitContainer.Image,
			},
		}
		var expectedInitContainers2 = []corev1.Container{
			{
				Name:  baseInitContainer.Name + "-" + imageName2,
				Image: baseInitContainer.Image,
			},
		}

		validateInitContainers(workspaceObj, expectedInitContainers1)
		validateImagePullSecrets(workspaceObj, testAdapters1[0].Source.ImagePullSecrets)
		validateAdapterAdded(workspaceObj, workspaceObj.Name, imageName1)

		workspaceObj = updateCustomWorkspaceWithAdapter(workspaceObj, testAdapters2)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)

		validateRevision(workspaceObj, "2")
		validateInitContainers(workspaceObj, expectedInitContainers2)
		validateImagePullSecrets(workspaceObj, testAdapters2[0].Source.ImagePullSecrets)
		validateAdapterAdded(workspaceObj, workspaceObj.Name, imageName2)
	})
})
