package e2e

import (
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	It("should create a falcon workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createFalconWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
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

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
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

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a Phi-3-mini-128k-instruct workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a mistral workspace with preset public mode and 2 gpu successfully", func() {
		// single node with 2 gpu
		numOfNode := 1
		workspaceObj := createMistralWorkspaceWithPresetPublicModeAndVLLMAndMultiGPU(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})
})

func createFalconWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Falcon 7B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-falcon-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-falcon-vllm"},
			}, nil, PresetFalcon7BModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMistralWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Mistral 7B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-mistral-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-mistral-vllm"},
			}, nil, PresetMistral7BInstructModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi2WorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Phi 2 preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi2-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-2-vllm"},
			}, nil, PresetPhi2Model, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Phi-3-mini-128k-instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi3-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-3-mini-128k-instruct-vllm"},
			}, nil, PresetPhi3Mini128kModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMistralWorkspaceWithPresetPublicModeAndVLLMAndMultiGPU(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Mistral 7B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-mistral-2gpu-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-mistral-2gpu-vllm"},
			}, nil, PresetMistral7BInstructModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}
