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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/k8s/nodes"
	"github.com/kaito-project/kaito/internal/manifests/builder"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

const (
	vllmPreloadSidecarName = "vllm-preload"
	vllmPreloadVolumeName  = "vllm-preload"
)

// vllmPreloadResolvedConfig carries the daemon command built with the server's
// model args from GenerateInferencePodSpec to ConfigureVLLMPreloadSidecar.
type vllmPreloadResolvedConfig struct {
	command          []string
	readinessTimeout time.Duration
}

// isVLLMPreloadEligible reports whether the workload can run the vllm preload
// daemon: an opted-in, single-node vLLM GPU workload that doesn't stream its
// weights (the server's disk fallback can't read a streamed source).
func isVLLMPreloadEligible(workspace *v1beta1.Workspace, runtimeName pkgmodel.RuntimeName,
	targetCPU, distributed bool, streamingModelPath string,
) (bool, string) {
	switch {
	case !v1beta1.UseVLLMPreload(workspace):
		return false, "not enabled"
	case runtimeName != pkgmodel.RuntimeNameVLLM:
		return false, "runtime is not vLLM"
	case targetCPU:
		return false, "CPU inference is unsupported"
	case distributed:
		return false, "multi-node inference is unsupported"
	case workspace.Resource.Partition != nil && workspace.Resource.Partition.Mode == v1beta1.PartitionModeMIG:
		return false, "MIG is unsupported"
	case streamingModelPath != "":
		return false, "streamed weights are unsupported"
	}
	return true, ""
}

// ConfigureVLLMPreloadSidecar runs vllm preload in a native sidecar that holds
// the pod's GPUs and keeps the weights resident on them, so a restart of the
// inference container maps them over CUDA IPC instead of reloading them. It
// runs after the other modifiers so the sidecar gets the main container's
// final mounts and env.
func ConfigureVLLMPreloadSidecar(resolved *vllmPreloadResolvedConfig) builder.TypedManifestModifier[builder.WorkspaceGeneratorContext, corev1.PodSpec] {
	return func(ctx *builder.WorkspaceGeneratorContext, spec *corev1.PodSpec) error {
		if len(resolved.command) == 0 {
			return nil
		}
		injectVLLMPreloadSidecar(spec, ctx.Workspace.Name, resolved.command, resolved.readinessTimeout)
		klog.InfoS("vLLM preload sidecar enabled", "workspace", klog.KObj(ctx.Workspace))
		return nil
	}
}

func injectVLLMPreloadSidecar(spec *corev1.PodSpec, mainContainerName string, command []string, readinessTimeout time.Duration) {
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
	main := &spec.Containers[mainIndex]

	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name:         vllmPreloadVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	})
	preloadMount := corev1.VolumeMount{Name: vllmPreloadVolumeName, MountPath: pkgmodel.VLLMPreloadSocketDir}

	// The sidecar loads the same weights the same way, so it gets the main
	// container's image, mounts and env.
	restartAlways := corev1.ContainerRestartPolicyAlways
	sidecar := corev1.Container{
		Name:          vllmPreloadSidecarName,
		Image:         main.Image,
		Command:       command,
		Env:           upsertEnv(append([]corev1.EnvVar(nil), main.Env...), "NVIDIA_DRIVER_CAPABILITIES", "compute,utility"),
		VolumeMounts:  append(append([]corev1.VolumeMount(nil), main.VolumeMounts...), preloadMount),
		Resources:     *main.Resources.DeepCopy(),
		RestartPolicy: &restartAlways,
		StartupProbe:  buildStartupProbe(readinessTimeout, pkgmodel.VLLMPreloadHealthPort),
	}
	spec.InitContainers = append(spec.InitContainers, sidecar)

	// The sidecar holds the GPU request. A second request in the main container
	// would double the pod's GPUs, so the server sees the node's GPUs and pins
	// itself to the sidecar's list (see VLLMPreloadServer).
	gpu := corev1.ResourceName(nodes.CapacityNvidiaGPU)
	delete(main.Resources.Requests, gpu)
	delete(main.Resources.Limits, gpu)
	main.Env = upsertEnv(main.Env, "NVIDIA_VISIBLE_DEVICES", "all")
	main.Env = upsertEnv(main.Env, "NVIDIA_DRIVER_CAPABILITIES", "compute,utility")
	main.VolumeMounts = append(main.VolumeMounts, preloadMount)
}

// upsertEnv sets name to value, replacing an existing entry.
func upsertEnv(env []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			env[i] = corev1.EnvVar{Name: name, Value: value}
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}
