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
	appsv1 "k8s.io/api/apps/v1"
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
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	ProbePath = "/health"
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: int32(consts.PortInferenceServer),
	}}

	defaultLivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt32(consts.PortInferenceServer),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	defaultReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt32(consts.PortInferenceServer),
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

func GetInferenceImageInfo(ctx context.Context, workspaceObj *v1beta1.Workspace) []corev1.LocalObjectReference {
	imagePullSecretRefs := []corev1.LocalObjectReference{}
	// Check if the workspace preset's access mode is private
	if len(workspaceObj.Inference.Adapters) > 0 {
		for _, adapter := range workspaceObj.Inference.Adapters {
			for _, secretName := range adapter.Source.ImagePullSecrets {
				imagePullSecretRefs = append(imagePullSecretRefs, corev1.LocalObjectReference{Name: secretName})
			}
		}
	}

	return imagePullSecretRefs
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

func GeneratePresetInference(ctx context.Context, workspaceObj *v1beta1.Workspace, revisionNum string,
	model pkgmodel.Model, kubeClient client.Client) (client.Object, error) {

	gctx := &generator.WorkspaceGeneratorContext{
		Ctx:        ctx,
		KubeClient: kubeClient,
		Workspace:  workspaceObj,
		Model:      model,
	}

	gpuConfig := getGPUConfig(gctx)
	// initially respect the user setting by deploying the model on the same number of nodes as the user requested
	numNodes := *workspaceObj.Resource.Count
	// if gpu mem is known, we can setup the distributed correctly
	if gpuConfig.GPUMemGB > 0 && gpuConfig.GPUCount > 0 {
		// Calculate the minimum number of nodes required to satisfy the model's total GPU memory requirement.
		// The goal is to maximize GPU utilization and not spread the model across too many nodes.
		totalGPUMemoryRequired := resource.MustParse(model.GetInferenceParameters().TotalGPUMemoryRequirement)
		totalGPUMemoryPerNode := resource.NewQuantity(int64(gpuConfig.GPUMemGB)*consts.GiBToBytes, resource.BinarySI)

		minimumNodes := 0
		for ; totalGPUMemoryRequired.Sign() > 0; totalGPUMemoryRequired.Sub(*totalGPUMemoryPerNode) {
			minimumNodes++
		}
		if minimumNodes < numNodes {
			numNodes = minimumNodes
		}
	}

	podOpts := []generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, corev1.PodSpec]{
		GenerateInferencePodSpec(&gpuConfig, numNodes),
		SetModelDownloadInfo,
		SetAdapterPuller,
	}

	// For multi-node distributed inference with vLLM, we need to use a StatefulSet instead of a Deployment
	// to ensure pods are created with individual identities (their ordinal indexes) -
	// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-identity
	if shouldUseDistributedInference(gctx, numNodes) {
		podOpts = append(podOpts, SetDistributedInferenceProbe)
		ssOpts := []generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, appsv1.StatefulSet]{
			manifests.GenerateStatefulSetManifest(revisionNum, numNodes),
		}

		if checkIfNVMeAvailable(ctx, &gpuConfig, kubeClient) {
			ssOpts = append(ssOpts, manifests.AddStatefulSetVolumeClaimTemplates(GenerateModelWeightsCacheVolume(ctx, workspaceObj, model)))
		} else {
			podOpts = append(podOpts, SetDefaultModelWeightsVolume)
		}

		podSpec, err := generator.GenerateManifest(gctx, podOpts...)
		if err != nil {
			return nil, err
		}
		ssOpts = append(ssOpts, manifests.SetStatefulSetPodSpec(podSpec))

		return generator.GenerateManifest(gctx, ssOpts...)
	} else {
		podOpts = append(podOpts, SetDefaultModelWeightsVolume)

		podSpec, err := generator.GenerateManifest(gctx, podOpts...)
		if err != nil {
			return nil, err
		}

		return generator.GenerateManifest(gctx,
			manifests.GenerateDeploymentManifest(revisionNum, numNodes),
			manifests.SetDeploymentPodSpec(podSpec),
		)
	}
}

func getGPUConfig(ctx *generator.WorkspaceGeneratorContext) sku.GPUConfig {
	var gpuConfig *sku.GPUConfig
	var err error
	// 1. try to get GPU config from known sku if instanceType is set
	if len(ctx.Workspace.Resource.PreferredNodes) == 0 {
		gpuConfig, _ = utils.GetGPUConfigBySKU(ctx.Workspace.Resource.InstanceType)
		if gpuConfig != nil {
			return *gpuConfig
		}
	}

	// 2. try to get GPU config from the node status
	gpuConfig, err = utils.TryGetGPUConfigFromNode(ctx.Ctx, ctx.KubeClient, ctx.Workspace.Status.WorkerNodes)
	if err == nil {
		return *gpuConfig
	}

	// 3. if both above methods fail, use the default GPU count requirement from the model
	//    FIXME: assume gpu nodes are provided here. cpu inference should not go through this path.
	defaultNumGPU := resource.MustParse(ctx.Model.GetInferenceParameters().GPUCountRequirement)
	skuNumGPUs := int(defaultNumGPU.Value())
	return sku.GPUConfig{
		GPUCount: skuNumGPUs,
	}
}

func shouldUseDistributedInference(ctx *generator.WorkspaceGeneratorContext, numNodes int) bool {
	runtimeName := v1beta1.GetWorkspaceRuntimeName(ctx.Workspace)
	return ctx.Model.SupportDistributedInference() && runtimeName == pkgmodel.RuntimeNameVLLM && numNodes > 1
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
		args["vllm-port"] = strconv.FormatInt(int64(consts.PortInferenceServer), 10)
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

func GenerateInferencePodSpec(gpuConfig *sku.GPUConfig, numNodes int) func(*generator.WorkspaceGeneratorContext, *corev1.PodSpec) error {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		configVolume, err := resources.EnsureConfigOrCopyFromDefault(ctx.Ctx, ctx.KubeClient,
			client.ObjectKey{
				Name:      ctx.Workspace.Inference.Config,
				Namespace: ctx.Workspace.Namespace,
			},
			client.ObjectKey{
				Name: v1beta1.DefaultInferenceConfigTemplate,
			},
		)
		if err != nil {
			return err
		}

		// additional volume
		var volumes []corev1.Volume
		var volumeMounts []corev1.VolumeMount

		// Add config volume mount
		cmVolume, cmVolumeMount := utils.ConfigCMVolume(configVolume.Name)
		volumes = append(volumes, cmVolume)
		volumeMounts = append(volumeMounts, cmVolumeMount)

		// add model weights volume mount
		volumeMounts = append(volumeMounts, utils.DefaultModelWeightsVolumeMount)

		// add share memory for cross process communication
		shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
		volumes = append(volumes, shmVolume)
		volumeMounts = append(volumeMounts, shmVolumeMount)

		// node selector
		nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(ctx.Workspace.Resource.LabelSelector.MatchLabels))
		for key, value := range ctx.Workspace.Resource.LabelSelector.MatchLabels {
			nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
				Key:      key,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{value},
			})
		}

		// resource requirements
		resourceReq := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(gpuConfig.GPUCount), resource.DecimalSI),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(gpuConfig.GPUCount), resource.DecimalSI),
			},
		}

		// inference command
		inferenceParam := ctx.Model.GetInferenceParameters().DeepCopy()
		runtimeName := v1beta1.GetWorkspaceRuntimeName(ctx.Workspace)
		commands := inferenceParam.GetInferenceCommand(pkgmodel.RuntimeContext{
			RuntimeName:          runtimeName,
			GPUConfig:            gpuConfig,
			ConfigVolume:         &cmVolumeMount,
			SKUNumGPUs:           gpuConfig.GPUCount,
			NumNodes:             numNodes,
			WorkspaceMetadata:    ctx.Workspace.ObjectMeta,
			DistributedInference: ctx.Model.SupportDistributedInference(),
			RuntimeContextExtraArguments: pkgmodel.RuntimeContextExtraArguments{
				AdaptersEnabled: len(ctx.Workspace.Inference.Adapters) > 0,
			},
		})

		spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: nodeRequirements,
						},
					},
				},
			},
		}
		spec.ImagePullSecrets = GetInferenceImageInfo(ctx.Ctx, ctx.Workspace)
		spec.Containers = []corev1.Container{
			{
				Name:           ctx.Workspace.Name,
				Image:          GetBaseImageName(),
				Command:        commands,
				Resources:      resourceReq,
				Ports:          containerPorts,
				LivenessProbe:  defaultLivenessProbe,
				ReadinessProbe: defaultReadinessProbe,
				VolumeMounts:   volumeMounts,
			},
		}
		spec.Tolerations = tolerations
		spec.Volumes = volumes

		return nil
	}
}

func SetModelDownloadInfo(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	if ctx.Model.GetInferenceParameters().DownloadAtRuntime {
		envvar := corev1.EnvVar{
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ctx.Workspace.Inference.Preset.PresetOptions.ModelAccessSecret,
					},
					Key: "HF_TOKEN",
				},
			},
		}

		for i := range spec.Containers {
			// add HF_TOKEN env var to all containers
			spec.Containers[i].Env = append(spec.Containers[i].Env, envvar)
		}
		return nil
	}

	// additional initContainers
	initContainers := manifests.GenerateModelPullerContainer(ctx.Ctx, ctx.Workspace, ctx.Model.GetInferenceParameters())
	spec.InitContainers = append(spec.InitContainers, initContainers...)
	return nil
}

func SetAdapterPuller(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	if len(ctx.Workspace.Inference.Adapters) == 0 {
		return nil
	}

	// add adapter volume mount if adapters are enabled
	adapterVolume, adapterVolumeMount := utils.ConfigAdapterVolume()
	spec.Volumes = append(spec.Volumes, adapterVolume)
	for i := range spec.Containers { // FIXME: assume only one container in the pod
		spec.Containers[i].VolumeMounts = append(spec.Containers[i].VolumeMounts, adapterVolumeMount)
	}

	// add container to pull adapters
	volumeMounts := []corev1.VolumeMount{adapterVolumeMount}
	pullerContainers, pullerEnvVars, pullerVolumes := manifests.GeneratePullerContainers(ctx.Workspace, volumeMounts)
	spec.InitContainers = append(spec.InitContainers, pullerContainers...)
	spec.Volumes = append(spec.Volumes, pullerVolumes...)
	for i := range spec.Containers { // FIXME: assume only one container in the pod
		spec.Containers[i].Env = append(spec.Containers[i].Env, pullerEnvVars...)
	}
	return nil
}

func SetDistributedInferenceProbe(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	// 60 seconds initial delay for liveness probe to allow workers to join the cluster
	livenessProbe := getDistributedInferenceProbe(probeTypeLiveness, ctx.Workspace, 60, 10, 5)
	readinessProbe := getDistributedInferenceProbe(probeTypeReadiness, ctx.Workspace, 0, 10, 1)
	envVar := corev1.EnvVar{
		Name: "POD_INDEX",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: fmt.Sprintf("metadata.labels['%s']", appsv1.PodIndexLabel),
			},
		},
	}
	for i := range spec.Containers {
		if spec.Containers[i].Name == ctx.Workspace.Name {
			spec.Containers[i].LivenessProbe = livenessProbe
			spec.Containers[i].ReadinessProbe = readinessProbe
			spec.Containers[i].Env = append(spec.Containers[i].Env, envVar)
			break
		}
	}
	return nil
}

func SetDefaultModelWeightsVolume(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	spec.Volumes = append(spec.Volumes, utils.DefaultModelWeightsVolume)
	return nil
}
