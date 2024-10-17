// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/azure/kaito/pkg/utils"
	"github.com/azure/kaito/pkg/utils/consts"

	"github.com/azure/kaito/api/v1alpha1"
	kaitov1alpha1 "github.com/azure/kaito/api/v1alpha1"
	"github.com/azure/kaito/pkg/model"
	"github.com/azure/kaito/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ProbePath = "/health"
	Port5000  = int32(5000)
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: Port5000,
	},
	}

	livenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(5000),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	readinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(5000),
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

func updateTorchParamsForDistributedInference(ctx context.Context, kubeClient client.Client, wObj *kaitov1alpha1.Workspace, inferenceParam *model.PresetParam) error {
	backendName := v1alpha1.GetWorkspaceBackendName(wObj)
	if backendName != model.BackendNameHuggingfaceTransformers {
		return fmt.Errorf("distributed inference is not supported for backend %s", backendName)
	}

	existingService := &corev1.Service{}
	err := resources.GetResource(ctx, wObj.Name, wObj.Namespace, kubeClient, existingService)
	if err != nil {
		return err
	}

	nodes := *wObj.Resource.Count
	inferenceParam.Transformers.TorchRunParams["nnodes"] = strconv.Itoa(nodes)
	inferenceParam.Transformers.TorchRunParams["nproc_per_node"] = strconv.Itoa(inferenceParam.WorldSize / nodes)
	if nodes > 1 {
		inferenceParam.Transformers.TorchRunParams["node_rank"] = "$(echo $HOSTNAME | grep -o '[^-]*$')"
		inferenceParam.Transformers.TorchRunParams["master_addr"] = existingService.Spec.ClusterIP
		inferenceParam.Transformers.TorchRunParams["master_port"] = "29500"
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

func GetInferenceImageInfo(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace, presetObj *model.PresetParam) (string, []corev1.LocalObjectReference) {
	imagePullSecretRefs := []corev1.LocalObjectReference{}
	// Check if the workspace preset's access mode is private
	if string(workspaceObj.Inference.Preset.AccessMode) == string(kaitov1alpha1.ModelImageAccessModePrivate) {
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

func CreatePresetInference(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace, revisionNum string,
	model model.Model, kubeClient client.Client) (client.Object, error) {
	inferenceParam := model.GetInferenceParameters().DeepCopy()

	if model.SupportDistributedInference() {
		if err := updateTorchParamsForDistributedInference(ctx, kubeClient, workspaceObj, inferenceParam); err != nil { //
			klog.ErrorS(err, "failed to update torch params", "workspace", workspaceObj)
			return nil, err
		}
	}

	// additional volume
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	shmVolume, shmVolumeMount := utils.ConfigSHMVolume(*workspaceObj.Resource.Count)
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

	// resource requirements
	skuNumGPUs, err := utils.GetSKUNumGPUs(ctx, kubeClient, workspaceObj.Status.WorkerNodes,
		workspaceObj.Resource.InstanceType, inferenceParam.GPUCountRequirement)
	if err != nil {
		return nil, fmt.Errorf("failed to get SKU num GPUs: %v", err)
	}
	resourceReq := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse(skuNumGPUs),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse(skuNumGPUs),
		},
	}

	// inference command
	backendName := v1alpha1.GetWorkspaceBackendName(workspaceObj)
	commands := inferenceParam.GetInferenceCommand(backendName)

	image, imagePullSecrets := GetInferenceImageInfo(ctx, workspaceObj, inferenceParam)

	var depObj client.Object
	if model.SupportDistributedInference() {
		depObj = resources.GenerateStatefulSetManifest(ctx, workspaceObj, image, imagePullSecrets, *workspaceObj.Resource.Count, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)
	} else {
		depObj = resources.GenerateDeploymentManifest(ctx, workspaceObj, revisionNum, image, imagePullSecrets, *workspaceObj.Resource.Count, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)
	}
	err = resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
