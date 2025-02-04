package e2e

import (
	"fmt"
	"math/rand"
	"os"
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
	"github.com/kaito-project/kaito/test/e2e/utils"
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

	It("should create RAG with localembedding and huggingface API successfully", func() {
		numOfNode := 1

		ragengineObj := createLocalEmbeddingHFURLRAGEngine()

		defer cleanupResources(nil, ragengineObj)

		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.ConditionTypeResourceStatus), "ragengineObj resource status to be ready")
		validateAssociatedService(ragengineObj.ObjectMeta)
		validateInferenceandRAGResource(ragengineObj.ObjectMeta, int32(numOfNode), false)
		validateRAGEngineCondition(ragengineObj, string(kaitov1alpha1.RAGEngineConditionTypeSucceeded), "ragengine to be ready")

	})
})

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

func createLocalEmbeddingHFURLRAGEngine() *kaitov1alpha1.RAGEngine {
	ragEngineObj := &kaitov1alpha1.RAGEngine{}
	hfURL := "https://api-inference.huggingface.co/models/HuggingFaceH4/zephyr-7b-beta/v1/completions"
	By("Creating RAG with localembedding and huggingface API", func() {
		uniqueID := fmt.Sprint("rag-", rand.Intn(1000))
		ragEngineObj = GenerateLocalEmbeddingRAGEngineManifest(uniqueID, namespaceName, "Standard_NC6s_v3", "BAAI/bge-small-en-v1.5",
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
	workspaceObj *kaitov1alpha1.Workspace,
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

func deleteWorkspace(workspaceObj *kaitov1alpha1.Workspace) error {
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
