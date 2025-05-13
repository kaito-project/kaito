// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	ProbePath = "/health"
	Port5000  = 5000
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: int32(Port5000),
	},
	}

	livenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(Port5000),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	readinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(Port5000),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
	}

	tolerations = []corev1.Toleration{
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpExists,
			Key:      resources.CapacityNvidiaGPU,
		},
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Value:    consts.GPUString,
			Key:      consts.SKUString,
			Operator: corev1.TolerationOpEqual,
		},
	}
)

func GetInferenceImageInfo(ctx context.Context, workspaceObj *v1beta1.Workspace, presetObj *model.PresetParam) (string, []corev1.LocalObjectReference) {
	imagePullSecretRefs := []corev1.LocalObjectReference{}
	// Check if the workspace preset's access mode is private
	if len(workspaceObj.Inference.Adapters) > 0 {
		for _, adapter := range workspaceObj.Inference.Adapters {
			for _, secretName := range adapter.Source.ImagePullSecrets {
				imagePullSecretRefs = append(imagePullSecretRefs, corev1.LocalObjectReference{Name: secretName})
			}
		}
	}

	// Three possible cases for inference workload image selection:
	// 1. If the preset is set to download at runtime, use the 'kaito-base' image.
	// 2. Otherwise, use the preset image, which has the model weights packaged in.
	var imageName, imageTag string
	if presetObj.DownloadAtRuntime {
		// Force the use of kaito-base image if the preset is set to download at runtime.
		// The kaito-base image is the same as other preset images but without the model
		// files packaged in.
		imageName = "base"
		imageTag = metadata.MustGet(imageName).Tag
	} else {
		imageName = string(workspaceObj.Inference.Preset.Name)
		imageTag = presetObj.Tag
	}

	registryName := os.Getenv("PRESET_REGISTRY_NAME")
	imageName = fmt.Sprintf("%s/kaito-%s:%s", registryName, imageName, imageTag)

	return imageName, imagePullSecretRefs
}

func CreatePresetInference(ctx context.Context, workspaceObj *v1beta1.Workspace, revisionNum string,
	model pkgmodel.Model, kubeClient client.Client) (client.Object, error) {
	inferenceParam := model.GetInferenceParameters().DeepCopy()

	configVolume, err := resources.EnsureConfigOrCopyFromDefault(ctx, kubeClient,
		client.ObjectKey{
			Name:      workspaceObj.Inference.Config,
			Namespace: workspaceObj.Namespace,
		},
		client.ObjectKey{
			Name: v1beta1.DefaultInferenceConfigTemplate,
		},
	)
	if err != nil {
		return nil, err
	}

	// Check if inference_config.yaml exists
	inferenceConfigYAML, ok := configVolume.Data["inference_config.yaml"]
	if !ok {
		return nil, apis.ErrMissingField("inference_config.yaml in ConfigMap")
	}

	// Parse the inference config
	var inferenceConfig v1beta1.InferenceConfig
	if err := yaml.Unmarshal([]byte(inferenceConfigYAML), &inferenceConfig); err != nil {
		return nil, apis.ErrGeneric(fmt.Sprintf("Failed to parse inference_config.yaml: %v", err), "inference_config.yaml")
	}

	// resource requirements
	var skuNumGPUs int
	gpuConfig, err := utils.GetGPUConfigBySKU(workspaceObj.Resource.InstanceType)
	if err != nil {
		gpuConfig, err = utils.TryGetGPUConfigFromNode(ctx, kubeClient, workspaceObj.Status.WorkerNodes)
		if err != nil {
			defaultNumGPU := resource.MustParse(inferenceParam.GPUCountRequirement)
			skuNumGPUs = int(defaultNumGPU.Value())
		}
	}
	if gpuConfig != nil {
		// Check if this is a multi-GPU instance with less than 20GB per GPU
		gpuMemPerGPU := gpuConfig.GPUMemGB / gpuConfig.GPUCount
		if gpuConfig.GPUCount > 1 && gpuMemPerGPU < 20 {
			// For multi-GPU instances with less than 20GB per GPU, max-model-len is required
			maxModelLen, exists := inferenceConfig.VLLM["max-model-len"]
			if !exists || maxModelLen == "" {
				return nil, apis.ErrMissingField("max-model-len is required in the vllm section of inference_config.yaml when using multi-GPU instances with <20GB of memory per GPU")
			}
		}

		skuNumGPUs = gpuConfig.GPUCount
	}
	resourceReq := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
		},
	}

	// additional volume
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	var envVars []corev1.EnvVar

	// Add config volume mount
	cmVolume, cmVolumeMount := utils.ConfigCMVolume(configVolume.Name)
	volumes = append(volumes, cmVolume)
	volumeMounts = append(volumeMounts, cmVolumeMount)

	// add share memory for cross process communication
	shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
	if shmVolume.Name != "" {
		volumes = append(volumes, shmVolume)
	}
	if shmVolumeMount.Name != "" {
		volumeMounts = append(volumeMounts, shmVolumeMount)
	}
	if len(workspaceObj.Inference.Adapters) > 0 {
		adapterVolume, adapterVolumeMount := utils.ConfigAdapterVolume()
		volumes = append(volumes, adapterVolume)
		volumeMounts = append(volumeMounts, adapterVolumeMount)
	}
	if inferenceParam.DownloadAtRuntime {
		envVars = append(envVars, corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: workspaceObj.Inference.Preset.PresetOptions.ModelAccessSecret,
					},
					Key: "HF_TOKEN",
				},
			},
		})
	}

	// inference command
	runtimeName := v1beta1.GetWorkspaceRuntimeName(workspaceObj)
	commands := inferenceParam.GetInferenceCommand(pkgmodel.RuntimeContext{
		RuntimeName:  runtimeName,
		GPUConfig:    gpuConfig,
		ConfigVolume: &cmVolumeMount,
		SKUNumGPUs:   skuNumGPUs,
		RuntimeContextExtraArguments: pkgmodel.RuntimeContextExtraArguments{
			AdaptersEnabled: len(workspaceObj.Inference.Adapters) > 0,
		},
	})

	image, imagePullSecrets := GetInferenceImageInfo(ctx, workspaceObj, inferenceParam)

	var depObj client.Object
	if model.SupportDistributedInference() {
		depObj = manifests.GenerateStatefulSetManifest(workspaceObj, image, imagePullSecrets, *workspaceObj.Resource.Count, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts, envVars)
	} else {
		depObj = manifests.GenerateDeploymentManifest(workspaceObj, revisionNum, image, imagePullSecrets, *workspaceObj.Resource.Count, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts, envVars)
	}
	err = resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
