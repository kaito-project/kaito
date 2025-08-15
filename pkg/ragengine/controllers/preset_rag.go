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
	"github.com/kaito-project/kaito/pkg/sku"
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

type ImageConfig struct {
	RegistryName string
	ImageName    string
	ImageTag     string
}

func (ic ImageConfig) GetImage() string {
	return fmt.Sprintf("%s/%s:%s", ic.RegistryName, ic.ImageName, ic.ImageTag)
}

func getImageConfig() ImageConfig {
	return ImageConfig{
		RegistryName: getEnv("PRESET_RAG_REGISTRY_NAME", "aimodelsregistrytest.azurecr.io"),
		ImageName:    getEnv("PRESET_RAG_IMAGE_NAME", "kaito-rag-service"),
		ImageTag:     getEnv("PRESET_RAG_IMAGE_TAG", "0.3.2"),
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func CreatePresetRAG(ctx context.Context, ragEngineObj *v1alpha1.RAGEngine, revisionNum string, kubeClient client.Client) (client.Object, error) {
	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	shmVolume, shmVolumeMount := utils.ConfigSHMVolume()
	volumes = append(volumes, shmVolume)
	volumeMounts = append(volumeMounts, shmVolumeMount)

	var resourceReq corev1.ResourceRequirements

	if ragEngineObj.Spec.Embedding.Local != nil {
		var skuNumGPUs int
		var gpuConfig *sku.GPUConfig = nil
		var err error
		if len(ragEngineObj.Spec.Compute.PreferredNodes) == 0 {
			gpuConfig, _ = utils.GetGPUConfigBySKU(ragEngineObj.Spec.Compute.InstanceType)
		}
		if gpuConfig != nil {
			skuNumGPUs = gpuConfig.GPUCount
		} else {
			gpuConfig, err = utils.TryGetGPUConfigFromNode(ctx, kubeClient, ragEngineObj.Status.WorkerNodes)
			if err != nil {
				skuNumGPUs = 0
			} else if gpuConfig != nil {
				skuNumGPUs = gpuConfig.GPUCount
			}
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

	image := getImageConfig().GetImage()

	imagePullSecretRefs := []corev1.LocalObjectReference{}

	depObj := manifests.GenerateRAGDeploymentManifest(ragEngineObj, revisionNum, image, imagePullSecretRefs, *ragEngineObj.Spec.Compute.Count, commands,
		containerPorts, livenessProbe, readinessProbe, resourceReq, tolerations, volumes, volumeMounts)

	err := resources.CreateResource(ctx, depObj, kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
