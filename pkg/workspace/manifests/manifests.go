// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package manifests

import (
	"context"
	"fmt"
	"path"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/workspace/image"
)

var controller = true

func GenerateHeadlessServiceManifest(ctx context.Context, workspaceObj *kaitov1beta1.Workspace) *corev1.Service {
	serviceName := fmt.Sprintf("%s-headless", workspaceObj.Name)
	selector := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}

	return &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      serviceName,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1beta1.GroupVersion.String(),
					Kind:       "Workspace",
					UID:        workspaceObj.UID,
					Name:       workspaceObj.Name,
					Controller: &controller,
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector:  selector,
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:       "torchrun",
					Protocol:   corev1.ProtocolTCP,
					Port:       29500,
					TargetPort: intstr.FromInt32(29500),
				},
			},
			PublishNotReadyAddresses: true,
		},
	}
}

func GenerateServiceManifest(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, serviceType corev1.ServiceType, isStatefulSet bool) *corev1.Service {
	selector := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}
	// If statefulset, modify the selector to select the pod with index 0 as the endpoint
	if isStatefulSet {
		podNameForIndex0 := fmt.Sprintf("%s-0", workspaceObj.Name)
		selector["statefulset.kubernetes.io/pod-name"] = podNameForIndex0
	}

	return &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1beta1.GroupVersion.String(),
					Kind:       "Workspace",
					UID:        workspaceObj.UID,
					Name:       workspaceObj.Name,
					Controller: &controller,
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Ports: []corev1.ServicePort{
				// HTTP API Port
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt32(5000),
				},
				// Torch NCCL Port
				{
					Name:       "torch",
					Protocol:   corev1.ProtocolTCP,
					Port:       29500,
					TargetPort: intstr.FromInt32(29500),
				},
			},
			Selector: selector,
			// Added this to allow pods to discover each other
			// (DNS Resolution) During their initialization phase
			PublishNotReadyAddresses: true,
		},
	}
}

func GenerateStatefulSetManifest(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, imageName string,
	imagePullSecretRefs []corev1.LocalObjectReference, replicas int, commands []string, containerPorts []corev1.ContainerPort,
	livenessProbe, readinessProbe *corev1.Probe, resourceRequirements corev1.ResourceRequirements,
	tolerations []corev1.Toleration, volumes []corev1.Volume, volumeMount []corev1.VolumeMount) *appsv1.StatefulSet {

	nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(workspaceObj.Resource.LabelSelector.MatchLabels))
	for key, value := range workspaceObj.Resource.LabelSelector.MatchLabels {
		nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
			Key:      key,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{value},
		})
	}

	selector := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}
	labelselector := &v1.LabelSelector{
		MatchLabels: selector,
	}
	// Add PYTORCH_CUDA_ALLOC_CONF environment variable
	envVars := []corev1.EnvVar{
		{
			Name:  "PYTORCH_CUDA_ALLOC_CONF",
			Value: "expandable_segments:True",
		},
	}

	ss := &appsv1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1beta1.GroupVersion.String(),
					Kind:       "Workspace",
					UID:        workspaceObj.UID,
					Name:       workspaceObj.Name,
					Controller: &controller,
				},
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            lo.ToPtr(int32(replicas)),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector:            labelselector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: imagePullSecretRefs,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: nodeRequirements,
									},
								},
							},
						},
					},

					Containers: []corev1.Container{
						{
							Name:           workspaceObj.Name,
							Image:          imageName,
							Command:        commands,
							Resources:      resourceRequirements,
							LivenessProbe:  livenessProbe,
							ReadinessProbe: readinessProbe,
							Ports:          containerPorts,
							VolumeMounts:   volumeMount,
							Env:            envVars,
						},
					},
					Tolerations: tolerations,
					Volumes:     volumes,
				},
			},
		},
	}
	ss.Spec.ServiceName = fmt.Sprintf("%s-headless", workspaceObj.Name)
	return ss
}

func GenerateTuningJobManifest(ctx context.Context, wObj *kaitov1beta1.Workspace, revisionNum string, imageName string,
	imagePullSecretRefs []corev1.LocalObjectReference, replicas int, commands []string, containerPorts []corev1.ContainerPort,
	livenessProbe, readinessProbe *corev1.Probe, resourceRequirements corev1.ResourceRequirements, tolerations []corev1.Toleration,
	initContainers []corev1.Container, sidecarContainers []corev1.Container, volumes []corev1.Volume, volumeMounts []corev1.VolumeMount,
	envVars []corev1.EnvVar) *batchv1.Job {
	labels := map[string]string{
		kaitov1beta1.LabelWorkspaceName: wObj.Name,
	}

	// TODO: make containers only mount the volumes they need

	for i := range initContainers {
		initContainers[i].VolumeMounts = append(initContainers[i].VolumeMounts, volumeMounts...)
	}

	for i := range sidecarContainers {
		sidecarContainers[i].VolumeMounts = append(sidecarContainers[i].VolumeMounts, volumeMounts...)
	}

	// Construct the complete list of containers (main and sidecars)
	containers := append([]corev1.Container{
		{
			Name:           wObj.Name,
			Image:          imageName,
			Command:        commands,
			Resources:      resourceRequirements,
			LivenessProbe:  livenessProbe,
			ReadinessProbe: readinessProbe,
			Ports:          containerPorts,
			VolumeMounts:   volumeMounts,
			Env:            envVars,
		},
	}, sidecarContainers...)

	return &batchv1.Job{
		TypeMeta: v1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      wObj.Name,
			Namespace: wObj.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				kaitov1beta1.WorkspaceRevisionAnnotation: revisionNum,
			},
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1beta1.GroupVersion.String(),
					Kind:       "Workspace",
					Name:       wObj.Name,
					UID:        wObj.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					InitContainers:        initContainers,
					Containers:            containers,
					RestartPolicy:         corev1.RestartPolicyNever,
					ShareProcessNamespace: ptr.To(true),
					Volumes:               volumes,
					Tolerations:           tolerations,
					ImagePullSecrets:      imagePullSecretRefs,
				},
			},
		},
	}
}

func GenerateDeploymentManifest(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, revisionNum string, imageName string,
	imagePullSecretRefs []corev1.LocalObjectReference, replicas int, commands []string, containerPorts []corev1.ContainerPort,
	livenessProbe, readinessProbe *corev1.Probe, resourceRequirements corev1.ResourceRequirements,
	tolerations []corev1.Toleration, volumes []corev1.Volume, volumeMount []corev1.VolumeMount) *appsv1.Deployment {

	nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(workspaceObj.Resource.LabelSelector.MatchLabels))
	for key, value := range workspaceObj.Resource.LabelSelector.MatchLabels {
		nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
			Key:      key,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{value},
		})
	}

	selector := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}
	labelselector := &v1.LabelSelector{
		MatchLabels: selector,
	}
	envs := []corev1.EnvVar{}
	// Add PYTORCH_CUDA_ALLOC_CONF environment variable
	envs = append(envs, corev1.EnvVar{
		Name:  "PYTORCH_CUDA_ALLOC_CONF",
		Value: "expandable_segments:True",
	})

	pullerContainers, pullerEnvVars, pullerVolumes := GeneratePullerContainers(workspaceObj, volumeMount)
	envs = append(envs, pullerEnvVars...)
	volumes = append(volumes, pullerVolumes...)

	return &appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1beta1.GroupVersion.String(),
					Kind:       "Workspace",
					UID:        workspaceObj.UID,
					Name:       workspaceObj.Name,
					Controller: &controller,
				},
			},
			Annotations: map[string]string{
				kaitov1beta1.WorkspaceRevisionAnnotation: revisionNum,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr(int32(replicas)),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				}, // Configuration for rolling updates: allows no extra pods during the update and permits at most one unavailable pod at a time。
			},
			Selector: labelselector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: imagePullSecretRefs,
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: nodeRequirements,
									},
								},
							},
						},
					},
					InitContainers: pullerContainers,
					Containers: []corev1.Container{
						{
							Name:           workspaceObj.Name,
							Image:          imageName,
							Command:        commands,
							Resources:      resourceRequirements,
							LivenessProbe:  livenessProbe,
							ReadinessProbe: readinessProbe,
							Ports:          containerPorts,
							VolumeMounts:   volumeMount,
							Env:            envs,
						},
					},
					Tolerations: tolerations,
					Volumes:     volumes,
				},
			},
		},
	}
}

func GeneratePullerContainers(wObj *kaitov1beta1.Workspace, volumeMounts []corev1.VolumeMount) ([]corev1.Container, []corev1.EnvVar, []corev1.Volume) {
	size := len(wObj.Inference.Adapters)

	initContainers := make([]corev1.Container, 0, size)
	var envVars []corev1.EnvVar
	volumes := make([]corev1.Volume, 0, size)

	for _, adapter := range wObj.Inference.Adapters {
		source := adapter.Source
		sourceName := source.Name

		volume, volumeMount := utils.ConfigImagePullSecretVolume(sourceName+"-inference-adapter", source.ImagePullSecrets)
		volumes = append(volumes, volume)

		if adapter.Strength != nil {
			envVar := corev1.EnvVar{
				Name:  sourceName,
				Value: *adapter.Strength,
			}
			envVars = append(envVars, envVar)
		}

		outputDirectory := path.Join("/mnt/adapter", sourceName)
		pullerContainer := image.NewPullerContainer(source.Image, outputDirectory)
		pullerContainer.Name += "-" + sourceName
		pullerContainer.VolumeMounts = append(volumeMounts, volumeMount)
		initContainers = append(initContainers, *pullerContainer)
	}

	return initContainers, envVars, volumes
}

func GenerateDeploymentManifestWithPodTemplate(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, tolerations []corev1.Toleration) *appsv1.Deployment {
	nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(workspaceObj.Resource.LabelSelector.MatchLabels))
	for key, value := range workspaceObj.Resource.LabelSelector.MatchLabels {
		nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
			Key:      key,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{value},
		})
	}

	templateCopy := workspaceObj.Inference.Template.DeepCopy()

	if templateCopy.ObjectMeta.Labels == nil {
		templateCopy.ObjectMeta.Labels = make(map[string]string)
	}
	templateCopy.ObjectMeta.Labels[kaitov1beta1.LabelWorkspaceName] = workspaceObj.Name
	labelselector := &v1.LabelSelector{
		MatchLabels: map[string]string{
			kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
		},
	}
	// Overwrite affinity
	templateCopy.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: nodeRequirements,
					},
				},
			},
		},
	}

	// append tolerations
	if templateCopy.Spec.Tolerations == nil {
		templateCopy.Spec.Tolerations = tolerations
	} else {
		templateCopy.Spec.Tolerations = append(templateCopy.Spec.Tolerations, tolerations...)
	}

	return &appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1beta1.GroupVersion.String(),
					Kind:       "Workspace",
					UID:        workspaceObj.UID,
					Name:       workspaceObj.Name,
					Controller: &controller,
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr(int32(*workspaceObj.Resource.Count)),
			Selector: labelselector,
			Template: *templateCopy,
		},
	}
}
