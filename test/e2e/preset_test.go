// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

const (
	PresetLlama2AChat                   = "llama-2-7b-chat"
	PresetLlama2BChat                   = "llama-2-13b-chat"
	PresetFalcon7BModel                 = "falcon-7b"
	PresetFalcon40BModel                = "falcon-40b"
	PresetMistral7BInstructModel        = "mistral-7b-instruct"
	PresetQwen2_5Coder7BModel           = "qwen2.5-coder-7b-instruct"
	PresetPhi2Model                     = "phi-2"
	PresetPhi3Mini128kModel             = "phi-3-mini-128k-instruct"
	PresetDeepSeekR1DistillLlama8BModel = "deepseek-r1-distill-llama-8b"
	PresetDeepSeekR1DistillQwen14BModel = "deepseek-r1-distill-qwen-14b"
	PresetPhi4MiniModel                 = "phi-4-mini-instruct"
	WorkspaceHashAnnotation             = "workspace.kaito.io/hash"
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

func createCustomWorkspaceWithAdapter(numOfNode int, validAdapters []kaitov1beta1.AdapterSpec) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace with adapter", func() {
		uniqueID := fmt.Sprint("preset-falcon-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "custom-preset-e2e-test-falcon"},
			}, nil, PresetFalcon7BModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, validAdapters)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func updateCustomWorkspaceWithAdapter(workspaceObj *kaitov1beta1.Workspace, validAdapters []kaitov1beta1.AdapterSpec) *kaitov1beta1.Workspace {
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

func createFalconWorkspaceWithPresetPublicMode(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Falcon 7B preset public mode", func() {
		uniqueID := fmt.Sprint("preset-falcon-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-falcon"},
			}, nil, PresetFalcon7BModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMistralWorkspaceWithPresetPublicMode(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Mistral 7B preset public mode", func() {
		uniqueID := fmt.Sprint("preset-mistral-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC12s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-mistral"},
			}, nil, PresetMistral7BInstructModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createPhi2WorkspaceWithPresetPublicMode(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Phi 2 preset public mode", func() {
		uniqueID := fmt.Sprint("preset-phi2-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "", numOfNode, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-2"},
			}, nil, PresetPhi2Model, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createLlama7BWorkspaceWithPresetPrivateMode(registry, registrySecret, imageVersion string, numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Llama 7B Chat preset private mode", func() {
		uniqueID := fmt.Sprint("preset-llama-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, fmt.Sprintf("%s/%s:%s", registry, PresetLlama2AChat, imageVersion),
			numOfNode, "Standard_NC12s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "private-preset-e2e-test-llama-2-7b"},
			}, nil, PresetLlama2AChat, kaitov1beta1.ModelImageAccessModePrivate, []string{registrySecret}, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createLlama13BWorkspaceWithPresetPrivateMode(registry, registrySecret, imageVersion string, numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Llama 13B Chat preset private mode", func() {
		uniqueID := fmt.Sprint("preset-llama-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, fmt.Sprintf("%s/%s:%s", registry, PresetLlama2BChat, imageVersion),
			numOfNode, "Standard_NC12s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "private-preset-e2e-test-llama-2-13b"},
			}, nil, PresetLlama2BChat, kaitov1beta1.ModelImageAccessModePrivate, []string{registrySecret}, nil, nil)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createCustomWorkspaceWithPresetCustomMode(imageName string, numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
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

func createPhi3WorkspaceWithPresetPublicMode(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Phi-3-mini-128k-instruct preset public mode", func() {
		uniqueID := fmt.Sprint("preset-phi3-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifest(uniqueID, namespaceName, "",
			numOfNode, "Standard_NC6s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-phi-3-mini-128k-instruct"},
			}, nil, PresetPhi3Mini128kModel, kaitov1beta1.ModelImageAccessModePublic, nil, nil, nil)

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

func createPhi3TuningWorkspaceWithPresetPublicMode(configMapName string, numOfNode int) (*kaitov1beta1.Workspace, string, string) {
	workspaceObj := &kaitov1beta1.Workspace{}
	e2eOutputImageName := fmt.Sprintf("adapter-%s-e2e-test", PresetPhi3Mini128kModel)
	e2eOutputImageTag := utils.GenerateRandomString()
	outputRegistryUrl := fmt.Sprintf("%s.azurecr.io/%s:%s", azureClusterName, e2eOutputImageName, e2eOutputImageTag)
	var uniqueID string
	By("Creating a workspace Tuning CR with Phi-3 preset public mode", func() {
		uniqueID = fmt.Sprint("preset-", rand.Intn(1000))
		workspaceObj = utils.GenerateE2ETuningWorkspaceManifest(uniqueID, namespaceName, "",
			fullDatasetImageName1, outputRegistryUrl, numOfNode, "Standard_NC6s_v3", &metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-tuning-falcon"},
			}, nil, PresetPhi3Mini128kModel, kaitov1beta1.ModelImageAccessModePublic, []string{e2eACRSecret}, configMapName)

		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj, uniqueID, outputRegistryUrl
}

func createAndValidateWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	By("Creating workspace", func() {
		createConfigForWorkspace(workspaceObj)
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

func createConfigForWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	if workspaceObj.Inference == nil || workspaceObj.Resource.InstanceType == "" {
		return
	}

	handler := sku.GetCloudSKUHandler(consts.AzureCloudName)
	gpuConfig := handler.GetGPUConfigBySKU(workspaceObj.Resource.InstanceType)
	if gpuConfig == nil || gpuConfig.GPUCount <= 1 {
		return
	}

	By("Creating config file", func() {
		cm := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inference-config",
				Namespace: workspaceObj.Namespace,
			},
			Data: map[string]string{
				"inference_config.yaml": `
vllm:
  max-model-len: 1024
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

func updatePhi3TuningWorkspaceWithPresetPublicMode(workspaceObj *kaitov1beta1.Workspace, datasetImageName string) (*kaitov1beta1.Workspace, string) {
	e2eOutputImageName := fmt.Sprintf("adapter-%s-e2e-test2", PresetPhi3Mini128kModel)
	e2eOutputImageTag := utils.GenerateRandomString()
	outputRegistryUrl := fmt.Sprintf("%s.azurecr.io/%s:%s", azureClusterName, e2eOutputImageName, e2eOutputImageTag)
	By("Updating a workspace Tuning CR with Phi-3 preset public mode. The update includes the tuning input and output configurations for the workspace.", func() {
		workspaceObj.Tuning.Input = &kaitov1beta1.DataSource{
			Image: datasetImageName,
		}
		workspaceObj.Tuning.Output = &kaitov1beta1.DataDestination{
			Image:           outputRegistryUrl,
			ImagePushSecret: e2eACRSecret,
		}
		updateAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj, outputRegistryUrl
}

func updateAndValidateWorkspace(workspaceObj *kaitov1beta1.Workspace) {
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
func validateResourceStatus(workspaceObj *kaitov1beta1.Workspace) {
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
				return condition.Type == string(kaitov1beta1.ConditionTypeResourceStatus) &&
					condition.Status == metav1.ConditionTrue
			})
			return conditionFound
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for resource status to be ready")
	})
}

func validateAssociatedService(workspaceObj *kaitov1beta1.Workspace) {
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
func validateInferenceResource(workspaceObj *kaitov1beta1.Workspace, expectedReplicas int32, isStatefulSet bool) {
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
func validateRevision(workspaceObj *kaitov1beta1.Workspace, revisionStr string) {
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
func validateTuningResource(workspaceObj *kaitov1beta1.Workspace) {
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

func validateTuningJobInputOutput(workspaceObj *kaitov1beta1.Workspace, inputImage string, outputImage string) {
	By("Checking the tuning input and output", func() {
		Eventually(func() bool {
			var job batchv1.Job
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKeyFromObject(workspaceObj), &job); err != nil {
				if client.IgnoreNotFound(err) == nil {
					GinkgoWriter.Printf("Job not found: %v\n", err)
					return false
				}

				Expect(err).NotTo(HaveOccurred())
			}

			var pullerContainer *v1.Container
			for _, container := range job.Spec.Template.Spec.InitContainers {
				if strings.HasPrefix(container.Name, "puller") {
					pullerContainer = &container
					break
				}
			}

			pullerSH := pullerContainer.Args[0]
			if !strings.Contains(pullerSH, fmt.Sprintf("\n[ ! -z \"${IMG_REF}\" ] || IMG_REF='%s'\n", inputImage)) {
				GinkgoWriter.Printf("Unexpected pullerSH: %s\n", pullerSH)
				return false
			}

			var pusherContainer *v1.Container
			for _, container := range job.Spec.Template.Spec.Containers {
				if strings.HasPrefix(container.Name, "pusher") {
					pusherContainer = &container
					break
				}
			}

			pusherSH := pusherContainer.Args[0]
			if !strings.Contains(pusherSH, fmt.Sprintf("\n[ ! -z \"${IMG_REF}\" ] || IMG_REF='%s'\n", outputImage)) {
				GinkgoWriter.Printf("Unexpected pusherSH: %s\n", pullerSH)
				return false
			}

			return true
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for Tuning resource to be ready")
	})
}

func validateACRTuningResultsUploaded(workspaceObj *kaitov1beta1.Workspace, jobName string) {
	coreClient, err := utils.GetK8sClientset()
	if err != nil {
		Fail(fmt.Sprintf("Failed to create core client: %v", err))
	}

	for {
		job, err := coreClient.BatchV1().Jobs(workspaceObj.Namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			Fail(fmt.Sprintf("Failed to get job %s: %v", jobName, err))
		}

		if job.Status.CompletionTime.IsZero() {
			time.Sleep(10 * time.Second) // Poll every 10 seconds
			continue
		}

		if job.Status.Succeeded == 0 {
			Fail("Job did not succeed")
			break
		}

		GinkgoWriter.Println("Upload complete")
		break
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

func validateModelsEndpoint(workspaceObj *kaitov1beta1.Workspace) {
	deploymentName := workspaceObj.Name
	modelName := workspaceObj.Inference.Preset.Name
	expectedModelID := fmt.Sprintf(`"id":"%s"`, modelName)
	execOption := corev1.PodExecOptions{
		Command:   []string{"bash", "-c", fmt.Sprintf(`apt-get update && apt-get install curl -y; curl -s -X GET http://%s.%s.svc.cluster.local:80/v1/models | grep -e '%s'`, workspaceObj.Name, workspaceObj.Namespace, expectedModelID)},
		Container: deploymentName,
		Stdout:    true,
		Stderr:    true,
	}

	By("Validating the /v1/models endpoint", func() {
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
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for /v1/models endpoint to be ready")
	})
}

func validateCompletionsEndpoint(workspaceObj *kaitov1beta1.Workspace) {
	deploymentName := workspaceObj.Name
	expectedCompletion := `"object":"text_completion"`
	execOption := corev1.PodExecOptions{
		Command:   []string{"bash", "-c", fmt.Sprintf(`apt-get update && apt-get install curl -y; curl -s -X POST -H "Content-Type: application/json" -d '{"model":"%s","prompt":"What is Kubernetes?","max_tokens":7,"temperature":0}' http://%s.%s.svc.cluster.local:80/v1/completions | grep -e '%s'`, workspaceObj.Inference.Preset.Name, workspaceObj.Name, workspaceObj.Namespace, expectedCompletion)},
		Container: deploymentName,
		Stdout:    true,
		Stderr:    true,
	}

	By("Validating the /v1/completions endpoint", func() {
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
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for /v1/completions endpoint to be ready")
	})
}

func cleanupResources(workspaceObj *kaitov1beta1.Workspace) {
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

	It("should create a custom template workspace successfully", utils.GinkgoLabelFastCheck, func() {
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

	It("should create a Phi-3-mini-128k-instruct workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
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

	It("should create a workspace for tuning successfully, and update the workspace with another dataset and output image", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
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

func validateCreateNode(workspaceObj *kaitov1beta1.Workspace, numOfNode int) {
	utils.ValidateNodeClaimCreation(ctx, workspaceObj, numOfNode)
}

// validateInferenceConfig validates that the inference config exists and contains data
func validateInferenceConfig(workspaceObj *kaitov1beta1.Workspace) {
	By("Checking the inference config exists", func() {
		Eventually(func() bool {
			configMap := &v1.ConfigMap{}
			configName := kaitov1beta1.DefaultInferenceConfigTemplate
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
