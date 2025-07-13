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
