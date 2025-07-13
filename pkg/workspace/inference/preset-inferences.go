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

package inference

import (
	"context"
	"fmt"
	"strconv"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	ProbePath = "/health"

	// PortInferenceServer is the default port for the inference server.
	PortInferenceServer = 5000
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: int32(PortInferenceServer),
	}}

	defaultLivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(PortInferenceServer),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	defaultReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(PortInferenceServer),
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

	return GetBaseImageName(), imagePullSecretRefs
}

// GenerateModelFileCacheVolume generates a volume for caching model files.
// These files would be stored in the local pv and its lifetime is tied to the pod.
// Use NVMe for storage acceleration if it's available.
//
// notes: no capacity check here because NVMe is typically a TiB level storage,
// which is sufficient for almost all models. check it if this assumption is not true.
func GenerateModelWeightsCacheVolume(ctx context.Context, workspaceObj *v1beta1.Workspace, model pkgmodel.Model) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "model-weights-volume",
			Labels: map[string]string{
				v1beta1.LabelWorkspaceName: workspaceObj.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &consts.LocalNVMeStorageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					// place model files in this volume
					corev1.ResourceStorage: resource.MustParse(model.GetInferenceParameters().DiskStorageRequirement),
				},
			},
		},
	}
}

// TODO: refactor this function
func GeneratePresetInference(ctx context.Context, workspaceObj *v1beta1.Workspace, revisionNum string,
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
		totalGPUMemoryPerNode := resource.NewQuantity(int64(gpuConfig.GPUMemGB)*consts.GiBToBytes, resource.BinarySI)

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
	var pvcs []corev1.PersistentVolumeClaim

	// Add config volume mount
	cmVolume, cmVolumeMount := utils.ConfigCMVolume(configVolume.Name)
	volumes = append(volumes, cmVolume)
	volumeMounts = append(volumeMounts, cmVolumeMount)

	// add model weights volume mount
	volumeMounts = append(volumeMounts, utils.DefaultModelWeightsVolumeMount)

	// add share memory for cross process communication
	shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
	if shmVolume.Name != "" {
		volumes = append(volumes, shmVolume)
	}
	if shmVolumeMount.Name != "" {
		volumeMounts = append(volumeMounts, shmVolumeMount)
	}

	// add adapter volume mount if adapters are enabled
	if len(workspaceObj.Inference.Adapters) > 0 {
		adapterVolume, adapterVolumeMount := utils.ConfigAdapterVolume()
		volumes = append(volumes, adapterVolume)
		volumeMounts = append(volumeMounts, adapterVolumeMount)
	}

	// additional environment variables
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

	// additional initContainers
	initContainers := manifests.GenerateModelPullerContainer(ctx, workspaceObj, inferenceParam)

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
		if checkIfNVMeAvailable(ctx, gpuConfig, kubeClient) {
			pvcs = append(pvcs, GenerateModelWeightsCacheVolume(ctx, workspaceObj, model))
		} else {
			volumes = append(volumes, utils.DefaultModelWeightsVolume)
		}

		// 60 seconds initial delay for liveness probe to allow workers to join the cluster
		livenessProbe := getDistributedInferenceProbe(probeTypeLiveness, workspaceObj, 60, 10, 5)
		readinessProbe := getDistributedInferenceProbe(probeTypeReadiness, workspaceObj, 0, 10, 1)
		depObj = manifests.GenerateStatefulSetManifest(workspaceObj, revisionNum, image, imagePullSecrets, numNodes, commands,
			containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts, envVars, initContainers, pvcs)
	} else {
		volumes = append(volumes, utils.DefaultModelWeightsVolume)

		depObj = manifests.GenerateDeploymentManifest(workspaceObj, revisionNum, image, imagePullSecrets, numNodes, commands,
			containerPorts, defaultLivenessProbe, defaultReadinessProbe, resourceReq, tolerations, volumes, volumeMounts, envVars, initContainers)
	}
	return depObj, nil
}

type probeType string

const (
	probeTypeLiveness  probeType = "liveness"
	probeTypeReadiness probeType = "readiness"
)

func checkIfNVMeAvailable(ctx context.Context, gpuConfig *sku.GPUConfig, kubeClient client.Client) bool {
	if gpuConfig == nil || !gpuConfig.NVMeDiskEnabled {
		return false
	}

	// Check if the required NVMe storage class exists
	storageClass := &storagev1.StorageClass{}
	err := kubeClient.Get(ctx, client.ObjectKey{Name: consts.LocalNVMeStorageClass}, storageClass)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false
		}
		klog.ErrorS(err, "Failed to check for NVMe storage class. Assuming it's available.")
	}
	return true
}

// getDistributedInferenceProbe returns a container probe configuration for the distributed inference workload.
func getDistributedInferenceProbe(probeType probeType, wObj *v1beta1.Workspace, initialDelaySeconds, periodSeconds, timeoutSeconds int32) *corev1.Probe {
	args := map[string]string{
		"leader-address": utils.GetRayLeaderHost(wObj.ObjectMeta),
	}
	switch probeType {
	case probeTypeLiveness:
		args["ray-port"] = strconv.Itoa(pkgmodel.PortRayCluster)
	case probeTypeReadiness:
		args["vllm-port"] = strconv.Itoa(PortInferenceServer)
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

func GetBaseImageName() string {
	presetObj := metadata.MustGet("base")
	return utils.GetPresetImageName(presetObj.Name, presetObj.Tag)
}
