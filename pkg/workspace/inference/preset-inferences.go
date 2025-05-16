// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
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
	}}

	defaultLivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(Port5000),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	defaultReadinessProbe = &corev1.Probe{
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

func GetInferenceImageInfo(ctx context.Context, workspaceObj *v1beta1.Workspace, presetObj *pkgmodel.PresetParam) (string, []corev1.LocalObjectReference) {
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

	// resource requirements
	var skuNumGPUs int
	// initially respect the user setting by deploying the model on the same number of nodes as the user requested
	numNodes := *workspaceObj.Resource.Count
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
		// Calculate the minimum number of nodes required to satisfy the model's total GPU memory requirement.
		// The goal is to maximize GPU utilization and not spread the model across too many nodes.
		totalGPUMemoryRequired := resource.MustParse(inferenceParam.TotalGPUMemoryRequirement)
		totalGPUMemoryPerNode := resource.NewScaledQuantity(int64(gpuConfig.GPUCount*gpuConfig.GPUMemGB), resource.Giga)
		minimumNodes := 0
		for ; totalGPUMemoryRequired.Sign() > 0; totalGPUMemoryRequired.Sub(*totalGPUMemoryPerNode) {
			minimumNodes++
		}
		if minimumNodes < numNodes {
			numNodes = minimumNodes
		}
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
		RuntimeName:          runtimeName,
		GPUConfig:            gpuConfig,
		ConfigVolume:         &cmVolumeMount,
		SKUNumGPUs:           skuNumGPUs,
		NumNodes:             numNodes,
		WorkspaceMetadata:    workspaceObj.ObjectMeta,
		DistributedInference: model.SupportDistributedInference(),
		RuntimeContextExtraArguments: pkgmodel.RuntimeContextExtraArguments{
			AdaptersEnabled: len(workspaceObj.Inference.Adapters) > 0,
		},
	})

	image, imagePullSecrets := GetInferenceImageInfo(ctx, workspaceObj, inferenceParam)

	var depObj client.Object
	// For multi-node distributed inference with vLLM, we need to use a StatefulSet instead of a Deployment
	// to ensure pods are created with individual identities (their ordinal indexes) -
	// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-identity
	if model.SupportDistributedInference() && runtimeName == pkgmodel.RuntimeNameVLLM && numNodes > 1 {
		// 60 seconds initial delay for liveness probe to allow workers to join the cluster
		livenessProbe := getDistributedInferenceProbe(probeTypeLiveness, workspaceObj, 60, 10, 5)
		readinessProbe := getDistributedInferenceProbe(probeTypeReadiness, workspaceObj, 0, 10, 1)
		depObj = manifests.GenerateStatefulSetManifest(workspaceObj, image, imagePullSecrets, numNodes, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts, envVars)
	} else {
		depObj = manifests.GenerateDeploymentManifest(workspaceObj, revisionNum, image, imagePullSecrets, numNodes, commands,
			containerPorts, defaultLivenessProbe, defaultReadinessProbe, resourceReq, tolerations, volumes, volumeMounts, envVars)
	}
	err = resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}

type probeType string

const (
	probeTypeLiveness  probeType = "liveness"
	probeTypeReadiness probeType = "readiness"
)

// getDistributedInferenceProbe returns a container probe configuration for the distributed inference workload.
func getDistributedInferenceProbe(probeType probeType, wObj *v1beta1.Workspace, initialDelaySeconds, periodSeconds, timeoutSeconds int32) *corev1.Probe {
	args := map[string]string{
		"leader-address": utils.GetRayLeaderHost(wObj.ObjectMeta),
	}
	switch probeType {
	case probeTypeLiveness:
		args["ray-port"] = "6379"
	case probeTypeReadiness:
		args["vllm-port"] = strconv.Itoa(Port5000)
	}

	// for distributed inference, we cannot use the default http probe since only the leader pod
	// exposes the health check endpoint. We need to use presets/workspace/inference/vllm/multi-node-health-check.py
	// to check the health of both the leader and worker pods.
	cmd := utils.BuildCmdStr(
		fmt.Sprintf("%s %s", DefaultVLLMMultiNodeHealthCheckCommand, probeType),
		args,
	)
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: utils.ShellCmd(cmd),
			},
		},
		InitialDelaySeconds: initialDelaySeconds,
		PeriodSeconds:       periodSeconds,
		TimeoutSeconds:      timeoutSeconds,

		// lowering the failure threshold from 3 (default) to 1 and setting the
		// termination grace period to 1 second to ensure that the pod is terminated
		// immediately if the health check fails to minimize downtime.
		FailureThreshold: 1,
	}
	if probeType == probeTypeLiveness {
		probe.TerminationGracePeriodSeconds = lo.ToPtr(int64(1))
	}

	return probe
}
