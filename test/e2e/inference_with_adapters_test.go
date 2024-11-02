// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package e2e

import (
	"fmt"
	"strings"
	"time"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/test/e2e/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var DefaultStrength = "1.0"

var imageName1 = "e2e-adapter"
var fullImageName1 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + imageName1 + ":0.0.1"
var imageName2 = "e2e-adapter2"
var fullImageName2 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + imageName2 + ":0.0.1"

var validAdapters1 = []kaitov1alpha1.AdapterSpec{
	{
		Source: &kaitov1alpha1.DataSource{
			Name:  imageName1,
			Image: fullImageName1,
		},
		Strength: &DefaultStrength,
	},
}

var validAdapters2 = []kaitov1alpha1.AdapterSpec{
	{
		Source: &kaitov1alpha1.DataSource{
			Name:  imageName2,
			Image: fullImageName2,
			ImagePullSecrets: []string{
				aiModelsRegistrySecret,
			},
		},
		Strength: &DefaultStrength,
	},
}

var expectedInitContainers1 = []corev1.Container{
	{
		Name:  imageName1,
		Image: fullImageName1,
	},
}

var expectedInitContainers2 = []corev1.Container{
	{
		Name:  imageName2,
		Image: fullImageName2,
	},
}

func validateInitContainers(workspaceObj *kaitov1alpha1.Workspace, expectedInitContainers []corev1.Container) {
	By("Checking the InitContainers", func() {
		GinkgoWriter.Printf("Starting to check InitContainers...\n")
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
			GinkgoWriter.Printf("initContainers:%v\n", initContainers)

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			if dep.Spec.Template.Spec.ImagePullSecrets == nil || len(dep.Spec.Template.Spec.ImagePullSecrets) == 0 {
				return false
			}
			if len(initContainers) != len(expectedInitContainers) {
				return false
			}
			initContainer, expectedInitContainer := initContainers[0], expectedInitContainers[0]

			GinkgoWriter.Printf("adapter output: initContainer.Image=%s, expectedInitContainer.Image=%s, equal=%v\n", initContainer.Image, expectedInitContainer.Image, initContainer.Image == expectedInitContainer.Image)
			GinkgoWriter.Printf("adapter output: initContainer.Name=%s, expectedInitContainer.Name=%s, equal=%v\n", initContainer.Name, expectedInitContainer.Name, initContainer.Name == expectedInitContainer.Name)

			return initContainer.Image == expectedInitContainer.Image && initContainer.Name == expectedInitContainer.Name
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for initContainers to be ready")
	})
}

func validateAdapterAdded(workspaceObj *kaitov1alpha1.Workspace, deploymentName string, adapterName string) {
	By("Checking the Adapters", func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sConfig()
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

var _ = Describe("Workspace Preset", func() {
	BeforeEach(func() {
		loadTestEnvVars()

		loadModelVersions()
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			utils.PrintPodLogsOnFailure(namespaceName, "")     // The Preset Pod
			utils.PrintPodLogsOnFailure("kaito-workspace", "") // The Kaito Workspace Pod
			utils.PrintPodLogsOnFailure("gpu-provisioner", "") // The gpu-provisioner Pod
			Fail("Fail threshold reached")
		}
	})

	It("should create a falcon workspace with adapter, and update the workspace with another adapter", func() {
		numOfNode := 1
		workspaceObj := createCustomWorkspaceWithAdapter(numOfNode, validAdapters1)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		if nodeProvisionerName == "azkarpenter" {
			utils.ValidateNodeClaimCreation(ctx, workspaceObj, numOfNode)
		} else {
			utils.ValidateMachineCreation(ctx, workspaceObj, numOfNode)
		}

		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)

		validateRevision(workspaceObj, "1")

		validateInitContainers(workspaceObj, expectedInitContainers1)
		validateAdapterAdded(workspaceObj, workspaceObj.Name, imageName1)

		workspaceObj = updateCustomWorkspaceWithAdapter(workspaceObj, validAdapters2)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)

		validateRevision(workspaceObj, "2")
		validateInitContainers(workspaceObj, expectedInitContainers2)
		validateAdapterAdded(workspaceObj, workspaceObj.Name, imageName2)
	})

})
