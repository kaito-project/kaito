// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	DefaultNnodes       = "1"
	DefaultNprocPerNode = "1"
	DefaultNodeRank     = "0"
	DefaultMasterAddr   = "localhost"
	DefaultMasterPort   = "29500"
)

// Torch Rendezvous Params
const (
	DefaultMaxRestarts  = "3"
	DefaultRdzvId       = "rdzv_id"
	DefaultRdzvBackend  = "c10d"            // Pytorch Native Distributed data store
	DefaultRdzvEndpoint = "localhost:29500" // e.g. llama-2-13b-chat-0.llama-headless.default.svc.cluster.local:29500
)

const (
	DefaultConfigFile   = "config.yaml"
	DefaultNumProcesses = "1"
	DefaultNumMachines  = "1"
	DefaultMachineRank  = "0"
	DefaultGPUIds       = "all"
)

// TODO: remove the above local variables starting with lower cases.
var (
	DefaultTorchRunParams = map[string]string{
		"nnodes":         DefaultNnodes,
		"nproc_per_node": DefaultNprocPerNode,
		"node_rank":      DefaultNodeRank,
		"master_addr":    DefaultMasterAddr,
		"master_port":    DefaultMasterPort,
	}

	DefaultTorchRunRdzvParams = map[string]string{
		"max_restarts":  DefaultMaxRestarts,
		"rdzv_id":       DefaultRdzvId,
		"rdzv_backend":  DefaultRdzvBackend,
		"rdzv_endpoint": DefaultRdzvEndpoint,
	}

	DefaultAccelerateParams = map[string]string{
		"num_processes": DefaultNumProcesses,
		"num_machines":  DefaultNumMachines,
		"machine_rank":  DefaultMachineRank,
		"gpu_ids":       DefaultGPUIds,
	}

	DefaultVLLMCommand         = "python3 /workspace/vllm/inference_api.py"
	DefaultTransformersMainFile = "/workspace/tfs/inference_api.py"

	DefaultImagePullSecrets = []corev1.LocalObjectReference{}
)
