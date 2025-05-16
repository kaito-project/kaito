// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultNumProcesses = "1"
	DefaultNumMachines  = "1"
	DefaultMachineRank  = "0"
	DefaultGPUIds       = "all"

	DefaultVLLMRayLeaderBaseCommand        = "/workspace/vllm/multi-node-serving.sh leader"
	DefaultVLLMRayWorkerBaseCommand        = "/workspace/vllm/multi-node-serving.sh worker"
	DefaultVLLMMultiNodeHealthCheckCommand = "python3 /workspace/vllm/multi-node-health-check.py"
	DefaultVLLMCommand                     = "python3 /workspace/vllm/inference_api.py"
	DefaultTransformersMainFile            = "/workspace/tfs/inference_api.py"
)

var (
	DefaultAccelerateParams = map[string]string{
		"num_processes": DefaultNumProcesses,
		"num_machines":  DefaultNumMachines,
		"machine_rank":  DefaultMachineRank,
		"gpu_ids":       DefaultGPUIds,
	}

	DefaultImagePullSecrets = []corev1.LocalObjectReference{}
)
