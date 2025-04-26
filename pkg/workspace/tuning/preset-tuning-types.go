// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package tuning

import corev1 "k8s.io/api/core/v1"

const (
	DefaultNumProcesses = "1"
	DefaultNumMachines  = "1"
	DefaultMachineRank  = "0"
	DefaultGPUIds       = "all"
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
