package e2e

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
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
			utils.PrintPodLogsOnFailure("gpu-provisioner", "") // The gpu-provisioner Pod
			Fail("Fail threshold reached")
		}
	})

	It("should create RAG with localembedding and kaito VLLM workspace successfully", func() {
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

		ragengineObj := createLocalEmbeddingKaitoVLLMRAGEngine(clusterIP)

		defer cleanupResources(workspaceObj, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfReplica), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

		createIndexPod(ragengineObj)
		createAndValidateQueryPod(ragengineObj)

	})

	It("should create RAG with localembedding and huggingface API successfully", func() {
		numOfReplica := 1

		ragengineObj := createLocalEmbeddingHFURLRAGEngine()

		defer cleanupResources(nil, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfReplica), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

		createIndexPod(ragengineObj)
		//TODO: add the createAndValidateQueryPod here in the next PR

	})
})

func createPhi3WorkspaceWithPresetPublicModeAndVLLM(numOfReplica int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Phi-3-mini-128k-instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-phi3-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfReplica, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "rag-e2e-test-phi-3-mini-128k-instruct-vllm"},
			}, nil, PresetPhi3Mini128kModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createAndValidateWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	By("Creating workspace", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)
	})
}

func createAndValidateRAGEngine(ragEngineObj *kaitov1alpha1.RAGEngine) {
	By("Creating ragEngine", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, ragEngineObj, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create ragEngine   %s", ragEngineObj.Name)

		By("Validating ragEngine creation", func() {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: ragEngineObj.Namespace,
				Name:      ragEngineObj.Name,
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

// validateWorkspaceReadiness validates workspace readiness
func validateWorkspaceReadiness(workspaceObj *kaitov1beta1.Workspace) {
	By("Checking the workspace status is ready", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
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

func createLocalEmbeddingKaitoVLLMRAGEngine(baseURL string) *kaitov1alpha1.RAGEngine {
	ragEngineObj := &kaitov1alpha1.RAGEngine{}
	serviceURL := fmt.Sprintf("http://%s/v1/completions", baseURL)
	By("Creating RAG with localembedding and kaito vllm inference", func() {
		uniqueID := fmt.Sprint("rag-", rand.Intn(1000))
		ragEngineObj = GenerateLocalEmbeddingRAGEngineManifest(uniqueID, namespaceName, "Standard_NC24s_v3", "BAAI/bge-small-en-v1.5",
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
	hfURL := "https://api-inference.huggingface.co/models/HuggingFaceH4/zephyr-7b-beta/v1/completions"
	By("Creating RAG with localembedding and huggingface API", func() {
		uniqueID := fmt.Sprint("rag-", rand.Intn(1000))
		ragEngineObj = GenerateLocalEmbeddingRAGEngineManifest(uniqueID, namespaceName, "Standard_NC12s_v3", "BAAI/bge-small-en-v1.5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"apps": "phi-3"},
			},
			&kaitov1alpha1.InferenceServiceSpec{
				URL: hfURL,
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
					workspaceObj.Name, ragengineObj.Name)
			} else {
				GinkgoWriter.Printf("Test failed, keep Workspace %s\n", workspaceObj.Name)
			}
		}
	})
}

// validateWorkspacResourceStatus validates resource status
func validateWorkspaceResourceStatus(workspaceObj *kaitov1beta1.Workspace) {
	By("Checking the resource status", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
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
				Namespace: ragengineObj.Namespace,
				Name:      ragengineObj.Name,
			}, ragengineObj)

			if errors.IsNotFound(err) {
				GinkgoWriter.Printf("RAGEngine %s does not exist, no need to delete\n", ragengineObj.Name)
				return nil
			}
			if err != nil {
				return fmt.Errorf("error checking if ragengine %s exists: %v", ragengineObj.Name, err)
			}

			err = utils.TestingCluster.KubeClient.Delete(ctx, ragengineObj, &client.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete ragengine %s: %v", ragengineObj.Name, err)
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
				Namespace: ragengineObj.Namespace,
				Name:      ragengineObj.Name,
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
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, workspaceObj)

			if errors.IsNotFound(err) {
				GinkgoWriter.Printf("Workspace %s does not exist, no need to delete\n", workspaceObj.Name)
				return nil
			}
			if err != nil {
				return fmt.Errorf("error checking if workspace %s exists: %v", workspaceObj.Name, err)
			}

			err = utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("failed to delete workspace %s: %v", workspaceObj.Name, err)
			}
			return nil
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")
	})

	return nil
}

func createIndexPod(ragengineObj *kaitov1alpha1.RAGEngine) error {
	By("Creating index pod", func() {
		pod := GenerateIndexPodManifest(ragengineObj.Namespace, ragengineObj.Name)
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, pod, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create index pod")
	})
	time.Sleep(60 * time.Second)

	return nil
}

func createAndValidateQueryPod(ragengineObj *kaitov1alpha1.RAGEngine) error {
	By("Creating query pod", func() {
		pod := GenerateQueryPodManifest(ragengineObj.Namespace, ragengineObj.Name)
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, pod, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create query pod")
	})
	time.Sleep(30 * time.Second)
	By("Checking the query logs", func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				GinkgoWriter.Printf("Failed to create core client: %v\n", err)
				return false
			}

			logs, err := utils.GetPodLogs(coreClient, ragengineObj.Namespace, "querypod", "")
			if err != nil {
				GinkgoWriter.Printf("Failed to get logs from pod %s: %v\n", "querypod", err)
				return false
			}

			searchQuerySuccess := "\\nKaito is an operator that automates the AI/ML model inference or tuning workload in a Kubernetes cluster.\\n\\n\\n"

			return strings.Contains(logs, searchQuerySuccess)
		}, 2*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for query logs to be ready")
	})

	return nil
}

func GenerateIndexPodManifest(namespace, serviceName string) *v1.Pod {

	curlCommand := `curl -X POST ` + serviceName + `:80/index \
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

	indexPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "indexpod",
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

	return indexPod
}

func GenerateQueryPodManifest(namespace, serviceName string) *v1.Pod { // TODO: add another model param for the remote inference service in the next PR

	curlCommand := `curl -X POST ` + serviceName + `:80/query \
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

	queryPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "querypod",
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

	return queryPod
}
