// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package e2e

import (
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var _ = Describe("Workspace Preset on vllm runtime", func() {
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

	It("should create a deepseek-distilled-llama-8b workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createDeepSeekLlama8BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a deepseek-distilled-qwen-14b workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		if !runLlama13B {
			Skip("Skipping deepseek-distilled-qwen-14b workspace test")
		}
		numOfNode := 1
		workspaceObj := createDeepSeekQwen14BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a falcon workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createFalconWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a mistral workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createMistralWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a Phi-2 workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createPhi2WorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a Phi-3-mini-128k-instruct workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
		workspaceObj := createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a qwen2.5 coder workspace with preset public mode and 2 gpu successfully", utils.GinkgoLabelFastCheck, func() {
		// single node with 2 gpu
		numOfNode := 1
		workspaceObj := createQwen2_5WorkspaceWithPresetPublicModeAndVLLMAndMultiGPU(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)
	})

	It("should create a phi4 workspace with adapter successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
		workspaceObj := createPhi4WorkspaceWithAdapterAndVLLM(numOfNode, phi4Adapter)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateCompletionsEndpoint(workspaceObj)

		expectedInitContainers := []corev1.Container{
			{
				Name:  baseInitContainer.Name + "-" + phi4AdapterName,
				Image: baseInitContainer.Image,
			},
		}
		validateInitContainers(workspaceObj, expectedInitContainers)

		validateAdapterLoadedInVLLM(workspaceObj, phi4AdapterName)
	})
})

func createDeepSeekLlama8BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with DeepSeek Distilled Llama 8B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-deepseek-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-deepseek-llama-vllm"},
			}, nil, PresetDeepSeekR1DistillLlama8BModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createDeepSeekQwen14BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with DeepSeek Distilled Qwen 14B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-deepseek-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-deepseek-qwen-vllm"},
			}, nil, PresetDeepSeekR1DistillQwen14BModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createFalconWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Falcon 7B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-falcon-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-falcon-vllm"},
			}, nil, PresetFalcon7BModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi4WorkspaceWithAdapterAndVLLM(numOfNode int, validAdapters []kaitov1beta1.AdapterSpec) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with phi4 mini preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi4-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi4-adapter-vllm"},
			}, nil, PresetPhi4MiniModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, validAdapters)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMistralWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Mistral 7B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-mistral-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-mistral-vllm"},
			}, nil, PresetMistral7BInstructModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi2WorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Phi 2 preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi2-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-2-vllm"},
			}, nil, PresetPhi2Model, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Phi-3-mini-128k-instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi3-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-3-mini-128k-instruct-vllm"},
			}, nil, PresetPhi3Mini128kModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createQwen2_5WorkspaceWithPresetPublicModeAndVLLMAndMultiGPU(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Qwen2.5 Coder 7B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-qwen-2gpu-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-qwen-2gpu-vllm"},
			}, nil, PresetQwen2_5Coder7BModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}
