// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"
	"fmt"
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
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

func updateTorchParamsForDistributedInference(ctx context.Context, kubeClient client.Client, wObj *v1beta1.Workspace, inferenceParam *pkgmodel.PresetParam) error {
	runtimeName := v1beta1.GetWorkspaceRuntimeName(wObj)
	if runtimeName != pkgmodel.RuntimeNameHuggingfaceTransformers {
		return fmt.Errorf("distributed inference is not supported for runtime %s", runtimeName)
	}

	existingService := &corev1.Service{}
	err := resources.GetResource(ctx, wObj.Name, wObj.Namespace, kubeClient, existingService)
	if err != nil {
		return err
	}

	nodes := *wObj.Resource.Count
	if inferenceParam.Transformers.TorchRunParams != nil {
		inferenceParam.Transformers.TorchRunParams["nnodes"] = strconv.Itoa(nodes)
		inferenceParam.Transformers.TorchRunParams["nproc_per_node"] = strconv.Itoa(inferenceParam.WorldSize / nodes)
		if nodes > 1 {
			inferenceParam.Transformers.TorchRunParams["node_rank"] = "$(echo $HOSTNAME | grep -o '[^-]*$')"
			inferenceParam.Transformers.TorchRunParams["master_addr"] = existingService.Spec.ClusterIP
			inferenceParam.Transformers.TorchRunParams["master_port"] = "29500"
		}
	}
	if inferenceParam.Transformers.TorchRunRdzvParams != nil {
		inferenceParam.Transformers.TorchRunRdzvParams["max_restarts"] = "3"
		inferenceParam.Transformers.TorchRunRdzvParams["rdzv_id"] = "job"
		inferenceParam.Transformers.TorchRunRdzvParams["rdzv_backend"] = "c10d"
		inferenceParam.Transformers.TorchRunRdzvParams["rdzv_endpoint"] =
			fmt.Sprintf("%s-0.%s-headless.%s.svc.cluster.local:29500", wObj.Name, wObj.Name, wObj.Namespace)
	}
	return nil
}

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
	if string(workspaceObj.Inference.Preset.AccessMode) == string(v1beta1.ModelImageAccessModePrivate) {
		imageName := workspaceObj.Inference.Preset.PresetOptions.Image
		for _, secretName := range workspaceObj.Inference.Preset.PresetOptions.ImagePullSecrets {
			imagePullSecretRefs = append(imagePullSecretRefs, corev1.LocalObjectReference{Name: secretName})
		}
		return imageName, imagePullSecretRefs
	} else {
		imageName := string(workspaceObj.Inference.Preset.Name)
		imageTag := presetObj.Tag
		registryName := os.Getenv("PRESET_REGISTRY_NAME")
		imageName = fmt.Sprintf("%s/kaito-%s:%s", registryName, imageName, imageTag)

		return imageName, imagePullSecretRefs
	}
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

	if model.SupportDistributedInference() {
		if err := updateTorchParamsForDistributedInference(ctx, kubeClient, workspaceObj, inferenceParam); err != nil {
			klog.ErrorS(err, "failed to update torch params", "workspace", workspaceObj)
			return nil, err
		}
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

	// Add config volume mount
	cmVolume, cmVolumeMount := utils.ConfigCMVolume(configVolume.Name)
	volumes = append(volumes, cmVolume)
	volumeMounts = append(volumeMounts, cmVolumeMount)

	// add share memory for cross process communication
	shmVolume, shmVolumeMount := utils.ConfigSHMVolume(skuNumGPUs)
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
		depObj = manifests.GenerateStatefulSetManifest(ctx, workspaceObj, image, imagePullSecrets, *workspaceObj.Resource.Count, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)
	} else {
		depObj = manifests.GenerateDeploymentManifest(ctx, workspaceObj, revisionNum, image, imagePullSecrets, *workspaceObj.Resource.Count, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)
	}
	err = resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
