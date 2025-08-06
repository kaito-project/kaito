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
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

const (
	PresetPhi3Mini128kModel = "phi-3-mini-128k-instruct"
)

func loadTestEnvVars() {
	// Required for Llama models
	aiModelsRegistry = utils.GetEnv("AI_MODELS_REGISTRY")
	aiModelsRegistrySecret = utils.GetEnv("AI_MODELS_REGISTRY_SECRET")
	// Currently required for uploading fine-tuning results
	e2eACRSecret = utils.GetEnv("E2E_ACR_REGISTRY_SECRET")
	supportedModelsYamlPath = utils.GetEnv("SUPPORTED_MODELS_YAML_PATH")
	azureClusterName = utils.GetEnv("AZURE_CLUSTER_NAME")
}

func loadModelVersions() {
	// Load stable model versions
	configs, err := utils.GetModelConfigInfo(supportedModelsYamlPath)
	if err != nil {
		fmt.Printf("Failed to load model configs: %v\n", err)
		os.Exit(1)
	}

	modelInfo, err = utils.ExtractModelVersion(configs)
	if err != nil {
		fmt.Printf("Failed to extract stable model versions: %v\n", err)
		os.Exit(1)
	}
}

var aiModelsRegistry string
var aiModelsRegistrySecret string
var e2eACRSecret string
var supportedModelsYamlPath string
var modelInfo map[string]string
var azureClusterName string

var _ = Describe("RAGEngine", func() {
	BeforeEach(func() {
		loadTestEnvVars()
		loadModelVersions()
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			utils.PrintPodLogsOnFailure(namespaceName, "")     // The Preset Pod
			utils.PrintPodLogsOnFailure("kaito-workspace", "") // The Kaito Workspace Pod
			utils.PrintPodLogsOnFailure("kaito-ragengine", "") // The Kaito ragengine Pod
			if !*skipGPUProvisionerCheck {
				utils.PrintPodLogsOnFailure("gpu-provisioner", "") // The gpu-provisioner Pod
			}
			Fail("Fail threshold reached")
		}
	})

	It("should create RAG with localembedding and huggingface API successfully", func() {
		numOfReplica := 1

		createAndValidateSecret()
		ragengineObj := createLocalEmbeddingHFURLRAGEngine()

		defer cleanupResources(nil, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfReplica), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

		indexDoc, err := createAndValidateIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate IndexPod")
		Expect(indexDoc).NotTo(BeNil(), "Index document should not be nil")
		Expect(indexDoc["doc_id"]).NotTo(BeNil(), "Index document ID should not be nil")
		Expect(indexDoc["text"]).NotTo(BeNil(), "Index document text should not be nil")
		docID := indexDoc["doc_id"].(string)

		searchQuerySuccess := "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster"
		err = createAndValidateQueryPod(ragengineObj, searchQuerySuccess, true)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate QueryPod")

		err = createAndValidateQueryChatMessagesPod(ragengineObj, searchQuerySuccess, true)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate QueryChatMessagesPod")

		persistLogSuccess := "Successfully persisted index kaito"
		err = createAndValidatePersistPod(ragengineObj, persistLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate PersistPod")

		loadLogSuccess := "Successfully loaded index kaito"
		err = createAndValidateLoadPod(ragengineObj, loadLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate LoadPod")

		err = createAndValidateUpdateDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate UpdateDocumentPod")

		err = createAndValidateDeleteDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteDocumentPod")

		err = createAndValidateDeleteIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteIndexPod")
	})

	It("should create RAG with localembedding and kaito VLLM workspace successfully", utils.GinkgoLabelFastCheck, func() {
		numOfReplica := 1
		workspaceObj := createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfReplica)

		time.Sleep(30 * time.Second)

		validateWorkspaceResourceStatus(workspaceObj)

		validateAssociatedService(workspaceObj.ObjectMeta)

		validateInferenceandRAGResource(workspaceObj.ObjectMeta, int32(numOfReplica), false)

		validateWorkspaceReadiness(workspaceObj)

		serviceName := workspaceObj.ObjectMeta.Name
		serviceNamespace := workspaceObj.ObjectMeta.Namespace
		service := &v1.Service{}

		_ = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: serviceNamespace,
			Name:      serviceName,
		}, service)

		clusterIP := service.Spec.ClusterIP

		ragengineObj := createLocalEmbeddingKaitoVLLMRAGEngine(clusterIP, "v1/completions")

		defer cleanupResources(workspaceObj, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfReplica), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

		indexDoc, err := createAndValidateIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate IndexPod")
		Expect(indexDoc).NotTo(BeNil(), "Index document should not be nil")
		Expect(indexDoc["doc_id"]).NotTo(BeNil(), "Index document ID should not be nil")
		Expect(indexDoc["text"]).NotTo(BeNil(), "Index document text should not be nil")
		docID := indexDoc["doc_id"].(string)

		searchQuerySuccess := "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster"
		err = createAndValidateQueryPod(ragengineObj, searchQuerySuccess, false)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate QueryPod")

		persistLogSuccess := "Successfully persisted index kaito"
		err = createAndValidatePersistPod(ragengineObj, persistLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate PersistPod")

		loadLogSuccess := "Successfully loaded index kaito"
		err = createAndValidateLoadPod(ragengineObj, loadLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate LoadPod")

		err = createAndValidateUpdateDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate UpdateDocumentPod")

		err = createAndValidateDeleteDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteDocumentPod")

		err = createAndValidateDeleteIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteIndexPod")
	})

	It("should create RAG with preferred nodes and kaito VLLM workspace successfully", utils.GinkgoLabelFastCheck, func() {
		numOfReplica := 1
		workspaceObj := createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfReplica)

		time.Sleep(30 * time.Second)

		validateWorkspaceResourceStatus(workspaceObj)

		validateAssociatedService(workspaceObj.ObjectMeta)

		validateInferenceandRAGResource(workspaceObj.ObjectMeta, int32(numOfReplica), false)

		validateWorkspaceReadiness(workspaceObj)

		serviceName := workspaceObj.Name
		serviceNamespace := workspaceObj.Namespace
		service := &v1.Service{}

		_ = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: serviceNamespace,
			Name:      serviceName,
		}, service)

		clusterIP := service.Spec.ClusterIP

		preferredNode, err := getRagPoolNode()
		Expect(err).NotTo(HaveOccurred(), "Failed to get ragpool node")

		// Get the preferred node from ragpool
		ragengineObj := createLocalPreferredNodesRAGEngine(clusterIP, preferredNode)

		defer cleanupResources(workspaceObj, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfReplica), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

		indexDoc, err := createAndValidateIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate IndexPod")
		Expect(indexDoc).NotTo(BeNil(), "Index document should not be nil")
		Expect(indexDoc["doc_id"]).NotTo(BeNil(), "Index document ID should not be nil")
		Expect(indexDoc["text"]).NotTo(BeNil(), "Index document text should not be nil")
		docID := indexDoc["doc_id"].(string)

		searchQuerySuccess := "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster"
		err = createAndValidateQueryPod(ragengineObj, searchQuerySuccess, false)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate QueryPod")

		persistLogSuccess := "Successfully persisted index kaito"
		err = createAndValidatePersistPod(ragengineObj, persistLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate PersistPod")

		loadLogSuccess := "Successfully loaded index kaito"
		err = createAndValidateLoadPod(ragengineObj, loadLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate LoadPod")

		err = createAndValidateUpdateDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate UpdateDocumentPod")

		err = createAndValidateDeleteDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteDocumentPod")

		err = createAndValidateDeleteIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteIndexPod")
	})

	It("should create RAG with localembedding and kaito VLLM workspace successfully for chat completions", utils.GinkgoLabelFastCheck, func() {
		numOfReplica := 1
		workspaceObj := createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfReplica)

		time.Sleep(30 * time.Second)

		validateWorkspaceResourceStatus(workspaceObj)

		validateAssociatedService(workspaceObj.ObjectMeta)

		validateInferenceandRAGResource(workspaceObj.ObjectMeta, int32(numOfReplica), false)

		validateWorkspaceReadiness(workspaceObj)

		serviceName := workspaceObj.ObjectMeta.Name
		serviceNamespace := workspaceObj.ObjectMeta.Namespace
		service := &v1.Service{}

		_ = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: serviceNamespace,
			Name:      serviceName,
		}, service)

		clusterIP := service.Spec.ClusterIP

		ragengineObj := createLocalEmbeddingKaitoVLLMRAGEngine(clusterIP, "v1/chat/completions")

		defer cleanupResources(workspaceObj, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfReplica), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

		indexDoc, err := createAndValidateIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate IndexPod")
		Expect(indexDoc).NotTo(BeNil(), "Index document should not be nil")
		Expect(indexDoc["doc_id"]).NotTo(BeNil(), "Index document ID should not be nil")
		Expect(indexDoc["text"]).NotTo(BeNil(), "Index document text should not be nil")
		docID := indexDoc["doc_id"].(string)

		searchQuerySuccess := "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster"
		err = createAndValidateQueryChatMessagesPod(ragengineObj, searchQuerySuccess, false)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate QueryChatMessagesPod")

		persistLogSuccess := "Successfully persisted index kaito"
		err = createAndValidatePersistPod(ragengineObj, persistLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate PersistPod")

		loadLogSuccess := "Successfully loaded index kaito"
		err = createAndValidateLoadPod(ragengineObj, loadLogSuccess)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate LoadPod")

		err = createAndValidateUpdateDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate UpdateDocumentPod")

		err = createAndValidateDeleteDocumentPod(ragengineObj, docID)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteDocumentPod")

		err = createAndValidateDeleteIndexPod(ragengineObj)
		Expect(err).NotTo(HaveOccurred(), "Failed to create and validate DeleteIndexPod")
	})

})

func createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfReplica int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Phi-3-mini-128k-instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi3-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfReplica, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "rag-e2e-test-phi-4-mini-instruct-vllm"},
			}, nil, PresetPhi3Mini128kModel, nil, nil, nil, "")

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createConfigForWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	if workspaceObj.Inference == nil || workspaceObj.Resource.InstanceType == "" {
		return
	}

	// TODO: uncomment the following lines when A10 GPU support is added
	// handler := sku.GetCloudSKUHandler(consts.AzureCloudName)
	// gpuConfig := handler.GetGPUConfigBySKU(workspaceObj.Resource.InstanceType)
	// if gpuConfig == nil || (gpuConfig.GPUCount <= 1 && lo.FromPtr(workspaceObj.Resource.Count) <= 1) {
	// 	return
	// }

	By("Creating config file", func() {
		cm := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inference-config",
				Namespace: workspaceObj.Namespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
vllm:
  max-model-len: 4096
`,
			},
		}
		workspaceObj.Inference.Config = cm.Name

		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Create(ctx, &cm, &client.CreateOptions{})
			return client.IgnoreAlreadyExists(err)
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create configmap %s", cm.Name)
	})
}

func createAndValidateWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	createConfigForWorkspace(workspaceObj)
	By("Creating workspace", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create workspace %s", workspaceObj.ObjectMeta.Name)
	})
}

func createAndValidateRAGEngine(ragEngineObj *kaitov1alpha1.RAGEngine) {
	By("Creating ragEngine", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, ragEngineObj, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create ragEngine   %s", ragEngineObj.ObjectMeta.Name)

		By("Validating ragEngine creation", func() {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: ragEngineObj.ObjectMeta.Namespace,
				Name:      ragEngineObj.ObjectMeta.Name,
			}, ragEngineObj, &client.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
}

func GenerateLocalEmbeddingRAGEngineManifest(name, namespace, instanceType, embeddingModelID string, labelSelector *metav1.LabelSelector, inferenceSpec *kaitov1alpha1.InferenceServiceSpec) *kaitov1alpha1.RAGEngine {
	return &kaitov1alpha1.RAGEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &kaitov1alpha1.RAGEngineSpec{
			Compute: &kaitov1alpha1.ResourceSpec{
				InstanceType:  instanceType,
				LabelSelector: labelSelector,
			},
			Embedding: &kaitov1alpha1.EmbeddingSpec{
				Local: &kaitov1alpha1.LocalEmbeddingSpec{
					ModelID: embeddingModelID,
				},
			},
			InferenceService: inferenceSpec,
		},
	}
}

func GenerateLocalEmbeddingRAGEngineManifestWithPreferredNodes(name, namespace, preferredNodes, embeddingModelID string, labelSelector *metav1.LabelSelector, inferenceSpec *kaitov1alpha1.InferenceServiceSpec) *kaitov1alpha1.RAGEngine {
	return &kaitov1alpha1.RAGEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: &kaitov1alpha1.RAGEngineSpec{
			Compute: &kaitov1alpha1.ResourceSpec{
				PreferredNodes: []string{preferredNodes},
				LabelSelector:  labelSelector,
			},
			Embedding: &kaitov1alpha1.EmbeddingSpec{
				Local: &kaitov1alpha1.LocalEmbeddingSpec{
					ModelID: embeddingModelID,
				},
			},
			InferenceService: inferenceSpec,
		},
	}
}

// validateWorkspaceReadiness validates workspace readiness
func validateWorkspaceReadiness(workspaceObj *kaitov1beta1.Workspace) {
	By("Checking the workspace status is ready", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.ObjectMeta.Namespace,
				Name:      workspaceObj.ObjectMeta.Name,
			}, workspaceObj, &client.GetOptions{})

			if err != nil {
				return false
			}

			_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == string(kaitov1beta1.WorkspaceConditionTypeSucceeded) &&
					condition.Status == metav1.ConditionTrue
			})
			return conditionFound
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for workspace to be ready")
	})
}

func createLocalEmbeddingKaitoVLLMRAGEngine(baseURL, llmPath string) *kaitov1alpha1.RAGEngine {
	ragEngineObj := &kaitov1alpha1.RAGEngine{}
	serviceURL := fmt.Sprintf("http://%s/%s", baseURL, llmPath)
	By("Creating RAG with localembedding and kaito vllm inference", func() {
		uniqueID := fmt.Sprint("rag-", rand.Intn(1000))
		ragEngineObj = GenerateLocalEmbeddingRAGEngineManifest(uniqueID, namespaceName, "Standard_NV36ads_A10_v5", "BAAI/bge-small-en-v1.5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"apps": "phi-4"},
			},
			&kaitov1alpha1.InferenceServiceSpec{
				URL: serviceURL,
			},
		)

		createAndValidateRAGEngine(ragEngineObj)
	})
	return ragEngineObj
}

func createLocalPreferredNodesRAGEngine(baseURL, preferredNode string) *kaitov1alpha1.RAGEngine {
	ragEngineObj := &kaitov1alpha1.RAGEngine{}
	serviceURL := fmt.Sprintf("http://%s/v1/completions", baseURL)
	By("Creating RAG with localembedding and kaito vllm inference", func() {
		uniqueID := fmt.Sprint("rag-", rand.Intn(1000))
		ragEngineObj = GenerateLocalEmbeddingRAGEngineManifestWithPreferredNodes(uniqueID, namespaceName, preferredNode, "BAAI/bge-small-en-v1.5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"apps": "phi-3"},
			},
			&kaitov1alpha1.InferenceServiceSpec{
				URL: serviceURL,
			},
		)

		createAndValidateRAGEngine(ragEngineObj)
	})
	return ragEngineObj
}

func createLocalEmbeddingHFURLRAGEngine() *kaitov1alpha1.RAGEngine {
	ragEngineObj := &kaitov1alpha1.RAGEngine{}
	hfURL := "https://router.huggingface.co/featherless-ai/v1/chat/completions"
	By("Creating RAG with localembedding and huggingface API", func() {
		uniqueID := fmt.Sprint("rag-", rand.Intn(1000))
		ragEngineObj = GenerateLocalEmbeddingRAGEngineManifest(uniqueID, namespaceName, "Standard_NV36ads_A10_v5", "BAAI/bge-small-en-v1.5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"apps": "phi-3"},
			},
			&kaitov1alpha1.InferenceServiceSpec{
				URL:          hfURL,
				AccessSecret: "huggingface-token",
			},
		)

		createAndValidateRAGEngine(ragEngineObj)
	})
	return ragEngineObj
}

func cleanupResources(
	workspaceObj *kaitov1beta1.Workspace,
	ragengineObj *kaitov1alpha1.RAGEngine,
) {
	By("Cleaning up resources", func() {
		if !CurrentSpecReport().Failed() {
			err := deleteRAGEngine(ragengineObj)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RAGEngine")

			if workspaceObj != nil {
				err = deleteWorkspace(workspaceObj)
				Expect(err).NotTo(HaveOccurred(), "Failed to delete Workspace")
			}
		} else {
			if ragengineObj != nil {
				GinkgoWriter.Printf("Test failed, keep Workspace %s and RAGEngine %s\n",
					workspaceObj.ObjectMeta.Name, ragengineObj.ObjectMeta.Name)
			} else {
				GinkgoWriter.Printf("Test failed, keep Workspace %s\n", workspaceObj.ObjectMeta.Name)
			}
		}
	})
}

// validateWorkspacResourceStatus validates resource status
func validateWorkspaceResourceStatus(workspaceObj *kaitov1beta1.Workspace) {
	By("Checking the resource status", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.ObjectMeta.Namespace,
				Name:      workspaceObj.ObjectMeta.Name,
			}, workspaceObj, &client.GetOptions{})

			if err != nil {
				return false
			}

			_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == string(kaitov1alpha1.ConditionTypeResourceStatus) &&
					condition.Status == metav1.ConditionTrue
			})
			return conditionFound
		}, 25*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for workspace resource status to be ready")
	})
}

func deleteRAGEngine(ragengineObj *kaitov1alpha1.RAGEngine) error {
	By("Deleting ragengineObj", func() {
		Eventually(func() error {
			// Check if the workspace exists
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: ragengineObj.ObjectMeta.Namespace,
				Name:      ragengineObj.ObjectMeta.Name,
			}, ragengineObj)

			if errors.IsNotFound(err) {
				GinkgoWriter.Printf("RAGEngine %s does not exist, no need to delete\n", ragengineObj.ObjectMeta.Name)
				return nil
			}
			if err != nil {
				return fmt.Errorf("error checking if ragengine %s exists: %v", ragengineObj.ObjectMeta.Name, err)
			}

			err = utils.TestingCluster.KubeClient.Delete(ctx, ragengineObj, &client.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete ragengine %s: %v", ragengineObj.ObjectMeta.Name, err)
			}
			return nil
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete ragengine")
	})

	return nil
}

// validateInferenceResource validates inference deployment
func validateInferenceandRAGResource(objectMeta metav1.ObjectMeta, expectedReplicas int32, isStatefulSet bool) {
	By("Checking the inference resource", func() {
		Eventually(func() bool {
			var err error
			var readyReplicas int32

			if isStatefulSet {
				sts := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectMeta.Name,
						Namespace: objectMeta.Namespace,
					},
				}
				err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: objectMeta.Namespace,
					Name:      objectMeta.Name,
				}, sts)
				readyReplicas = sts.Status.ReadyReplicas

			} else {
				dep := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectMeta.Name,
						Namespace: objectMeta.Namespace,
					},
				}
				err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: objectMeta.Namespace,
					Name:      objectMeta.Name,
				}, dep)
				readyReplicas = dep.Status.ReadyReplicas
			}

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			if readyReplicas == expectedReplicas {
				return true
			}

			return false
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for inference resource to be ready")
	})
}

func validateAssociatedService(objectMeta metav1.ObjectMeta) {
	serviceName := objectMeta.Name
	serviceNamespace := objectMeta.Namespace

	By(fmt.Sprintf("Checking for service %s in namespace %s", serviceName, serviceNamespace), func() {
		service := &v1.Service{}

		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: serviceNamespace,
				Name:      serviceName,
			}, service)

			if err != nil {
				if errors.IsNotFound(err) {
					GinkgoWriter.Printf("Service %s not found in namespace %s\n", serviceName, serviceNamespace)
				} else {
					GinkgoWriter.Printf("Error fetching service %s in namespace %s: %v\n", serviceName, serviceNamespace, err)
				}
				return false
			}

			GinkgoWriter.Printf("Found service: %s in namespace %s\n", serviceName, serviceNamespace)
			return true
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for service to be created")
	})
}

// validateRAGEngineReadiness validates ragengine conditions
func validateRAGEngineCondition(ragengineObj *kaitov1alpha1.RAGEngine, conditionType string, description string) {
	By(fmt.Sprintf("Checking %s", description), func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: ragengineObj.ObjectMeta.Namespace,
				Name:      ragengineObj.ObjectMeta.Name,
			}, ragengineObj, &client.GetOptions{})
			if err != nil {
				return false
			}
			_, conditionFound := lo.Find(ragengineObj.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == conditionType &&
					condition.Status == metav1.ConditionTrue
			})
			return conditionFound
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), fmt.Sprintf("Failed to wait for %s", description))
	})
}

func deleteWorkspace(workspaceObj *kaitov1beta1.Workspace) error {
	By("Deleting workspace", func() {
		Eventually(func() error {
			// Check if the workspace exists
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.ObjectMeta.Namespace,
				Name:      workspaceObj.ObjectMeta.Name,
			}, workspaceObj)

			if errors.IsNotFound(err) {
				GinkgoWriter.Printf("Workspace %s does not exist, no need to delete\n", workspaceObj.ObjectMeta.Name)
				return nil
			}
			if err != nil {
				return fmt.Errorf("error checking if workspace %s exists: %v", workspaceObj.ObjectMeta.Name, err)
			}

			err = utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete workspace %s: %v", workspaceObj.ObjectMeta.Name, err)
			}
			return nil
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")
	})

	return nil
}

func createAndValidateIndexPod(ragengineObj *kaitov1alpha1.RAGEngine) (map[string]any, error) {
	curlCommand := `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/index \
-H "Content-Type: application/json" \
-d '{
    "index_name": "kaito",
    "documents": [
        {
            "text": "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster",
            "metadata": {"author": "kaito", "category": "kaito"}
        }
    ]
}'`
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("index-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster",
		WaitForRunning:     false,
		ParseJSONResponse:  true,
		JSONStartMarker:    "[",
		JSONEndMarker:      "]",
	}
	return createAndValidateAPIPod(ragengineObj, opts)
}

func createAndValidateUpdateDocumentPod(ragengineObj *kaitov1alpha1.RAGEngine, docID string) error {
	curlCommand := `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/indexes/kaito/documents \
-H "Content-Type: application/json" \
-d '{
    "documents": [
        {
			"doc_id": "` + docID + `",
            "text": "Kaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster. It now has RAG capabilities.",
            "metadata": {"author": "kaito", "category": "ai-ml"}
        }
    ]
}'`
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("update-document-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: `"updated_documents":[{"doc_id":"` + docID + `"`,
		WaitForRunning:     false,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

func createAndValidateDeleteDocumentPod(ragengineObj *kaitov1alpha1.RAGEngine, docID string) error {
	curlCommand := `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/indexes/kaito/documents/delete \
-H "Content-Type: application/json" \
-d '{"doc_ids": ["` + docID + `"]}'`
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("delete-document-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: `"deleted_doc_ids":["` + docID + `"]`,
		WaitForRunning:     false,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

func createAndValidateDeleteIndexPod(ragengineObj *kaitov1alpha1.RAGEngine) error {
	curlCommand := `curl -X DELETE ` + ragengineObj.ObjectMeta.Name + `:80/indexes/kaito \
-H "Content-Type: application/json"`
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("delete-index-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: "Successfully deleted index kaito",
		WaitForRunning:     false,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

func createAndValidateQueryPod(ragengineObj *kaitov1alpha1.RAGEngine, expectedSearchQueries string, remote bool) error {
	var curlCommand string
	// Note: Request without model specified should still succeed with vLLM. As model name is dynamically fetched.
	if remote {
		curlCommand = `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/query \
-H "Content-Type: application/json" \
-d '{
	"index_name": "kaito",
	"model": "HuggingFaceH4/zephyr-7b-beta",
    "query": "what is kaito?",
    "llm_params": {
      "max_tokens": 50,
      "temperature": 0
    }
}'`
	} else {
		curlCommand = `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/query \
-H "Content-Type: application/json" \
-d '{
	"index_name": "kaito",
    "model": "phi-3-mini-128k-instruct",
    "query": "what is kaito?",
    "llm_params": {
      "max_tokens": 50,
      "temperature": 0
    }
}'`
	}
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("query-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: expectedSearchQueries,
		WaitForRunning:     true,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

func createAndValidateQueryChatMessagesPod(ragengineObj *kaitov1alpha1.RAGEngine, expectedSearchQueries string, remote bool) error {
	var curlCommand string
	// Note: Request without model specified should still succeed with vLLM. As model name is dynamically fetched.
	if remote {
		curlCommand = `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/v1/chat/completions \
-H "Content-Type: application/json" \
-d '{
	"index_name": "kaito",
	"model": "HuggingFaceH4/zephyr-7b-beta",
    "messages": [
		{
			"role": "user",
			"content": "what is kaito?"
		}
	],
    "max_tokens": 50,
    "temperature": 0
}'`
	} else {
		curlCommand = `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/v1/chat/completions \
-H "Content-Type: application/json" \
-d '{
	"index_name": "kaito",
    "model": "phi-3-mini-128k-instruct",
    "messages": [
		{
			"role": "user",
			"content": "what is kaito?"
		}
	],
    "max_tokens": 50,
    "temperature": 0
}'`
	}
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("chat-completions-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: expectedSearchQueries,
		WaitForRunning:     true,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

func createAndValidatePersistPod(ragengineObj *kaitov1alpha1.RAGEngine, expectedPersistResult string) error {
	curlCommand := `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/persist/kaito`
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("persist-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: expectedPersistResult,
		WaitForRunning:     true,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

func createAndValidateLoadPod(ragengineObj *kaitov1alpha1.RAGEngine, expectedLoadResult string) error {
	curlCommand := `curl -X POST ` + ragengineObj.ObjectMeta.Name + `:80/load/kaito?overwrite=True`
	opts := PodValidationOptions{
		PodName:            fmt.Sprintf("load-pod-%s", utils.GenerateRandomString()),
		CurlCommand:        curlCommand,
		Namespace:          ragengineObj.ObjectMeta.Namespace,
		ExpectedLogContent: expectedLoadResult,
		WaitForRunning:     true,
		ParseJSONResponse:  false,
	}
	_, err := createAndValidateAPIPod(ragengineObj, opts)
	return err
}

// PodValidationOptions holds configuration for pod validation
type PodValidationOptions struct {
	PodName            string
	CurlCommand        string
	Namespace          string
	ExpectedLogContent string
	WaitForRunning     bool
	ParseJSONResponse  bool
	JSONStartMarker    string
	JSONEndMarker      string
}

// createAndValidateAPIPod is a generic function to create and validate API test pods
func createAndValidateAPIPod(ragengineObj *kaitov1alpha1.RAGEngine, opts PodValidationOptions) (map[string]any, error) {
	var jsonResp []map[string]any

	By(fmt.Sprintf("Creating %s", opts.PodName), func() {
		pod := GenerateCURLPodManifest(opts.PodName, opts.CurlCommand, opts.Namespace)
		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Create(ctx, pod, &client.CreateOptions{})
			if err != nil {
				GinkgoWriter.Printf("Failed to create pod %s: %v\n", opts.PodName, err)
				return err
			}
			return nil
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), fmt.Sprintf("Failed to create %s", opts.PodName))
	})

	if opts.WaitForRunning {
		By(fmt.Sprintf("Waiting for %s to be running", opts.PodName), func() {
			Eventually(func() bool {
				pod := &v1.Pod{}
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: opts.Namespace,
					Name:      opts.PodName,
				}, pod)
				if err != nil {
					GinkgoWriter.Printf("Failed to get pod %s: %v\n", opts.PodName, err)
					return false
				}
				return pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded
			}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), fmt.Sprintf("%s did not reach Running or Succeeded state", opts.PodName))
		})
	}

	By(fmt.Sprintf("Checking the %s logs", opts.PodName), func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				GinkgoWriter.Printf("Failed to create core client: %v\n", err)
				return false
			}

			logs, err := utils.GetPodLogs(coreClient, opts.Namespace, opts.PodName, "")
			if err != nil {
				GinkgoWriter.Printf("Failed to get logs from pod %s: %v\n", opts.PodName, err)
				return false
			}

			GinkgoWriter.Printf("%s logs: %s\n", opts.PodName, logs)

			if opts.ParseJSONResponse {
				startMarker := opts.JSONStartMarker
				endMarker := opts.JSONEndMarker
				if startMarker == "" {
					startMarker = "["
				}
				if endMarker == "" {
					endMarker = "]"
				}

				startIndex := strings.Index(logs, startMarker)
				endIndex := strings.LastIndex(logs, endMarker)
				if startIndex == -1 || endIndex == -1 || startIndex >= endIndex {
					GinkgoWriter.Printf("Invalid JSON format in pod %s: %s\n", opts.PodName, logs)
					return false
				}

				apiResp := logs[startIndex : endIndex+1]
				GinkgoWriter.Printf("Parsed API response: %s\n", apiResp)

				err = json.Unmarshal([]byte(apiResp), &jsonResp)
				if err != nil {
					GinkgoWriter.Printf("Failed to unmarshal pod logs to JSON response %s: %v\n", opts.PodName, err)
					return false
				}

				if len(jsonResp) == 0 {
					GinkgoWriter.Printf("No JSON response found in pod %s\n", opts.PodName)
					return false
				}
			}

			return strings.Contains(logs, opts.ExpectedLogContent)
		}, 4*time.Minute, utils.PollInterval).Should(BeTrue(), fmt.Sprintf("Failed to wait for %s logs to be ready", opts.PodName))
	})

	if opts.ParseJSONResponse && len(jsonResp) > 0 {
		return jsonResp[0], nil
	}
	return nil, nil
}

func GenerateCURLPodManifest(podName, curlCommand, namespace string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			Containers: []v1.Container{
				{
					Name:    "curl",
					Image:   "curlimages/curl:latest",
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{curlCommand},
				},
			},
		},
	}
}

func createAndValidateSecret() {
	hfToken := os.Getenv("HF_TOKEN")
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "huggingface-token",
			Namespace: namespaceName,
		},
		Data: map[string][]byte{
			"LLM_ACCESS_SECRET": []byte(hfToken),
		},
		Type: v1.SecretTypeOpaque,
	}
	By("Creating secret", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, secret, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create secret   %s", secret.Name)

		By("Validating secret creation", func() {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: secret.Namespace,
				Name:      secret.Name,
			}, secret, &client.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
}

func getRagPoolNode() (string, error) {
	nodeList := &v1.NodeList{}
	err := utils.TestingCluster.KubeClient.List(ctx, nodeList)
	if err != nil {
		return "", fmt.Errorf("failed to list nodes: %v", err)
	}

	for _, node := range nodeList.Items {
		if strings.Contains(node.Name, "ragpool") {
			return node.Name, nil
		}
	}

	return "", fmt.Errorf("no node containing 'ragpool' found")
}
