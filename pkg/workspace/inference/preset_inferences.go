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
	"math"
	"strconv"
	"time"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/utils/nodes"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	metadata "github.com/kaito-project/kaito/presets/workspace/models"
)

const (
	ProbePath = "/health"

	// defaultStartupProbeTimeout is the startup probe timeout for models that do not
	// specify ReadinessTimeout. 30 minutes covers all current models.
	defaultStartupProbeTimeout = 30 * time.Minute
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: int32(consts.PortInferenceServer),
	}}

	// defaultLivenessProbe has no initial delay because the startup probe ensures
	// the model is up before liveness evaluation begins.
	defaultLivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt32(consts.PortInferenceServer),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 0,
		PeriodSeconds:       10,
		FailureThreshold:    3,
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
)

func defaultTolerations(ws *v1beta1.Workspace) []corev1.Toleration {
	tolerations := []corev1.Toleration{
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpExists,
			Key:      nodes.CapacityNvidiaGPU,
		},
		{
			Effect:   corev1.TaintEffectNoSchedule,
			Value:    consts.GPUString,
			Key:      consts.SKUString,
			Operator: corev1.TolerationOpEqual,
		},
	}

	if sku.IsAzureCloudProvider() {
		tolerations = append(tolerations, corev1.Toleration{
			Effect:   corev1.TaintEffectNoSchedule,
			Key:      consts.SpotInstanceKey,
			Operator: corev1.TolerationOpEqual,
			Value:    consts.SpotInstanceValue,
		})
	}

	return tolerations
}

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
	model pkgmodel.Model, kubeClient client.Client, provisioner nodeprovision.NodeProvisioner) (client.Object, error) {

	gctx := &generator.WorkspaceGeneratorContext{
		Ctx:             ctx,
		KubeClient:      kubeClient,
		Workspace:       workspaceObj,
		Model:           model,
		NodeProvisioner: provisioner,
	}

	gpuConfig, err := getGPUConfig(gctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU config: %w", err)
	}

	// Set the target node count for the inference workload
	numNodes := int(workspaceObj.Status.TargetNodeCount)

	// Resolve streaming configuration
	streamingEnabled := ModelStreamingEnabled(workspaceObj)
	var streamingModelPath, streamingLoadFormat string
	var streamingCfg *StreamingConfig
	var modelID string

	if streamingEnabled {
		modelID = ResolveHFModelID(workspaceObj)
		crName := ModelMirrorCRName(modelID)

		mmCR := &kaitov1alpha1.ModelMirror{}
		if err := gctx.KubeClient.Get(gctx.Ctx, client.ObjectKey{Name: crName}, mmCR); err != nil {
			return nil, fmt.Errorf("failed to get ModelMirror CR %s for streaming config: %w", crName, err)
		}

		streamingCfg, err = StreamingDefaults.ModelStreamer.GetStreamingConfig(gctx, crName, mmCR.Spec.JobNamespace, modelID)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve streaming config: %w", err)
		}
		streamingModelPath = streamingCfg.ModelPath
		streamingLoadFormat = "runai_streamer"
	}

	podOpts := []generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, corev1.PodSpec]{
		GenerateInferencePodSpec(gpuConfig, numNodes, streamingModelPath, streamingLoadFormat),
		SetProvisionerNodeSelector,
		SetHFToken,
	}

	// Model source: streaming (az://) vs local download. Mutually exclusive.
	if streamingEnabled {
		podOpts = append(podOpts, SetStreamingConfig(streamingCfg, modelID, StreamingDefaults.ServiceAccount))
	} else {
		podOpts = append(podOpts, SetModelDownloadInfo)
	}

	podOpts = append(podOpts, SetAdapterPuller)

	// Use StatefulSet for all use cases to ensure consistent pod identity and storage management
	// For multi-node distributed inference with vLLM, we need StatefulSet to ensure pods are
	// created with individual identities (their ordinal indexes) -
	// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-identity
	distributed := shouldUseDistributedInference(gctx, numNodes)
	if distributed {
		podOpts = append(podOpts, SetDistributedInferenceProbe)
	}
	if v1beta1.ShouldRunBenchmark(workspaceObj) {
		podOpts = append(podOpts, SetBenchmarkConfig(distributed))
	}

	ssOpts := []generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, appsv1.StatefulSet]{
		manifests.GenerateStatefulSetManifest(revisionNum, numNodes),
	}

	// Volume handling: streaming skips weights volume (model is read from az:// directly).
	if !streamingEnabled {
		if checkIfNVMeAvailable(ctx, gpuConfig, kubeClient) {
			ssOpts = append(ssOpts, manifests.AddStatefulSetVolumeClaimTemplates(GenerateModelWeightsCacheVolume(ctx, workspaceObj, model)))
		} else {
			podOpts = append(podOpts, SetDefaultModelWeightsVolume)
		}
	}

	// Add provider-specific pod labels to StatefulSet template (e.g. Azure WI label)
	if streamingEnabled && len(streamingCfg.PodLabels) > 0 {
		podLabels := streamingCfg.PodLabels
		ssOpts = append(ssOpts, func(ctx *generator.WorkspaceGeneratorContext, ss *appsv1.StatefulSet) error {
			if ss.Spec.Template.Labels == nil {
				ss.Spec.Template.Labels = make(map[string]string)
			}
			for k, v := range podLabels {
				ss.Spec.Template.Labels[k] = v
			}
			return nil
		})
	}

	podSpec, err := generator.GenerateManifest(gctx, podOpts...)
	if err != nil {
		return nil, err
	}

	ssOpts = append(ssOpts, manifests.SetStatefulSetPodSpec(podSpec))

	return generator.GenerateManifest(gctx, ssOpts...)
}

func getGPUConfig(ctx *generator.WorkspaceGeneratorContext) (*sku.GPUConfig, error) {
	if featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] {
		// NAP is disabled (BYO scenario) - prefer to get GPU config from matching nodes with nvidia.com labels
		// Only try to find matching nodes if we have a labelSelector and if WorkerNodes is not already populated
		readyNodes, err := nodeprovision.GetReadyNodes(ctx.Ctx, ctx.KubeClient, ctx.NodeProvisioner, ctx.Workspace)
		if err != nil {
			return nil, fmt.Errorf("failed to list ready nodes: %w", err)
		}
		if len(readyNodes) == 0 {
			return nil, fmt.Errorf("no ready nodes found matching the workspace's label selector")
		}

		return sku.GetGPUConfigFromNodeLabels(readyNodes[0])
	} else {
		// NAP is enabled - try to get GPU config from known SKU
		gpuConfig, err := sku.GetGPUConfigBySKU(ctx.Workspace.Resource.InstanceType)
		if err != nil {
			return nil, err
		}

		return gpuConfig, nil
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
func getDistributedInferenceProbe(probeType probeType, wObj *v1beta1.Workspace, initialDelaySeconds, periodSeconds, timeoutSeconds, failureThreshold int32, vllmPort ...int32) *corev1.Probe {
	port := consts.PortInferenceServer
	if len(vllmPort) > 0 && vllmPort[0] > 0 {
		port = vllmPort[0]
	}
	args := map[string]string{
		"leader-address": utils.GetRayLeaderHost(wObj.ObjectMeta),
	}
	switch probeType {
	case probeTypeLiveness:
		args["ray-port"] = strconv.Itoa(pkgmodel.PortRayCluster)
	case probeTypeReadiness:
		args["vllm-port"] = strconv.FormatInt(int64(port), 10)
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
		FailureThreshold:    failureThreshold,
	}
	if probeType == probeTypeLiveness {
		probe.TerminationGracePeriodSeconds = lo.ToPtr(int64(1))
	}

	return probe
}

func buildStartupProbe(timeout time.Duration, port ...int32) *corev1.Probe {
	const periodSeconds = 10
	probePort := consts.PortInferenceServer
	if len(port) > 0 && port[0] > 0 {
		probePort = port[0]
	}
	// ceil(timeout / period) ensures the full timeout window is covered.
	failureThreshold := int32(math.Ceil(timeout.Seconds() / periodSeconds))
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt32(probePort),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 0,
		PeriodSeconds:       periodSeconds,
		FailureThreshold:    failureThreshold,
	}
}

// buildProbeWithPort returns a deep copy of the template probe with the HTTPGet
// port overridden when port > 0. When port is 0 the template's original port is
// preserved unchanged.
func buildProbeWithPort(template *corev1.Probe, port int32) *corev1.Probe {
	p := template.DeepCopy()
	if port > 0 && p.HTTPGet != nil {
		p.HTTPGet.Port = intstr.FromInt32(port)
	}
	return p
}

func buildDistributedStartupProbe(timeout time.Duration, wObj *v1beta1.Workspace, vllmPort ...int32) *corev1.Probe {
	const periodSeconds = int32(10)
	const timeoutSeconds = int32(1)
	failureThreshold := int32(math.Ceil(timeout.Seconds() / float64(periodSeconds)))
	return getDistributedInferenceProbe(probeTypeReadiness, wObj, 0, periodSeconds, timeoutSeconds, failureThreshold, vllmPort...)
}

// buildBenchmarkStartupProbe returns an exec startup probe that runs
// benchmark_entrypoint.py on every kubelet tick.
//
// While vLLM is loading, the script exits 1 (/health not yet up), consuming the
// failureThreshold budget.  Once /health passes, the script runs the full benchmark
// and drain phase, then exits 0 — which marks the startup probe as passed and
// activates the readiness probe.
//
// When wObj is non-nil the probe is built for distributed inference: a shell
// conditional routes the leader (POD_INDEX=0) to benchmark_entrypoint.py and
// workers to the standard multi-node health check.
//
// timeoutSeconds is set to 600 to prevent kubelet killing the process mid-benchmark.
func buildBenchmarkStartupProbe(timeout time.Duration, wObj *v1beta1.Workspace, distributed bool) *corev1.Probe {
	const periodSeconds = int32(10)
	const timeoutSeconds = int32(600) // covers benchmark duration + drain + buffer
	failureThreshold := int32(math.Ceil(timeout.Seconds() / float64(periodSeconds)))

	var command []string
	if !distributed {
		command = []string{"python3", "/workspace/vllm/benchmark_entrypoint.py"}
	} else {
		// Workers exit 0 immediately — they don't serve traffic and don't need to
		// wait for the leader. This prevents rolling update deadlocks.
		cmd := `if [ "$POD_INDEX" = "0" ]; then python3 /workspace/vllm/benchmark_entrypoint.py; else true; fi`
		command = utils.ShellCmd(cmd)
	}

	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: command,
			},
		},
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}
}

func GetBaseImageName() string {
	presetObj := metadata.MustGet("base")
	return utils.GetPresetImageName(presetObj.Registry, presetObj.Name, presetObj.Tag)
}

// GetBaseImageTag returns just the tag portion of the base image reference.
func GetBaseImageTag() string {
	presetObj := metadata.MustGet("base")
	return presetObj.Tag
}

// GetBaseRuntimeVersion returns the inference engine version baked into the base
// image for the given runtime, sourced from the base model metadata. It returns
// an empty string when the runtime is unrecognized or the version is not set.
func GetBaseRuntimeVersion(runtimeName pkgmodel.RuntimeName) string {
	rv := metadata.MustGet("base").RuntimeVersion
	switch runtimeName {
	case pkgmodel.RuntimeNameVLLM:
		return rv.VLLM
	case pkgmodel.RuntimeNameHuggingfaceTransformers:
		return rv.Transformers
	default:
		return ""
	}
}

// GetPresetQuantization returns the weight quantization method for a preset model
// (e.g. "awq", "gptq"). It returns an empty string when the preset name is empty,
// the model is not in the supported catalog, or the model is not quantized.
func GetPresetQuantization(presetName string) string {
	if presetName == "" {
		return ""
	}
	m, ok := metadata.Get(presetName)
	if !ok {
		return ""
	}
	return m.QuantMethod
}

func GenerateInferencePodSpec(gpuConfig *sku.GPUConfig, numNodes int, streamingModelPath, streamingLoadFormat string) func(*generator.WorkspaceGeneratorContext, *corev1.PodSpec) error {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		// additional volume
		var volumes []corev1.Volume
		var volumeMounts []corev1.VolumeMount

		// Mount the user-provided inference config ConfigMap when set. When empty,
		// no config volume is mounted and the runtime falls back to its built-in defaults.
		var cmVolumeMountRef *corev1.VolumeMount
		if userConfig := ctx.Workspace.Inference.Config; userConfig != "" {
			cmVolume, cmVolumeMount := utils.ConfigCMVolume(userConfig)
			volumes = append(volumes, cmVolume)
			volumeMounts = append(volumeMounts, cmVolumeMount)
			cmVolumeMountRef = &cmVolumeMount
		}

		// add model weights volume mount (skip when streaming — weights come from az://)
		if streamingModelPath == "" {
			volumeMounts = append(volumeMounts, utils.DefaultModelWeightsVolumeMount)
		}

		// add share memory for cross process communication
		shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
		volumes = append(volumes, shmVolume)
		volumeMounts = append(volumeMounts, shmVolumeMount)

		// node selector
		selectorLabels := v1beta1.SanitizedMatchLabels(ctx.Workspace.Resource.LabelSelector)
		nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(selectorLabels))
		for key, value := range selectorLabels {
			nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
				Key:      key,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{value},
			})
		}
		// resource requirements
		resourceReq := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName(nodes.CapacityNvidiaGPU): *resource.NewQuantity(int64(gpuConfig.GPUCount), resource.DecimalSI),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceName(nodes.CapacityNvidiaGPU): *resource.NewQuantity(int64(gpuConfig.GPUCount), resource.DecimalSI),
			},
		}

		// inference command
		inferenceParam := ctx.Model.GetInferenceParameters().DeepCopy()
		runtimeName := v1beta1.GetWorkspaceRuntimeName(ctx.Workspace)

		// Context-length sizing is delegated to vLLM's native auto-fit logic by
		// passing --max-model-len=auto (https://docs.vllm.ai/en/latest/configuration/engine_args/#-max-model-len).
		// vLLM measures the real KV-cache budget at startup and selects the largest
		// context that fits. An explicit max-model-len in the user's inference
		// config still takes precedence: it is appended after this flag on the
		// vLLM command line (see inference_api.py).
		maxModelLen := 2048 // Default for non-vLLM runtimes.
		if runtimeName == pkgmodel.RuntimeNameVLLM {
			maxModelLen = pkgmodel.MaxModelLenAuto
		}

		// When the routing sidecar is needed, vLLM moves to PortDecodeVLLM (5001)
		// so the sidecar can occupy PortInferenceServer (5000).
		isSidecarNeeded := needsRoutingSidecar(ctx.Workspace)
		var vllmPort int32
		if isSidecarNeeded {
			vllmPort = consts.PortDecodeVLLM
		}

		commands := inferenceParam.GetInferenceCommand(pkgmodel.RuntimeContext{
			RuntimeName:          runtimeName,
			GPUConfig:            gpuConfig,
			ConfigVolume:         cmVolumeMountRef,
			SKUNumGPUs:           gpuConfig.GPUCount,
			NumNodes:             numNodes,
			WorkspaceMetadata:    ctx.Workspace.ObjectMeta,
			DistributedInference: ctx.Model.SupportDistributedInference(),
			MaxModelLen:          maxModelLen,
			InferencePort:        vllmPort,
			RuntimeContextExtraArguments: pkgmodel.RuntimeContextExtraArguments{
				AdaptersEnabled:     len(ctx.Workspace.Inference.Adapters) > 0,
				PerformanceMode:     v1beta1.GetPerformanceMode(ctx.Workspace),
				StreamingModelPath:  streamingModelPath,
				StreamingLoadFormat: streamingLoadFormat,
			},
		})

		// Only set nodeAffinity when the user supplied selector labels.
		// An empty MatchExpressions list is rejected by the Kubernetes API server.
		if len(nodeRequirements) > 0 {
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
		}
		spec.ImagePullSecrets = GetInferenceImageInfo(ctx.Ctx, ctx.Workspace)

		// Use the model's ReadinessTimeout if specified; otherwise fall back to the
		// default. containerStatuses[].started is reliable for downstream.
		readinessTimeout := inferenceParam.ReadinessTimeout
		if readinessTimeout <= 0 {
			readinessTimeout = defaultStartupProbeTimeout
		}

		// KAITO does not support FlashInfer. Disable vLLM's FlashInfer sampler so it
		// stays on the Torch-native sampling path instead of JIT-compiling kernels at
		// runtime (the base image ships no CUDA toolchain/nvcc).
		var mainContainerEnv []corev1.EnvVar
		if runtimeName == pkgmodel.RuntimeNameVLLM {
			mainContainerEnv = append(mainContainerEnv, corev1.EnvVar{
				Name:  consts.VLLMUseFlashInferSamplerEnvName,
				Value: "0",
			})
			// Disable vLLM's DeepGEMM FP8 kernels. vLLM 0.22.1 enables them by default
			// and reports DeepGEMM as available, but the native backend is absent from
			// the base image, so the FP8 warmup hard-fails at engine init.
			mainContainerEnv = append(mainContainerEnv, corev1.EnvVar{
				Name:  consts.VLLMUseDeepGEMMEnvName,
				Value: "0",
			})
			// Disable vLLM's FlashInfer MoE backends across all precisions. For MoE
			// models vLLM auto-selects a FlashInfer (TRTLLM/CUTLASS) expert kernel,
			// which JIT-compiles at runtime via nvcc (absent from the base image) and
			// crashes the engine at startup. Setting each per-precision toggle to "0"
			// forces the Triton MoE fallback, which needs no nvcc JIT.
			for _, name := range []string{
				consts.VLLMUseFlashInferMoeFP16EnvName,
				consts.VLLMUseFlashInferMoeFP8EnvName,
				consts.VLLMUseFlashInferMoeFP4EnvName,
				consts.VLLMUseFlashInferMoeMXFP4BF16EnvName,
				consts.VLLMUseFlashInferMoeMXFP4MXFP8EnvName,
				consts.VLLMUseFlashInferMoeMXFP4MXFP8CutlassEnvName,
			} {
				mainContainerEnv = append(mainContainerEnv, corev1.EnvVar{
					Name:  name,
					Value: "0",
				})
			}
		}

		spec.Containers = []corev1.Container{
			{
				Name:           ctx.Workspace.Name,
				Image:          GetBaseImageName(),
				Command:        commands,
				Resources:      resourceReq,
				Ports:          append([]corev1.ContainerPort(nil), containerPorts...),
				StartupProbe:   buildStartupProbe(readinessTimeout, vllmPort),
				LivenessProbe:  buildProbeWithPort(defaultLivenessProbe, vllmPort),
				ReadinessProbe: buildProbeWithPort(defaultReadinessProbe, vllmPort),
				VolumeMounts:   volumeMounts,
				Env:            mainContainerEnv,
			},
		}

		applyInferenceRoleEnv(ctx.Workspace.Labels, ctx.Workspace.Name, spec)

		if isSidecarNeeded {
			injectRoutingSidecar(spec)
		}

		spec.Tolerations = defaultTolerations(ctx.Workspace)
		spec.Volumes = volumes

		return nil
	}
}

// SetHFToken adds the HF_TOKEN env var to the main inference container if
// a model access secret is configured. Needed for both DAR (download weights)
// and streaming (vLLM fetches model config/tokenizer from HuggingFace).
func SetHFToken(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	if ctx.Workspace.Inference == nil || ctx.Workspace.Inference.Preset == nil {
		return nil
	}
	accessSecret := ctx.Workspace.Inference.Preset.PresetOptions.ModelAccessSecret
	if accessSecret == "" {
		return nil
	}
	envvar := corev1.EnvVar{
		Name: "HF_TOKEN",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: accessSecret},
				Key:                  "HF_TOKEN",
				Optional:             ptr.To(true),
			},
		},
	}
	for i := range spec.Containers {
		if spec.Containers[i].Name == ctx.Workspace.Name {
			spec.Containers[i].Env = append(spec.Containers[i].Env, envvar)
			break
		}
	}
	return nil
}

func SetModelDownloadInfo(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	if ctx.Model.GetInferenceParameters().DownloadAtRuntime {
		// HF_TOKEN is handled by SetHFToken.
		// DAR models just need the token present. no other download setup needed.
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

	// Find the main inference container by workspace name.
	mainIdx := -1
	for i := range spec.Containers {
		if spec.Containers[i].Name == ctx.Workspace.Name {
			mainIdx = i
			break
		}
	}
	if mainIdx == -1 {
		return fmt.Errorf("main inference container %q not found", ctx.Workspace.Name)
	}

	// Separate adapters by source type
	var imageAdapters []v1beta1.AdapterSpec
	var volumeAdapters []v1beta1.AdapterSpec
	for _, adapter := range ctx.Workspace.Inference.Adapters {
		if adapter.Source != nil && adapter.Source.Volume != nil {
			volumeAdapters = append(volumeAdapters, adapter)
		} else {
			imageAdapters = append(imageAdapters, adapter)
		}
	}

	// Handle image-based adapters (existing flow: EmptyDir + puller init containers)
	if len(imageAdapters) > 0 {
		adapterVolume, adapterVolumeMount := utils.ConfigAdapterVolume(nil)
		spec.Volumes = append(spec.Volumes, adapterVolume)
		spec.Containers[mainIdx].VolumeMounts = append(spec.Containers[mainIdx].VolumeMounts, adapterVolumeMount)

		// add container to pull adapters
		volumeMounts := []corev1.VolumeMount{adapterVolumeMount}
		pullerContainers, pullerEnvVars, pullerVolumes := manifests.GeneratePullerContainers(ctx.Workspace, imageAdapters, volumeMounts)
		spec.InitContainers = append(spec.InitContainers, pullerContainers...)
		spec.Volumes = append(spec.Volumes, pullerVolumes...)
		spec.Containers[mainIdx].Env = append(spec.Containers[mainIdx].Env, pullerEnvVars...)
	}

	// Handle volume-based adapters (mount volume directly, no puller needed)
	for _, adapter := range volumeAdapters {
		sourceName := adapter.Source.Name
		volumeName := fmt.Sprintf("adapter-volume-%s", sourceName)
		mountPath := fmt.Sprintf("%s/%s", utils.DefaultAdapterVolumePath, sourceName)

		volume := corev1.Volume{
			Name:         volumeName,
			VolumeSource: *adapter.Source.Volume,
		}
		volumeMount := corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
		}
		spec.Volumes = append(spec.Volumes, volume)
		spec.Containers[mainIdx].VolumeMounts = append(spec.Containers[mainIdx].VolumeMounts, volumeMount)

		// Propagate strength env vars for volume adapters
		if adapter.Strength != nil {
			envVar := corev1.EnvVar{
				Name:  sourceName,
				Value: *adapter.Strength,
			}
			spec.Containers[mainIdx].Env = append(spec.Containers[mainIdx].Env, envVar)
		}
	}

	return nil
}

// SetBenchmarkConfig overrides the startup probe to run the benchmark entrypoint.
// It must be appended after GenerateInferencePodSpec (and SetDistributedInferenceProbe
// when distributed) so the container already exists.
func SetBenchmarkConfig(distributed bool) generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, corev1.PodSpec] {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		inferenceParam := ctx.Model.GetInferenceParameters()
		readinessTimeout := inferenceParam.ReadinessTimeout
		if readinessTimeout <= 0 {
			readinessTimeout = defaultStartupProbeTimeout
		}

		var wObj *v1beta1.Workspace
		if distributed {
			wObj = ctx.Workspace
		}
		startupProbe := buildBenchmarkStartupProbe(readinessTimeout, wObj, distributed)

		for i := range spec.Containers {
			if spec.Containers[i].Name == ctx.Workspace.Name {
				spec.Containers[i].StartupProbe = startupProbe
				break
			}
		}
		return nil
	}
}

func SetDistributedInferenceProbe(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	readinessTimeout := ctx.Model.GetInferenceParameters().ReadinessTimeout
	if readinessTimeout <= 0 {
		readinessTimeout = defaultStartupProbeTimeout
	}

	// Determine vLLM port: decode pods use PortDecodeVLLM, others use default.
	var vllmPort int32
	if needsRoutingSidecar(ctx.Workspace) {
		vllmPort = consts.PortDecodeVLLM
	}

	// 60 seconds initial delay for liveness probe to allow workers to join the cluster
	livenessProbe := getDistributedInferenceProbe(probeTypeLiveness, ctx.Workspace, 60, 10, 5, 1, vllmPort)
	readinessProbe := getDistributedInferenceProbe(probeTypeReadiness, ctx.Workspace, 0, 10, 1, 1, vllmPort)
	startupProbe := buildDistributedStartupProbe(readinessTimeout, ctx.Workspace, vllmPort)
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
			spec.Containers[i].StartupProbe = startupProbe
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

// SetProvisionerNodeSelector appends provisioner-specific node selector
// requirements (e.g. kaito.sh/workspace, kaito.sh/workspacenamespace) to the
// pod's required node affinity, isolating pods to the nodes the provisioner
// created for this workspace. No-op when the provisioner returns no
// requirements (BYO mode).
func SetProvisionerNodeSelector(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
	if ctx.NodeProvisioner == nil {
		return nil
	}
	extra := ctx.NodeProvisioner.BuildNodeSelector(ctx.Ctx, ctx.Workspace)
	if len(extra) == 0 {
		return nil
	}

	if spec.Affinity == nil {
		spec.Affinity = &corev1.Affinity{}
	}
	if spec.Affinity.NodeAffinity == nil {
		spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	nodeSel := spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if nodeSel == nil {
		nodeSel = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{}},
		}
		spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSel
	}
	if len(nodeSel.NodeSelectorTerms) == 0 {
		nodeSel.NodeSelectorTerms = []corev1.NodeSelectorTerm{{}}
	}
	nodeSel.NodeSelectorTerms[0].MatchExpressions = append(nodeSel.NodeSelectorTerms[0].MatchExpressions, extra...)
	return nil
}

// applyInferenceRoleEnv sets inference-related env vars on the main inference
// container (identified by containerName) when the workspace has a valid
// inference-role label (prefill or decode). It injects:
//   - KAITO_INFERENCE_ROLE: role identification for the container
//   - VLLM_NIXL_SIDE_CHANNEL_HOST: pod IP for NIXL KV transfer side channel
//
// These env vars are set on the main inference container when the workspace has
// a prefill or decode role label. In practice, only vLLM workspaces created by
// MultiRoleInference carry this label.
// Note: the routing sidecar (injected by injectRoutingSidecar) independently
// sets VLLM_NIXL_SIDE_CHANNEL_HOST in its own container spec as well.
func applyInferenceRoleEnv(labels map[string]string, containerName string, spec *corev1.PodSpec) {
	role, ok := labels[v1beta1.LabelInferenceRole]
	if !ok || (role != string(kaitov1alpha1.MultiRoleInferenceRolePrefill) && role != string(kaitov1alpha1.MultiRoleInferenceRoleDecode)) {
		return
	}
	for i := range spec.Containers {
		if spec.Containers[i].Name == containerName {
			spec.Containers[i].Env = append(spec.Containers[i].Env, corev1.EnvVar{
				Name:  consts.InferenceRoleEnvName,
				Value: role,
			})
			// VLLM_NIXL_SIDE_CHANNEL_HOST is required for NIXL KV transfer
			// in P/D disaggregation. Without it, vLLM registers "localhost"
			// as the NIXL side channel host, causing cross-pod handshake failures.
			spec.Containers[i].Env = append(spec.Containers[i].Env, corev1.EnvVar{
				Name: "VLLM_NIXL_SIDE_CHANNEL_HOST",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			})
			return
		}
	}
}

// injectRoutingSidecar appends the llm-d routing sidecar container to the pod
// spec. The sidecar listens on PortInferenceServer (5000) and proxies to the
// main vLLM container on PortDecodeVLLM (5001).
// The command and probes are already configured with the correct port via
// RuntimeContext.InferencePort; this function only updates the container port
// declaration and adds the sidecar container.
func injectRoutingSidecar(spec *corev1.PodSpec) {
	if len(spec.Containers) == 0 {
		return
	}

	// Rewrite the main vLLM container port declaration from 5000 to 5001.
	for i := range spec.Containers[0].Ports {
		if spec.Containers[0].Ports[i].ContainerPort == consts.PortInferenceServer {
			spec.Containers[0].Ports[i].ContainerPort = consts.PortDecodeVLLM
		}
	}

	// Append the routing sidecar that listens on 5000 and proxies to vLLM on 5001.
	spec.Containers = append(spec.Containers, corev1.Container{
		Name:  "llm-d-routing-sidecar",
		Image: fmt.Sprintf("%s:%s", consts.RoutingSidecarImage, consts.RoutingSidecarTag),
		Args: []string{
			fmt.Sprintf("--port=%d", consts.PortInferenceServer),
			fmt.Sprintf("--vllm-port=%d", consts.PortDecodeVLLM),
			"--secure-proxy=false",
		},
		Ports: []corev1.ContainerPort{
			{ContainerPort: consts.PortInferenceServer, Name: "sidecar", Protocol: corev1.ProtocolTCP},
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
			{
				Name: "VLLM_NIXL_SIDE_CHANNEL_HOST",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
		},
	})
}

// needsRoutingSidecar returns true if the workspace requires the llm-d routing sidecar.
func needsRoutingSidecar(ws *v1beta1.Workspace) bool {
	role, ok := ws.Labels[v1beta1.LabelInferenceRole]
	if !ok || role != string(kaitov1alpha1.MultiRoleInferenceRoleDecode) {
		return false
	}
	return v1beta1.GetWorkspaceRuntimeName(ws) == pkgmodel.RuntimeNameVLLM
}
