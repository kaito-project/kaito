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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/generator"
)

const (
	lmCacheSidecarName            = "lmcache-mp-server"
	lmCacheRuntimeVolumeName      = "lmcache-runtime"
	lmCacheRuntimeMountPath       = "/var/run/kaito-lmcache"
	lmCacheDeploymentModeEnvName  = "KAITO_LMCACHE_DEPLOYMENT_MODE"
	lmCacheDeploymentModeSidecar  = "sidecar"
	lmCacheSidecarStartupAttempts = 60
	lmCacheProbeTimeoutSeconds    = 5
)

type lmCacheResolvedConfig struct {
	runtimeName pkgmodel.RuntimeName
	runParams   map[string]string
	// Reuse the target resolved during pod generation, including label-only BYO nodes.
	targetCPU bool
}

// ConfigureLMCacheSidecar injects LMCache as a native Kubernetes sidecar when
// the resolved workload supports multiprocess CPU KV offload.
func ConfigureLMCacheSidecar(resolved *lmCacheResolvedConfig) generator.TypedManifestModifier[generator.WorkspaceGeneratorContext, corev1.PodSpec] {
	return func(ctx *generator.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		// CPU KV offload extends GPU inference with host memory; the LMCache
		// server itself requires CUDA and must never gate a CPU inference pod.
		if resolved.targetCPU {
			klog.InfoS("LMCache sidecar disabled", "workspace", klog.KObj(ctx.Workspace), "reason", "CPU inference is unsupported")
			return nil
		}
		eligible, reason := isLMCacheWorkloadEligible(ctx.Workspace, resolved.runtimeName, resolved.runParams)
		if !eligible {
			klog.InfoS("LMCache sidecar disabled", "workspace", klog.KObj(ctx.Workspace), "reason", reason)
			return nil
		}
		eligible, reason, err := isLMCacheUserConfigEligible(ctx.Ctx, ctx.KubeClient, ctx.Workspace)
		if err != nil {
			return err
		}
		if !eligible {
			klog.InfoS("LMCache sidecar disabled", "workspace", klog.KObj(ctx.Workspace), "reason", reason)
			return nil
		}
		tensorParallelSize := resolved.runParams["tensor-parallel-size"]
		if tensorParallelSize == "" {
			tensorParallelSize = "1"
		}
		injectLMCacheSidecar(spec, ctx.Workspace.Name, tensorParallelSize)
		return nil
	}
}

// isLMCacheUserConfigEligible checks whether the Workspace inference ConfigMap
// permits KAITO to manage the LMCache sidecar. It returns errors for unavailable
// or invalid config and rejects settings that require a different KV topology.
func isLMCacheUserConfigEligible(ctx context.Context, kubeClient client.Client, workspace *v1beta1.Workspace) (bool, string, error) {
	if workspace.Inference == nil || workspace.Inference.Config == "" {
		return true, "", nil
	}
	configMap := &corev1.ConfigMap{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Namespace: workspace.Namespace, Name: workspace.Inference.Config}, configMap); err != nil {
		return false, "", fmt.Errorf("get inference config ConfigMap %s/%s: %w", workspace.Namespace, workspace.Inference.Config, err)
	}
	configData, found := configMap.Data[utils.InferenceConfigKey]
	if !found {
		return true, "", nil
	}
	runtimeConfig, err := utils.ParseInferenceRuntimeConfig(configData)
	if err != nil {
		return false, "", fmt.Errorf("parse inference config ConfigMap %s/%s: %w", workspace.Namespace, workspace.Inference.Config, err)
	}
	if runtimeConfig.KVCacheCPUMemoryUtilization != nil && *runtimeConfig.KVCacheCPUMemoryUtilization <= 0 {
		return false, "CPU KV offload is disabled in inference config", nil
	}
	if _, configured := runtimeConfig.VLLM["kv-transfer-config"]; configured {
		return false, "custom KV connector configuration owns its server lifecycle", nil
	}
	if _, configured := runtimeConfig.VLLM["data-parallel-size"]; configured {
		return false, "data parallelism is unsupported", nil
	}
	if _, configured := runtimeConfig.VLLM["pipeline-parallel-size"]; configured {
		return false, "multi-node pipeline parallelism is unsupported", nil
	}
	return true, "", nil
}

func isLMCacheWorkloadEligible(workspace *v1beta1.Workspace, runtimeName pkgmodel.RuntimeName,
	runParams map[string]string,
) (bool, string) {
	if runtimeName != pkgmodel.RuntimeNameVLLM {
		return false, "runtime is not vLLM"
	}
	if partition := workspace.Resource.Partition; partition != nil && partition.Mode == v1beta1.PartitionModeMIG {
		return false, "MIG is unsupported"
	}
	if workspace.Labels[v1beta1.LabelInferenceRole] != "" {
		return false, "prefill/decode disaggregation owns the KV connector"
	}
	if value, found := runParams["kaito-kv-cache-cpu-memory-utilization"]; found {
		if utilization, err := strconv.ParseFloat(value, 64); err == nil && utilization <= 0 {
			return false, "CPU KV offload is disabled"
		}
	}
	if _, configured := runParams["kv-transfer-config"]; configured {
		return false, "custom KV connector configuration owns its server lifecycle"
	}
	if _, configured := runParams["data-parallel-size"]; configured {
		return false, "data parallelism is unsupported"
	}
	if _, configured := runParams["pipeline-parallel-size"]; configured {
		return false, "multi-node pipeline parallelism is unsupported"
	}
	return true, ""
}

func injectLMCacheSidecar(spec *corev1.PodSpec, mainContainerName, tensorParallelSize string) {
	mainIndex := -1
	for i := range spec.Containers {
		if spec.Containers[i].Name == mainContainerName {
			mainIndex = i
			break
		}
	}
	if mainIndex < 0 {
		return
	}

	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name:         lmCacheRuntimeVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	runtimeMount := corev1.VolumeMount{Name: lmCacheRuntimeVolumeName, MountPath: lmCacheRuntimeMountPath}
	mainContainer := &spec.Containers[mainIndex]
	mainContainer.VolumeMounts = append(mainContainer.VolumeMounts, runtimeMount)
	mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{
		Name: lmCacheDeploymentModeEnvName, Value: lmCacheDeploymentModeSidecar,
	})
	mainContainer.LivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
			"python3", "/workspace/vllm/lmcache_server.py", "--health-check",
		}}},
		PeriodSeconds: 10, TimeoutSeconds: lmCacheProbeTimeoutSeconds, FailureThreshold: 3,
	}

	args := []string{"--isolated-ipc", "--tensor-parallel-size=" + tensorParallelSize}
	sidecarMounts := []corev1.VolumeMount{runtimeMount}
	for _, mount := range mainContainer.VolumeMounts {
		if mount.MountPath == utils.DefaultConfigMapMountPath {
			sidecarMounts = append(sidecarMounts, mount)
			args = append(args, "--config-file="+utils.DefaultConfigMapMountPath+"/"+utils.InferenceConfigKey)
			break
		}
	}

	restartAlways := corev1.ContainerRestartPolicyAlways
	allowPrivilegeEscalation := false
	serverHealthProbe := corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
		"python3", "/workspace/vllm/lmcache_server.py", "--server-health-check",
	}}}
	spec.InitContainers = append(spec.InitContainers, corev1.Container{
		Name:          lmCacheSidecarName,
		Image:         GetBaseImageName(),
		Command:       []string{"python3", "/workspace/vllm/lmcache_server.py"},
		Args:          args,
		RestartPolicy: &restartAlways,
		// LMCache must open CUDA IPC handles for the same GPUs as vLLM, but a
		// sibling container cannot share vLLM's device-plugin allocation. KAITO
		// uses dedicated GPU nodes, so expose their GPUs explicitly; compute
		// enables CUDA and utility provides the driver/NVML discovery APIs.
		Env: []corev1.EnvVar{
			{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"},
			{Name: "NVIDIA_DRIVER_CAPABILITIES", Value: "compute,utility"},
		},
		StartupProbe: &corev1.Probe{
			ProbeHandler:  serverHealthProbe,
			PeriodSeconds: 5, TimeoutSeconds: lmCacheProbeTimeoutSeconds, FailureThreshold: lmCacheSidecarStartupAttempts,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:  serverHealthProbe,
			PeriodSeconds: 5, TimeoutSeconds: lmCacheProbeTimeoutSeconds, FailureThreshold: 3,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler:  serverHealthProbe,
			PeriodSeconds: 10, TimeoutSeconds: lmCacheProbeTimeoutSeconds, FailureThreshold: 3,
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		VolumeMounts: sidecarMounts,
	})
}
