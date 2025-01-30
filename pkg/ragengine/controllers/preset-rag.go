// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/ragengine/manifests"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
)

const (
	ProbePath = "/health"
	Port5000  = 5000
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: int32(Port5000),
	},
	}

	livenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(Port5000),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	readinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(Port5000),
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

func CreatePresetRAG(ctx context.Context, ragEngineObj *v1alpha1.RAGEngine, revisionNum string, kubeClient client.Client) (client.Object, error) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	shmVolume, shmVolumeMount := utils.ConfigSHMVolume(*ragEngineObj.Spec.Compute.Count)
	if shmVolume.Name != "" {
		volumes = append(volumes, shmVolume)
	}
	if shmVolumeMount.Name != "" {
		volumeMounts = append(volumeMounts, shmVolumeMount)
	}

	var resourceReq corev1.ResourceRequirements

	if ragEngineObj.Spec.Embedding.Local != nil {
		skuNumGPUs, err := utils.GetSKUNumGPUs(ctx, kubeClient, ragEngineObj.Status.WorkerNodes,
			ragEngineObj.Spec.Compute.InstanceType, "1")
		if err != nil {
			return nil, fmt.Errorf("failed to get SKU num GPUs: %v", err)
		}

		resourceReq = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse(skuNumGPUs),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): resource.MustParse(skuNumGPUs),
			},
		}

	}
	commands := utils.ShellCmd("python3 main.py")
	// TODO: provide this image
	image := "aimodelsregistrytest.azurecr.io/kaito-ragengine:0.0.1"

	imagePullSecretRefs := []corev1.LocalObjectReference{}

	depObj := manifests.GenerateRAGDeploymentManifest(ctx, ragEngineObj, revisionNum, image, imagePullSecretRefs, *ragEngineObj.Spec.Compute.Count, commands,
		containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)

	err := resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
