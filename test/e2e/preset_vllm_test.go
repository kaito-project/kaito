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
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	kaitoutils "github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	controllers "github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var _ = Describe("Workspace Preset on vllm runtime", func() {
	BeforeEach(func() {
		loadTestEnvVars()
		loadModelVersions()
	})

	It("should create a qwen3-coder-30b-a3b-instruct two-node workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 2
		workspaceObj := createQWen3Coder30BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a single-node llama-3.1-8b-instruct workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
		workspaceObj := createLlama3_1_8BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode, "Standard_NV36ads_A10_v5")

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a multi-node llama-3.1-8b-instruct workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		// Need 2 Standard_NC6s_v3 nodes to run Llama 3.1-8B Instruct model.
		// Each node has 1 V100 GPU, so total 2 GPUs are used
		numOfNode := 2
		workspaceObj := createLlama3_1_8BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode, "Standard_NC16as_T4_v3")

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		time.Sleep(1 * time.Minute)
		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a Gemma InferenceSet with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfReplicas := 2
		inferenceSetObj := createGemmaInferenceSetWithPresetPublicModeAndVLLM(numOfReplicas)
		defer cleanupResourcesForInferenceSet(inferenceSetObj)
		time.Sleep(120 * time.Second)

		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, int32(numOfReplicas))
		validateInferenceSetBenchmarkCompleted(inferenceSetObj)
		validateGatewayAPIInferenceExtensionResources(inferenceSetObj)
	})

	It("should create a phi4 workspace with adapter successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createPhi4WorkspaceWithAdapterAndVLLM(numOfNode, phi4Adapter)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)

		expectedInitContainers := []corev1.Container{
			{
				Name:  baseInitContainer.Name + "-" + phi4AdapterName,
				Image: baseInitContainer.Image,
			},
		}
		validateInitContainers(workspaceObj, expectedInitContainers)

		validateAdapterLoadedInVLLM(workspaceObj, phi4AdapterName)
	})

	It("should create a llama-3.3-70b-instruct workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
		// Need 2 Standard_NC48ads_A100_v4 nodes to run Llama 3.3-70B Instruct model.
		// Each node has 2 A100 GPUs, so total 4 GPUs are used
		numOfNode := 2
		workspaceObj := createLlama3_3_70BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a gemma-3-4b-instruct workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
		workspaceObj := createGemma3_4BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a gemma-3-27b-instruct workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createGemma3_27BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a gpt-oss-20b workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createGPTOss20BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a gpt-oss-120b workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createGPTOss120BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a ministral-3-3b-instruct workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createMinistral3_3BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})
})

func createPhi4WorkspaceWithAdapterAndVLLM(numOfNode int, validAdapters []kaitov1beta1.AdapterSpec) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with phi4 mini preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi4-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi4-adapter-vllm"},
			}, nil, PresetPhi4MiniModel, nil, nil, validAdapters, "", "")

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createGemmaInferenceSetWithPresetPublicModeAndVLLM(replicas int) *kaitov1alpha1.InferenceSet {
	modelSecret := createAndValidateModelSecret()
	inferenceSetObj := &kaitov1alpha1.InferenceSet{}
	By("Creating a InferenceSet CR with Gemma preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gemma-is-", rand.Intn(1000))
		inferenceSetObj = utils.GenerateInferenceSetManifestWithVLLM(uniqueID, namespaceName, "", replicas, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-is-e2e-test-gemma-vllm"},
			}, PresetGemma3_4BInstructModel, nil, nil, modelSecret.Name)
		createAndValidateInferenceSet(inferenceSetObj)

	})
	return inferenceSetObj
}

func createLlama3_1_8BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int, instanceType string) *kaitov1beta1.Workspace {
	modelSecret := createAndValidateModelSecret()
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Llama 3.1-8B Instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-llama3-1-8b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, instanceType,
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": uniqueID},
			}, nil, PresetLlama3_1_8BInstruct, nil, nil, nil, modelSecret.Name, "") // Llama 3.1-8B Instruct model requires a model access secret
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createLlama3_3_70BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	modelSecret := createAndValidateModelSecret()
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Llama 3.3-70B Instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-llama3-3-70b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC48ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-llama3-3-70b-vllm"},
			}, nil, PresetLlama3_3_70BInstruct, nil, nil, nil, modelSecret.Name, "") // Llama 3.3-70B Instruct model requires a model access secret
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createGemma3_4BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	modelSecret := createAndValidateModelSecret()
	workspaceObj := &kaitov1beta1.Workspace{}

	By("Creating a workspace CR with Gemma 3 4B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gemma-3-4b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gemma-3-4b-vllm"},
			}, nil, PresetGemma3_4BInstructModel, nil, nil, nil, modelSecret.Name, "")

		createAndValidateWorkspace(workspaceObj)
	})

	return workspaceObj
}

func createGemma3_27BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	modelSecret := createAndValidateModelSecret()
	workspaceObj := &kaitov1beta1.Workspace{}

	By("Creating a workspace CR with Gemma 3 27B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gemma-3-27b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gemma-3-27b-vllm"},
			}, nil, PresetGemma3_27BInstructModel, nil, nil, nil, modelSecret.Name, "")

		createAndValidateWorkspace(workspaceObj)
	})

	return workspaceObj
}

func createGPTOss20BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}

	By("Creating a workspace CR with GPT-OSS-20B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gpt-oss-20b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gpt-oss-20b-vllm"},
			}, nil, PresetGPT_OSS_20BModel, nil, nil, nil, "", "")

		createAndValidateWorkspace(workspaceObj)
	})

	return workspaceObj
}

func createGPTOss120BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with GPT-OSS-120B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gpt-oss-120b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gpt-oss-120b-vllm"},
			}, nil, PresetGPT_OSS_120BModel, nil, nil, nil, "", "")

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createCustomInferenceConfigMapForE2E(name string) *corev1.ConfigMap {
	configMap := utils.GenerateE2EInferenceConfigMapManifest(name, namespaceName)

	By("Creating a custom workspace inference configmap for E2E", func() {
		createAndValidateConfigMap(configMap)
	})

	return configMap
}

func createQWen3Coder30BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Qwen3 Coder 30B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-qwen3-coder-30b-", rand.Intn(1000))
		configMap := createCustomInferenceConfigMapForE2E(fmt.Sprintf("%s-%s", "preset-qwen3-coder-30b", uniqueID))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV72ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-qwen3-coder-30b-vllm"},
			}, nil, PresetQwen3_Coder30BModel, nil, nil, nil, "", configMap.Name)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMinistral3_3BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Ministral 3 3B Instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-ministral-3-3b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-ministral-3-3b-instruct-vllm"},
			}, nil, PresetMinistral33BInstructModel, nil, nil, nil, "", "")

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func validateGatewayAPIInferenceExtensionResources(iObj *kaitov1alpha1.InferenceSet) {
	// Only validate if the Inference Preset is set
	if iObj.Spec.Template.Inference.Preset == nil {
		return
	}

	By("Checking Flux OCIRepository is Ready", func() {
		Eventually(func() bool {
			ociRepository := &sourcev1.OCIRepository{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: iObj.Namespace,
				Name:      kaitoutils.InferencePoolName(iObj.Name),
			}, ociRepository, &client.GetOptions{})
			if err != nil {
				return false
			}
			for _, cond := range ociRepository.Status.Conditions {
				if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to validate Flux OCIRepository is Ready")
	})

	By("Checking Flux HelmRelease is Ready", func() {
		Eventually(func() bool {
			helmRelease := &helmv2.HelmRelease{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: iObj.Namespace,
				Name:      kaitoutils.InferencePoolName(iObj.Name),
			}, helmRelease, &client.GetOptions{})
			if err != nil {
				return false
			}
			for _, cond := range helmRelease.Status.Conditions {
				if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to validate Flux HelmRelease is Ready")
	})
}

// validateWorkspaceBenchmarkCompleted asserts that:
// - BenchmarkCompleted condition is True
// - status.Performance.Metrics["peakTokensPerMinute"] is set with a positive value
// - config map has the four standard keys
func validateWorkspaceBenchmarkCompleted(workspaceObj *kaitov1beta1.Workspace) {
	By("Validating workspace benchmark completed and performance is set", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      workspaceObj.Name,
				Namespace: workspaceObj.Namespace,
			}, workspaceObj)
			if err != nil {
				return false
			}
			_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == string(kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted) &&
					condition.Status == metav1.ConditionTrue
			})
			if !conditionFound {
				return false
			}
			if workspaceObj.Status.Performance == nil {
				return false
			}
			m, ok := workspaceObj.Status.Performance.Metrics[controllers.BenchmarkMetricPeakTPM]
			if !ok {
				return false
			}
			tpm, err := strconv.ParseFloat(m.Value, 64)
			if err != nil || tpm <= 0 {
				return false
			}
			for _, key := range []string{"durationSec", "inputTokens", "outputTokens", "maxConcurrency"} {
				if _, hasKey := m.Config[key]; !hasKey {
					return false
				}
			}
			return true
		}, 30*time.Second, utils.PollInterval).Should(BeTrue(),
			"workspace benchmark should complete with valid performance metrics")
	})

	By("Printing benchmark elapsed time from pod logs", func() {
		coreClient, err := utils.GetK8sClientset()
		if err != nil {
			GinkgoWriter.Printf("WARNING: could not get k8s clientset to fetch benchmark logs: %v\n", err)
			return
		}
		podName := workspaceObj.Name + "-0"
		tailLines := int64(50)
		req := coreClient.CoreV1().Pods(workspaceObj.Namespace).GetLogs(podName, &corev1.PodLogOptions{
			TailLines: &tailLines,
		})
		stream, err := req.Stream(ctx)
		if err != nil {
			GinkgoWriter.Printf("WARNING: could not fetch logs for pod %s: %v\n", podName, err)
			return
		}
		defer stream.Close()
		buf := new(strings.Builder)
		if _, err = io.Copy(buf, stream); err != nil {
			GinkgoWriter.Printf("WARNING: could not read logs for pod %s: %v\n", podName, err)
			return
		}
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.Contains(line, "benchmark_done") {
				GinkgoWriter.Printf("[benchmark] %s: %s\n", workspaceObj.Name, line)
				return
			}
		}
		GinkgoWriter.Printf("[benchmark] %s: benchmark_done line not found in pod logs\n", workspaceObj.Name)
	})
}

// validateInferenceSetBenchmarkCompleted asserts that:
// - status.performance.metrics["aggregatedPeakTokensPerMinute"] is set with a positive value
// - all child workspaces have BenchmarkCompleted=True and their own performance set
func validateInferenceSetBenchmarkCompleted(inferenceSetObj *kaitov1alpha1.InferenceSet) {
	By("Validating inferenceset aggregated performance is set", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      inferenceSetObj.Name,
				Namespace: inferenceSetObj.Namespace,
			}, inferenceSetObj)
			if err != nil {
				return false
			}
			if inferenceSetObj.Status.Performance == nil {
				return false
			}
			m, ok := inferenceSetObj.Status.Performance.Metrics[controllers.BenchmarkMetricAggregatedPeakTPM]
			if !ok {
				return false
			}
			tpm, err := strconv.ParseFloat(m.Value, 64)
			if err != nil || tpm <= 0 {
				return false
			}
			return true
		}, 30*time.Second, utils.PollInterval).Should(BeTrue(),
			"inferenceset should have aggregated performance metric")
	})

	By("Validating all child workspace benchmarks completed", func() {
		wsList := &kaitov1beta1.WorkspaceList{}
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.List(ctx, wsList,
				client.InNamespace(inferenceSetObj.Namespace),
				client.MatchingLabels{consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name},
			)
			if err != nil {
				return false
			}
			for i := range wsList.Items {
				ws := &wsList.Items[i]
				_, condFound := lo.Find(ws.Status.Conditions, func(c metav1.Condition) bool {
					return c.Type == string(kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted) &&
						c.Status == metav1.ConditionTrue
				})
				if !condFound {
					return false
				}
				if ws.Status.Performance == nil {
					return false
				}
			}
			return true
		}, 30*time.Second, utils.PollInterval).Should(BeTrue(),
			"all child workspaces should have BenchmarkCompleted=True and performance set")
	})
}
