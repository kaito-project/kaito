// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package rag

import (
	"context"

	"github.com/azure/kaito/pkg/utils"
	"github.com/azure/kaito/pkg/utils/consts"

	kaitov1alpha1 "github.com/azure/kaito/api/v1alpha1"
	"github.com/azure/kaito/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ProbePath = "/healthz"
	Port5000  = int32(5000)
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: Port5000,
	},
	}

	livenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(5000),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	readinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(5000),
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

func CreatePresetRAG(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine, kubeClient client.Client) (client.Object, error) {
	// TODO: use real data instead of dummy ones
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	shmVolume, shmVolumeMount := utils.ConfigSHMVolume(*ragEngineObj.Spec.Compute.Count)
	if shmVolume.Name != "" {
		volumes = append(volumes, shmVolume)
	}
	if shmVolumeMount.Name != "" {
		volumeMounts = append(volumeMounts, shmVolumeMount)
	}

	commands := utils.ShellCmd("python3 main.py")
	resourceReq := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse("2"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse("2"),
		},
	}

	image := "kaito-ragengine:0.0.1"
	imagePullSecretRefs := []corev1.LocalObjectReference{}

	depObj := resources.GenerateRAGDeploymentManifest(ctx, ragEngineObj, image, imagePullSecretRefs, *ragEngineObj.Spec.Compute.Count, commands,
		containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)

	err := resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
