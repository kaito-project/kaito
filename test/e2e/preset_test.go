// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package e2e

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/test/e2e/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	PresetLlama2AChat            = "llama-2-7b-chat"
	PresetLlama2BChat            = "llama-2-13b-chat"
	PresetFalcon7BModel          = "falcon-7b"
	PresetFalcon40BModel         = "falcon-40b"
	PresetMistral7BInstructModel = "mistral-7b-instruct"
	PresetQwen2_5Coder7BModel    = "qwen2.5-coder-7b-instruct"
	PresetPhi2Model              = "phi-2"
	PresetPhi3Mini128kModel      = "phi-3-mini-128k-instruct"
	WorkspaceHashAnnotation      = "workspace.kaito.io/hash"
	// WorkspaceRevisionAnnotation represents the revision number of the workload managed by the workspace
	WorkspaceRevisionAnnotation = "workspace.kaito.io/revision"
)

var (
	datasetImageName1     = "e2e-dataset"
	fullDatasetImageName1 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + datasetImageName1 + ":0.0.1"
	datasetImageName2     = "e2e-dataset2"
	fullDatasetImageName2 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + datasetImageName2 + ":0.0.1"
)

func loadTestEnvVars() {
	var err error
	runLlama13B, err = strconv.ParseBool(os.Getenv("RUN_LLAMA_13B"))
	if err != nil {
		fmt.Print("Error: RUN_LLAMA_13B ENV Variable not set")
		runLlama13B = false
	}

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

func createCustomWorkspaceWithAdapter(numOfNode int, validAdapters []kaitov1alpha1.AdapterSpec) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace with adapter", func() {
		uniqueID := fmt.Sprint("preset-falcon-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "custom-preset-e2e-test-falcon"},
			}, nil, PresetFalcon7BModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, validAdapters)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func updateCustomWorkspaceWithAdapter(workspaceObj *kaitov1alpha1.Workspace, validAdapters []kaitov1alpha1.AdapterSpec) *kaitov1alpha1.Workspace {
	By("Updating a workspace with adapter", func() {
		workspaceObj.Inference.Adapters = validAdapters

		By("Updating workspace", func() {
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Update(ctx, workspaceObj)
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to update workspace %s", workspaceObj.Name)

			By("Validating workspace update", func() {
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: workspaceObj.Namespace,
					Name:      workspaceObj.Name,
				}, workspaceObj, &client.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
	return workspaceObj
}

func createFalconWorkspaceWithPresetPublicMode(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Falcon 7B preset public mode", func() {
		uniqueID := fmt.Sprint("preset-falcon-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-falcon"},
			}, nil, PresetFalcon7BModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMistralWorkspaceWithPresetPublicMode(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Mistral 7B preset public mode", func() {
		uniqueID := fmt.Sprint("preset-mistral-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-mistral"},
			}, nil, PresetMistral7BInstructModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi2WorkspaceWithPresetPublicMode(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Phi 2 preset public mode", func() {
		uniqueID := fmt.Sprint("preset-phi2-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-2"},
			}, nil, PresetPhi2Model, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createLlama7BWorkspaceWithPresetPrivateMode(registry, registrySecret, imageVersion string, numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Llama 7B Chat preset private mode", func() {
		uniqueID := fmt.Sprint("preset-llama-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, fmt.Sprintf("%s/%s:%s", registry, PresetLlama2AChat, imageVersion),
			numOfNode, "Standard_NC12s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "private-preset-e2e-test-llama-2-7b"},
			}, nil, PresetLlama2AChat, kaitov1alpha1.ModelImageAccessModePrivate, []string{registrySecret}, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createLlama13BWorkspaceWithPresetPrivateMode(registry, registrySecret, imageVersion string, numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Llama 13B Chat preset private mode", func() {
		uniqueID := fmt.Sprint("preset-llama-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, fmt.Sprintf("%s/%s:%s", registry, PresetLlama2BChat, imageVersion),
			numOfNode, "Standard_NC12s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "private-preset-e2e-test-llama-2-13b"},
			}, nil, PresetLlama2BChat, kaitov1alpha1.ModelImageAccessModePrivate, []string{registrySecret}, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createCustomWorkspaceWithPresetCustomMode(imageName string, numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with custom workspace mode", func() {
		uniqueID := fmt.Sprint("preset-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "",
			numOfNode, "Standard_D4s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "private-preset-e2e-test-custom"},
			}, nil, "", utils.InferenceModeCustomTemplate, nil, utils.GeneratePodTemplate(uniqueID, namespaceName, imageName, nil), nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi3WorkspaceWithPresetPublicMode(numOfNode int) *kaitov1alpha1.Workspace {
	workspaceObj := &kaitov1alpha1.Workspace{}
	By("Creating a workspace CR with Phi-3-mini-128k-instruct preset public mode", func() {
		uniqueID := fmt.Sprint("preset-phi3-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "",
			numOfNode, "Standard_NC6s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-3-mini-128k-instruct"},
			}, nil, PresetPhi3Mini128kModel, kaitov1alpha1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createCustomTuningConfigMapForE2E() *v1.ConfigMap {
	configMap := utils.GenerateE2ETuningConfigMapManifest(namespaceName)

	By("Creating a custom workspace tuning configmap for E2E", func() {
		createAndValidateConfigMap(configMap)
	})

	return configMap
}

func createAndValidateConfigMap(configMap *v1.ConfigMap) {
	By("Creating ConfigMap", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, configMap, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create ConfigMap %s", configMap.Name)

		By("Validating ConfigMap creation", func() {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: configMap.Namespace,
				Name:      configMap.Name,
			}, configMap, &client.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
}

func createPhi3TuningWorkspaceWithPresetPublicMode(configMapName string, numOfNode int) (*kaitov1alpha1.Workspace, string, string) {
	workspaceObj := &kaitov1alpha1.Workspace{}
	e2eOutputImageName := fmt.Sprintf("adapter-%s-e2e-test", PresetPhi3Mini128kModel)
	e2eOutputImageTag := utils.GenerateRandomString()
	outputRegistryUrl := fmt.Sprintf("%s.azurecr.io/%s:%s", azureClusterName, e2eOutputImageName, e2eOutputImageTag)
	var uniqueID string
	By("Creating a workspace Tuning CR with Phi-3 preset public mode", func() {
		uniqueID = fmt.Sprint("preset-", rand.Intn(1000))
		workspaceObj = utils.GenerateE2ETuningWorkspaceManifest(uniqueID, namespaceName, "",
			fullDatasetImageName1, outputRegistryUrl, numOfNode, "Standard_NC6s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-tuning-falcon"},
			}, nil, PresetPhi3Mini128kModel, kaitov1alpha1.ModelImageAccessModePublic, []string{e2eACRSecret}, configMapName)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj, uniqueID, outputRegistryUrl
}

func createAndValidateWorkspace(workspaceObj *kaitov1alpha1.Workspace) {
	By("Creating workspace", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)

		By("Validating workspace creation", func() {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, workspaceObj, &client.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
}

func updatePhi3TuningWorkspaceWithPresetPublicMode(workspaceObj *kaitov1alpha1.Workspace, datasetImageName string) (*kaitov1alpha1.Workspace, string) {
	e2eOutputImageName := fmt.Sprintf("adapter-%s-e2e-test2", PresetPhi3Mini128kModel)
	e2eOutputImageTag := utils.GenerateRandomString()
	outputRegistryUrl := fmt.Sprintf("%s.azurecr.io/%s:%s", azureClusterName, e2eOutputImageName, e2eOutputImageTag)
	By("Updating a workspace Tuning CR with Phi-3 preset public mode. The update includes the tuning input and output configurations for the workspace.", func() {
		workspaceObj.Tuning.Input = &kaitov1alpha1.DataSource{
			Image: datasetImageName,
		}
		workspaceObj.Tuning.Output = &kaitov1alpha1.DataDestination{
			Image:           outputRegistryUrl,
			ImagePushSecret: e2eACRSecret,
		}
		updateAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj, outputRegistryUrl
}

func updateAndValidateWorkspace(workspaceObj *kaitov1alpha1.Workspace) {
	By("Creating workspace", func() {
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Update(ctx, workspaceObj)
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)

		By("Validating workspace creation", func() {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, workspaceObj, &client.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
}

func copySecretToNamespace(secretName, targetNamespace string) error {
	originalNamespace := "default"
	originalSecret := &v1.Secret{}

	// Fetch the original secret from the default namespace
	err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
		Namespace: originalNamespace,
		Name:      secretName,
	}, originalSecret)
	if err != nil {
		return fmt.Errorf("failed to get secret %s in namespace %s: %v", secretName, originalNamespace, err)
	}

	// Create a copy of the secret for the target namespace
	newSecret := utils.CopySecret(originalSecret, targetNamespace)

	// Create the new secret in the target namespace
	err = utils.TestingCluster.KubeClient.Create(ctx, newSecret)
	if err != nil {
		return fmt.Errorf("failed to create secret %s in namespace %s: %v", secretName, targetNamespace, err)
	}

	return nil
}

// validateResourceStatus validates resource status
func validateResourceStatus(workspaceObj *kaitov1alpha1.Workspace) {
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
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for resource status to be ready")
	})
}

func validateAssociatedService(workspaceObj *kaitov1alpha1.Workspace) {
	serviceName := workspaceObj.Name
	serviceNamespace := workspaceObj.Namespace

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

// validateInferenceResource validates inference deployment
func validateInferenceResource(workspaceObj *kaitov1alpha1.Workspace, expectedReplicas int32, isStatefulSet bool) {
	By("Checking the inference resource", func() {
		Eventually(func() bool {
			var err error
			var readyReplicas int32

			if isStatefulSet {
				sts := &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      workspaceObj.Name,
						Namespace: workspaceObj.Namespace,
					},
				}
				err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: workspaceObj.Namespace,
					Name:      workspaceObj.Name,
				}, sts)
				readyReplicas = sts.Status.ReadyReplicas

			} else {
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

// validateRevision validates the annotations of the workspace and the workload, as well as the corresponding controller revision
func validateRevision(workspaceObj *kaitov1alpha1.Workspace, revisionStr string) {
	By("Checking the revisions of the resources", func() {
		Eventually(func() bool {
			var isWorkloadAnnotationCorrect bool
			if workspaceObj.Inference != nil {
				dep := &appsv1.Deployment{}
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: workspaceObj.Namespace,
					Name:      workspaceObj.Name,
				}, dep)
				if err != nil {
					GinkgoWriter.Printf("Error fetching resource: %v\n", err)
					return false
				}
				isWorkloadAnnotationCorrect = dep.Annotations[WorkspaceRevisionAnnotation] == revisionStr
			} else if workspaceObj.Tuning != nil {
				job := &batchv1.Job{}
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: workspaceObj.Namespace,
					Name:      workspaceObj.Name,
				}, job)
				if err != nil {
					GinkgoWriter.Printf("Error fetching resource: %v\n", err)
					return false
				}
				isWorkloadAnnotationCorrect = job.Annotations[WorkspaceRevisionAnnotation] == revisionStr
			}
			workspaceObjHash := workspaceObj.Annotations[WorkspaceHashAnnotation]
			revision := &appsv1.ControllerRevision{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      fmt.Sprintf("%s-%s", workspaceObj.Name, workspaceObjHash[:5]),
			}, revision)

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			revisionNum, _ := strconv.ParseInt(revisionStr, 10, 64)

			isWorkspaceAnnotationCorrect := workspaceObj.Annotations[WorkspaceRevisionAnnotation] == revisionStr
			isRevisionCorrect := revision.Revision == revisionNum

			return isWorkspaceAnnotationCorrect && isWorkloadAnnotationCorrect && isRevisionCorrect
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for correct revisions to be ready")
	})
}

// validateTuningResource validates tuning deployment
func validateTuningResource(workspaceObj *kaitov1alpha1.Workspace) {
	By("Checking the tuning resource", func() {
		Eventually(func() bool {
			var err error
			var jobFailed, jobSucceeded int32

			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workspaceObj.Name,
					Namespace: workspaceObj.Namespace,
				},
			}
			err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, job)

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			jobFailed = job.Status.Failed
			jobSucceeded = job.Status.Succeeded

			if jobFailed > 0 {
				GinkgoWriter.Printf("Job '%s' is in a failed state.\n", workspaceObj.Name)
				return false
			}

			if jobSucceeded > 0 {
				return true
			}

			return false
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for Tuning resource to be ready")
	})
}

func validateTuningJobInputOutput(workspaceObj *kaitov1alpha1.Workspace, inputImage string, output string) {
	By("Checking the tuning input and output", func() {
		Eventually(func() bool {
			var err error

			job := &batchv1.Job{}
			err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, job)

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			image := job.Spec.Template.Spec.InitContainers[0].Image

			expectedString1 := "docker build -t " + output
			expectedString2 := "if docker push " + output + "; then"

			var sidecarContainer v1.Container

			for _, container := range job.Spec.Template.Spec.Containers {
				if container.Name == "docker-sidecar" {
					sidecarContainer = container
					break
				}
			}
			return image == inputImage && strings.Contains(sidecarContainer.Args[0], expectedString1) && strings.Contains(sidecarContainer.Args[0], expectedString2)
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for Tuning resource to be ready")
	})
}

func validateACRTuningResultsUploaded(workspaceObj *kaitov1alpha1.Workspace, jobName string) {
	coreClient, err := utils.GetK8sClientset()
	if err != nil {
		log.Fatalf("Failed to create core client: %v", err)
	}
	namespace := workspaceObj.Namespace
	podName, err := utils.GetPodNameForJob(coreClient, namespace, jobName)
	if err != nil {
		log.Fatalf("Failed to get pod name for job %s: %v", jobName, err)
	}

	for {
		logs, err := utils.GetPodLogs(coreClient, namespace, podName, "docker-sidecar")
		if err != nil {
			log.Printf("Failed to get logs from pod %s: %v", podName, err)
			time.Sleep(10 * time.Second)
			continue
		}

		if strings.Contains(logs, "Upload complete") {
			fmt.Println("Upload complete")
			break
		}

		time.Sleep(10 * time.Second) // Poll every 10 seconds
	}
}

// validateWorkspaceReadiness validates workspace readiness
func validateWorkspaceReadiness(workspaceObj *kaitov1alpha1.Workspace) {
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
				return condition.Type == string(kaitov1alpha1.WorkspaceConditionTypeSucceeded) &&
					condition.Status == metav1.ConditionTrue
			})
			return conditionFound
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for workspace to be ready")
	})
}

func cleanupResources(workspaceObj *kaitov1alpha1.Workspace) {
	By("Cleaning up resources", func() {
		if !CurrentSpecReport().Failed() {
			// delete workspace
			err := deleteWorkspace(workspaceObj)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete workspace")
		} else {
			GinkgoWriter.Printf("test failed, keep %s \n", workspaceObj.Name)
		}
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

var runLlama13B bool
var aiModelsRegistry string
var aiModelsRegistrySecret string
var e2eACRSecret string
var supportedModelsYamlPath string
var modelInfo map[string]string
var azureClusterName string

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

	It("should create a mistral workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createMistralWorkspaceWithPresetPublicMode(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a Phi-2 workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createPhi2WorkspaceWithPresetPublicMode(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a falcon workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createFalconWorkspaceWithPresetPublicMode(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a llama 7b workspace with preset private mode successfully", func() {
		numOfNode := 1
		modelVersion, ok := modelInfo[PresetLlama2AChat]
		if !ok {
			Fail(fmt.Sprintf("Model version for %s not found", PresetLlama2AChat))
		}
		workspaceObj := createLlama7BWorkspaceWithPresetPrivateMode(aiModelsRegistry, aiModelsRegistrySecret, modelVersion, numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a llama 13b workspace with preset private mode successfully", func() {
		if !runLlama13B {
			Skip("Skipping llama 13b workspace test")
		}
		numOfNode := 2
		modelVersion, ok := modelInfo[PresetLlama2BChat]
		if !ok {
			Fail(fmt.Sprintf("Model version for %s not found", PresetLlama2AChat))
		}
		workspaceObj := createLlama13BWorkspaceWithPresetPrivateMode(aiModelsRegistry, aiModelsRegistrySecret, modelVersion, numOfNode)

		defer cleanupResources(workspaceObj)

		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), true)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a custom template workspace successfully", func() {
		numOfNode := 1
		imageName := "nginx:latest"
		workspaceObj := createCustomWorkspaceWithPresetCustomMode(imageName, numOfNode)

		defer cleanupResources(workspaceObj)

		time.Sleep(30 * time.Second)
		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a Phi-3-mini-128k-instruct workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createPhi3WorkspaceWithPresetPublicMode(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode), false)

		validateWorkspaceReadiness(workspaceObj)
	})

	It("should create a workspace for tuning successfully, and update the workspace with another dataset and output image", func() {
		numOfNode := 1
		err := copySecretToNamespace(e2eACRSecret, namespaceName)
		if err != nil {
			log.Fatalf("Error copying secret: %v", err)
		}
		configMap := createCustomTuningConfigMapForE2E()
		workspaceObj, jobName, outputRegistryUrl1 := createPhi3TuningWorkspaceWithPresetPublicMode(configMap.Name, numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)
		validateTuningResource(workspaceObj)

		validateACRTuningResultsUploaded(workspaceObj, jobName)

		validateWorkspaceReadiness(workspaceObj)

		validateTuningJobInputOutput(workspaceObj, fullDatasetImageName1, outputRegistryUrl1)

		validateRevision(workspaceObj, "1")

		workspaceObj, outputRegistryUrl2 := updatePhi3TuningWorkspaceWithPresetPublicMode(workspaceObj, fullDatasetImageName2)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)
		validateTuningResource(workspaceObj)

		validateACRTuningResultsUploaded(workspaceObj, jobName)

		validateWorkspaceReadiness(workspaceObj)

		validateTuningJobInputOutput(workspaceObj, fullDatasetImageName2, outputRegistryUrl2)

		validateRevision(workspaceObj, "2")
	})

})

func validateCreateNode(workspaceObj *kaitov1alpha1.Workspace, numOfNode int) {
	utils.ValidateNodeClaimCreation(ctx, workspaceObj, numOfNode)
}

// validateInferenceConfig validates that the inference config exists and contains data
func validateInferenceConfig(workspaceObj *kaitov1alpha1.Workspace) {
	By("Checking the inference config exists", func() {
		Eventually(func() bool {
			configMap := &v1.ConfigMap{}
			configName := kaitov1alpha1.DefaultInferenceConfigTemplate
			if workspaceObj.Inference.Config != "" {
				configName = workspaceObj.Inference.Config
			}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      configName,
			}, configMap)

			if err != nil {
				GinkgoWriter.Printf("Error fetching config: %v\n", err)
				return false
			}

			return len(configMap.Data) > 0
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for inference config to be ready")
	})
}
