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

package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	pkgscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
)

const (
	ExampleDatasetURL = "https://huggingface.co/datasets/philschmid/dolly-15k-oai-style/resolve/main/data/train-00000-of-00001-54e3756291ca09c6.parquet?download=true"
)

var (
	// PollInterval defines the interval time for a poll operation.
	PollInterval = 2 * time.Second
	// PollTimeout defines the time after which the poll operation times out.
	PollTimeout = 120 * time.Second
)

func GetEnv(envVar string) string {
	env := os.Getenv(envVar)
	if env == "" {
		fmt.Printf("%s is not set or is empty\n", envVar)
		return ""
	}
	return env
}

// GenerateRandomString generates a random number between 0 and 1000 and returns it as a string.
func GenerateRandomString() string {
	newRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	randomNumber := newRand.Intn(1001) // Generate a random number between 0 and 1000
	return fmt.Sprintf("%d", randomNumber)
}

func GetModelConfigInfo(configFilePath string) (map[string]interface{}, error) {
	var data map[string]interface{}

	yamlData, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading YAML file: %w", err)
	}

	err = yaml.Unmarshal(yamlData, &data)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML: %w", err)
	}

	return data, nil
}

func GetPodNameForJob(coreClient *kubernetes.Clientset, namespace, jobName string) (string, error) {
	podList, err := coreClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", err
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s", jobName)
	}

	return podList.Items[0].Name, nil
}

func GetPodNameForDeployment(coreClient *kubernetes.Clientset, namespace, deploymentName string) (string, error) {
	podList, err := coreClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kaito.sh/workspace=%s", deploymentName),
	})
	if err != nil {
		return "", err
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s", deploymentName)
	}

	return podList.Items[0].Name, nil
}

func GetK8sConfig() (*rest.Config, error) {
	var config *rest.Config
	var err error

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" && os.Getenv("KUBERNETES_SERVICE_PORT") != "" {
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatalf("Failed to get in-cluster config: %v", err)
		}
	} else {
		// Use kubeconfig file for local development
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("Failed to load kubeconfig: %v", err)
		}
	}

	return config, err
}

func GetK8sClientset() (*kubernetes.Clientset, error) {
	config, err := GetK8sConfig()
	if err != nil {
		log.Fatalf("Failed to get k8s config: %v", err)
	}
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create core client: %v", err)
	}
	return coreClient, err
}

func GetPodLogs(coreClient *kubernetes.Clientset, namespace, podName, containerName string) (string, error) {
	options := &corev1.PodLogOptions{}
	if containerName != "" {
		options.Container = containerName
	}

	req := coreClient.CoreV1().Pods(namespace).GetLogs(podName, options)
	logs, err := req.Stream(context.Background())
	if err != nil {
		return "", err
	}
	defer logs.Close()

	buf := new(strings.Builder)
	_, err = io.Copy(buf, logs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func ExecSync(ctx context.Context, config *rest.Config, coreClient *kubernetes.Clientset, namespace, podName string, options corev1.PodExecOptions) (string, error) {
	req := coreClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")
	req.VersionedParams(&options, pkgscheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to initialize SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute command %v: %w, stderr: %q", options.Command, err, stderr.String())
	}

	if stderr.Len() > 0 {
		return "", fmt.Errorf("command error: %s", stderr.String())
	}

	return stdout.String(), nil
}

func PrintPodLogsOnFailure(namespace, labelSelector string) {
	coreClient, err := GetK8sClientset()
	if err != nil {
		log.Printf("Failed to create core client: %v", err)
	}
	pods, err := coreClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		log.Printf("Failed to list pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			logs, err := GetPodLogs(coreClient, namespace, pod.Name, container.Name)
			if err != nil {
				log.Printf("Failed to get logs from pod %s, container %s: %v", pod.Name, container.Name, err)
			} else {
				fmt.Printf("Logs from pod %s, container %s:\n%s\n", pod.Name, container.Name, string(logs))
			}
		}
	}
}

func CopySecret(original *corev1.Secret, targetNamespace string) *corev1.Secret {
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      original.Name,
			Namespace: targetNamespace,
		},
		Data: original.Data,
		Type: original.Type,
	}
	return newSecret
}

func ExtractModelVersion(configs map[string]interface{}) (map[string]string, error) {
	modelsInfo := make(map[string]string)
	models, ok := configs["models"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("'models' key not found or is not a slice")
	}

	for _, modelItem := range models {
		model, ok := modelItem.(map[interface{}]interface{})
		if !ok {
			return nil, fmt.Errorf("model item is not a map")
		}

		modelName, ok := model["name"].(string)
		if !ok {
			return nil, fmt.Errorf("model name is not a string or not found")
		}

		modelTag, ok := model["tag"].(string) // Using 'tag' as the version
		if !ok {
			return nil, fmt.Errorf("model version for %s is not a string or not found", modelName)
		}

		modelsInfo[modelName] = modelTag
	}

	return modelsInfo, nil
}

func GenerateInferenceWorkspaceManifest(name, namespace, imageName string, resourceCount int, instanceType string,
	labelSelector *metav1.LabelSelector, preferredNodes []string, presetName kaitov1beta1.ModelName, imagePullSecret []string,
	podTemplate *corev1.PodTemplateSpec, adapters []kaitov1beta1.AdapterSpec, modelAccessSecret string) *kaitov1beta1.Workspace {

	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				kaitov1beta1.AnnotationWorkspaceRuntime: string(model.RuntimeNameHuggingfaceTransformers),
			},
		},
		Resource: kaitov1beta1.ResourceSpec{
			Count:          lo.ToPtr(resourceCount),
			InstanceType:   instanceType,
			LabelSelector:  labelSelector,
			PreferredNodes: preferredNodes,
		},
	}

	var workspaceInference kaitov1beta1.InferenceSpec
	// If presetName is not nil, we are using a preset,
	// otherwise we are using a custom template
	if presetName != "" {
		workspaceInference.Preset = &kaitov1beta1.PresetSpec{
			PresetMeta: kaitov1beta1.PresetMeta{
				Name: presetName,
			},
			PresetOptions: kaitov1beta1.PresetOptions{
				Image:             imageName,
				ImagePullSecrets:  imagePullSecret,
				ModelAccessSecret: modelAccessSecret,
			},
		}
	} else {
		workspaceInference.Template = podTemplate
	}

	if adapters != nil {
		workspaceInference.Adapters = adapters
	}

	workspace.Inference = &workspaceInference

	return workspace
}

func GenerateInferenceWorkspaceManifestWithVLLM(name, namespace, imageName string, resourceCount int, instanceType string,
	labelSelector *metav1.LabelSelector, preferredNodes []string, presetName kaitov1beta1.ModelName, imagePullSecret []string,
	podTemplate *corev1.PodTemplateSpec, adapters []kaitov1beta1.AdapterSpec, modelAccessSecret string) *kaitov1beta1.Workspace {
	workspace := GenerateInferenceWorkspaceManifest(name, namespace, imageName, resourceCount, instanceType,
		labelSelector, preferredNodes, presetName, imagePullSecret, podTemplate, adapters, modelAccessSecret)

	if workspace.Annotations == nil {
		workspace.Annotations = make(map[string]string)
	}
	workspace.Annotations[kaitov1beta1.AnnotationWorkspaceRuntime] = string(model.RuntimeNameVLLM)
	return workspace
}

func GenerateTuningWorkspaceManifest(name, namespace, imageName string, resourceCount int, instanceType string,
	labelSelector *metav1.LabelSelector, preferredNodes []string, input *kaitov1beta1.DataSource,
	output *kaitov1beta1.DataDestination, preset *kaitov1beta1.PresetSpec, method kaitov1beta1.TuningMethod) *kaitov1beta1.Workspace {

	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Resource: kaitov1beta1.ResourceSpec{
			Count:          lo.ToPtr(resourceCount),
			InstanceType:   instanceType,
			LabelSelector:  labelSelector,
			PreferredNodes: preferredNodes,
		},
		Tuning: &kaitov1beta1.TuningSpec{
			Method: method,
			Input:  input,
			Output: output,
			Preset: preset,
		},
	}

	return workspace
}

func GenerateE2ETuningWorkspaceManifest(name, namespace, imageName, datasetImageName, outputRegistry string,
	resourceCount int, instanceType string, labelSelector *metav1.LabelSelector,
	preferredNodes []string, presetName kaitov1beta1.ModelName, imagePullSecret []string,
	customConfigMapName string, datasetVolume *corev1.Volume, outputVolume *corev1.Volume) *kaitov1beta1.Workspace {
	workspace := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Resource: kaitov1beta1.ResourceSpec{
			Count:          lo.ToPtr(resourceCount),
			InstanceType:   instanceType,
			LabelSelector:  labelSelector,
			PreferredNodes: preferredNodes,
		},
	}

	var workspaceTuning kaitov1beta1.TuningSpec
	// If presetName is not nil, we are using a preset,
	// otherwise we are using a custom template
	if presetName != "" {
		workspaceTuning.Preset = &kaitov1beta1.PresetSpec{
			PresetMeta: kaitov1beta1.PresetMeta{
				Name: presetName,
			},
			PresetOptions: kaitov1beta1.PresetOptions{
				Image:            imageName,
				ImagePullSecrets: imagePullSecret,
			},
		}
	}

	workspace.Tuning = &workspaceTuning
	workspace.Tuning.Method = kaitov1beta1.TuningMethodQLora
	if datasetVolume != nil {
		workspace.Tuning.Input = &kaitov1beta1.DataSource{
			Volume: &datasetVolume.VolumeSource,
		}
	} else {
		workspace.Tuning.Input = &kaitov1beta1.DataSource{
			Image:            datasetImageName,
			ImagePullSecrets: imagePullSecret,
		}
	}
	if outputVolume != nil {
		workspace.Tuning.Output = &kaitov1beta1.DataDestination{
			Volume: &outputVolume.VolumeSource,
		}
	} else {
		workspace.Tuning.Output = &kaitov1beta1.DataDestination{
			Image:           outputRegistry,
			ImagePushSecret: imagePullSecret[0],
		}
	}

	if customConfigMapName != "" {
		workspace.Tuning.Config = customConfigMapName
	}

	return workspace
}

// GenerateE2ETuningConfigMapManifest generates a ConfigMap manifest for E2E tuning.
func GenerateE2ETuningConfigMapManifest(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-qlora-params-template",
			Namespace: namespace, // Same as workspace namespace
		},
		Data: map[string]string{
			"training_config.yaml": `training_config:
  ModelConfig:
    torch_dtype: "bfloat16"
    local_files_only: true
    device_map: "auto"
  
  QuantizationConfig:
    load_in_4bit: true
    bnb_4bit_quant_type: "nf4"
    bnb_4bit_compute_dtype: "bfloat16"
    bnb_4bit_use_double_quant: true
  
  LoraConfig:
    r: 8
    lora_alpha: 8
    lora_dropout: 0.0
    target_modules: ['k_proj', 'q_proj', 'v_proj', 'o_proj', "gate_proj", "down_proj", "up_proj"]
  
  TrainingArguments:
    output_dir: "/mnt/results"
    ddp_find_unused_parameters: false
    save_strategy: "epoch"
    per_device_train_batch_size: 1
    max_steps: 2  # Adding this line to limit training to 2 steps
  
  DataCollator:
    mlm: true
  
  DatasetConfig:
    shuffle_dataset: true
    train_test_split: 1`,
		},
	}
}

func GeneratePodTemplate(name, namespace, image string, labels map[string]string) *corev1.PodTemplateSpec {
	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            name,
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/sleep", "10000"},
				},
			},
		},
	}
}

func CompareSecrets(refs []corev1.LocalObjectReference, secrets []string) bool {
	if len(refs) != len(secrets) {
		return false
	}

	refSecrets := make([]string, len(refs))
	for i, ref := range refs {
		refSecrets[i] = ref.Name
	}

	sort.Strings(refSecrets)
	sort.Strings(secrets)

	return reflect.DeepEqual(refSecrets, secrets)
}
