// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package controllers

import (
	"context"
	"fmt"
	"os"

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
	ProbePath           = "/health"
	PortInferenceServer = 5000
)

var (
	containerPorts = []corev1.ContainerPort{{
		ContainerPort: int32(PortInferenceServer),
	},
	}

	livenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(PortInferenceServer),
				Path: ProbePath,
			},
		},
		InitialDelaySeconds: 600, // 10 minutes
		PeriodSeconds:       10,
	}

	readinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(PortInferenceServer),
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

	shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
	volumes = append(volumes, shmVolume)
	volumeMounts = append(volumeMounts, shmVolumeMount)

	var resourceReq corev1.ResourceRequirements

	if ragEngineObj.Spec.Embedding.Local != nil {
		var skuNumGPUs int
		gpuConfig, err := utils.GetGPUConfigBySKU(ragEngineObj.Spec.Compute.InstanceType)
		if err != nil {
			gpuConfig, err = utils.TryGetGPUConfigFromNode(ctx, kubeClient, ragEngineObj.Status.WorkerNodes)
			if err != nil {
				skuNumGPUs = 1
			}
		}
		if gpuConfig != nil {
			skuNumGPUs = gpuConfig.GPUCount
		}

		resourceReq = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceName(resources.CapacityNvidiaGPU): *resource.NewQuantity(int64(skuNumGPUs), resource.DecimalSI),
			},
		}

	}
	commands := utils.ShellCmd("python3 main.py")

	registryName := os.Getenv("PRESET_RAG_REGISTRY_NAME")
	if registryName == "" {
		registryName = "aimodelsregistrytest.azurecr.io"
	}

	imageName := os.Getenv("PRESET_RAG_IMAGE_NAME")
	if imageName == "" {
		imageName = "kaito-rag-service"
	}

	imageVersion := os.Getenv("PRESET_RAG_IMAGE_TAG")
	if imageVersion == "" {
		imageVersion = "0.3.2"
	}

	image := fmt.Sprintf("%s/%s:%s", registryName, imageName, imageVersion)

	imagePullSecretRefs := []corev1.LocalObjectReference{}

	depObj := manifests.GenerateRAGDeploymentManifest(ragEngineObj, revisionNum, image, imagePullSecretRefs, *ragEngineObj.Spec.Compute.Count, commands,
		containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)

	err := resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
