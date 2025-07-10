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

package manifests

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gaiev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"

	"github.com/kaito-project/kaito/api/v1beta1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/image"
)

func GenerateHeadlessServiceManifest(workspaceObj *kaitov1beta1.Workspace) *corev1.Service {
	serviceName := fmt.Sprintf("%s-headless", workspaceObj.Name)
	selector := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}

	return &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      serviceName,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: corev1.ServiceSpec{
			Selector:                 selector,
			ClusterIP:                "None",
			Ports:                    []corev1.ServicePort{},
			PublishNotReadyAddresses: true,
		},
	}
}

func GenerateServiceManifest(workspaceObj *kaitov1beta1.Workspace, serviceType corev1.ServiceType, isStatefulSet bool) *corev1.Service {
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
				*v1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
				{
					Name:       "ray",
					Protocol:   corev1.ProtocolTCP,
					Port:       6379,
					TargetPort: intstr.FromInt32(6379),
				},
				{
					Name:       "dashboard",
					Protocol:   corev1.ProtocolTCP,
					Port:       8265,
					TargetPort: intstr.FromInt32(8265),
				},
			},
			Selector: selector,
			// Added this to allow pods to discover each other
			// (DNS Resolution) During their initialization phase
			PublishNotReadyAddresses: true,
		},
	}
}

func GenerateStatefulSetManifest(revisionNum string, replicas int) func(*generator.WorkspaceGeneratorContext, *appsv1.StatefulSet) error {
	return func(ctx *generator.WorkspaceGeneratorContext, ss *appsv1.StatefulSet) error {
		selector := map[string]string{
			kaitov1beta1.LabelWorkspaceName: ctx.Workspace.Name,
		}
		labelselector := &v1.LabelSelector{
			MatchLabels: selector,
		}

		ss.ObjectMeta = v1.ObjectMeta{
			Name:      ctx.Workspace.Name,
			Namespace: ctx.Workspace.Namespace,
			Annotations: map[string]string{
				kaitov1beta1.WorkspaceRevisionAnnotation: revisionNum,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(ctx.Workspace, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		}
		ss.Spec = appsv1.StatefulSetSpec{
			Replicas:            lo.ToPtr(int32(replicas)),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
			},
			Selector: labelselector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: selector,
				},
			},
		}

		ss.Spec.ServiceName = fmt.Sprintf("%s-headless", ctx.Workspace.Name)
		return nil
	}
}

func AddStatefulSetVolumeClaimTemplates(volumeClaimTemplates corev1.PersistentVolumeClaim) func(*generator.WorkspaceGeneratorContext, *appsv1.StatefulSet) error {
	return func(ctx *generator.WorkspaceGeneratorContext, ss *appsv1.StatefulSet) error {
		ss.Spec.VolumeClaimTemplates = append(ss.Spec.VolumeClaimTemplates, volumeClaimTemplates)
		return nil
	}
}

func SetStatefulSetPodSpec(podSpec *corev1.PodSpec) func(*generator.WorkspaceGeneratorContext, *appsv1.StatefulSet) error {
	return func(ctx *generator.WorkspaceGeneratorContext, ss *appsv1.StatefulSet) error {
		ss.Spec.Template.Spec = *podSpec
		return nil
	}
}

func GenerateTuningJobManifest(wObj *kaitov1beta1.Workspace, revisionNum string, imageName string,
	imagePullSecretRefs []corev1.LocalObjectReference, replicas int, commands []string, containerPorts []corev1.ContainerPort,
	livenessProbe, readinessProbe *corev1.Probe, resourceRequirements corev1.ResourceRequirements, tolerations []corev1.Toleration,
	initContainers []corev1.Container, sidecarContainers []corev1.Container, volumes []corev1.Volume, volumeMounts []corev1.VolumeMount,
	envVars []corev1.EnvVar) *batchv1.Job {
	labels := map[string]string{
		kaitov1beta1.LabelWorkspaceName: wObj.Name,
	}

	// TODO: make containers only mount the volumes they need

	for i := range initContainers {
		initContainers[i].VolumeMounts = utils.DedupVolumeMounts(append(initContainers[i].VolumeMounts, volumeMounts...))
	}

	for i := range sidecarContainers {
		sidecarContainers[i].VolumeMounts = utils.DedupVolumeMounts(append(sidecarContainers[i].VolumeMounts, volumeMounts...))
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

	shouldShareProcessNamespace := ptr.To(true)
	if len(sidecarContainers) == 0 {
		shouldShareProcessNamespace = ptr.To(false)
	}

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
				*v1.NewControllerRef(wObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
					ShareProcessNamespace: shouldShareProcessNamespace,
					Volumes:               volumes,
					Tolerations:           tolerations,
					ImagePullSecrets:      imagePullSecretRefs,
				},
			},
		},
	}
}

func GenerateDeploymentManifest(revisionNum string, replicas int) func(*generator.WorkspaceGeneratorContext, *appsv1.Deployment) error {
	return func(ctx *generator.WorkspaceGeneratorContext, d *appsv1.Deployment) error {
		selector := map[string]string{
			kaitov1beta1.LabelWorkspaceName: ctx.Workspace.Name,
		}
		labelselector := &v1.LabelSelector{
			MatchLabels: selector,
		}

		d.ObjectMeta = v1.ObjectMeta{
			Name:      ctx.Workspace.Name,
			Namespace: ctx.Workspace.Namespace,
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(ctx.Workspace, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
			Annotations: map[string]string{
				kaitov1beta1.WorkspaceRevisionAnnotation: revisionNum,
			},
		}
		d.Spec = appsv1.DeploymentSpec{
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
			},
		}
		return nil
	}
}

func SetDeploymentPodSpec(podSpec *corev1.PodSpec) func(*generator.WorkspaceGeneratorContext, *appsv1.Deployment) error {
	return func(ctx *generator.WorkspaceGeneratorContext, d *appsv1.Deployment) error {
		d.Spec.Template.Spec = *podSpec
		return nil
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

func GenerateDeploymentManifestWithPodTemplate(workspaceObj *kaitov1beta1.Workspace, tolerations []corev1.Toleration) *appsv1.Deployment {
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
				*v1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr(int32(*workspaceObj.Resource.Count)),
			Selector: labelselector,
			Template: *templateCopy,
		},
	}
}

func GetModelImageName(presetObj *pkgmodel.PresetParam) string {
	return utils.GetPresetImageName(presetObj.Name, presetObj.Tag)
}

// GenerateModelPullerContainer creates an init container that pulls model images using ORAS
func GenerateModelPullerContainer(ctx context.Context, workspaceObj *v1beta1.Workspace, presetObj *pkgmodel.PresetParam) []corev1.Container {
	if presetObj.DownloadAtRuntime {
		// If the preset is set to download at runtime, we don't need to pull the model weights.
		return nil
	}

	puller := corev1.Container{
		Name:  "model-weights-downloader",
		Image: utils.DefaultORASToolImage,
		Command: []string{
			"oras",
			"pull",
			GetModelImageName(presetObj),
			"-o",
			utils.DefaultWeightsVolumePath,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "model-weights-volume",
				MountPath: utils.DefaultWeightsVolumePath,
			},
		},
	}

	return []corev1.Container{puller}
}

// GenerateInferencePool generates an InferencePool manifest for the given workspace object.
// See https://gateway-api-inference-extension.sigs.k8s.io/reference/spec/ for more details.
func GenerateInferencePool(workspaceObj *v1beta1.Workspace) *gaiev1alpha2.InferencePool {
	return &gaiev1alpha2.InferencePool{
		ObjectMeta: v1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: gaiev1alpha2.InferencePoolSpec{
			TargetPortNumber: 5000,
			Selector: map[gaiev1alpha2.LabelKey]gaiev1alpha2.LabelValue{
				kaitov1beta1.LabelWorkspaceName: gaiev1alpha2.LabelValue(workspaceObj.Name),
				// TODO(chewong): account for leader/worker pods in multi-node deployments
			},
			EndpointPickerConfig: gaiev1alpha2.EndpointPickerConfig{
				ExtensionRef: &gaiev1alpha2.Extension{
					ExtensionReference: gaiev1alpha2.ExtensionReference{
						Name: gaiev1alpha2.ObjectName(fmt.Sprintf("%s-epp", workspaceObj.Name)),
					},
				},
			},
		},
	}
}

// GenerateInferenceModels generates one InferenceModel for the base model and one InferenceModel
// for each adapter in the workspace object. See https://gateway-api-inference-extension.sigs.k8s.io/reference/spec/
// for more details.
func GenerateInferenceModels(workspaceObj *v1beta1.Workspace) []*gaiev1alpha2.InferenceModel {
	presetName := string(workspaceObj.Inference.Preset.Name)
	models := []*gaiev1alpha2.InferenceModel{}
	models = append(models, &gaiev1alpha2.InferenceModel{
		ObjectMeta: v1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", workspaceObj.Name, presetName),
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: gaiev1alpha2.InferenceModelSpec{
			ModelName: presetName,
			PoolRef: gaiev1alpha2.PoolObjectReference{
				Name: gaiev1alpha2.ObjectName(workspaceObj.Name),
			},
		},
	})
	for _, adapter := range workspaceObj.Inference.Adapters {
		models = append(models, &gaiev1alpha2.InferenceModel{
			ObjectMeta: v1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", workspaceObj.Name, strings.ToLower(adapter.Source.Name)),
				Namespace: workspaceObj.Namespace,
				Labels: map[string]string{
					kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
				},
				OwnerReferences: []v1.OwnerReference{
					*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
				},
			},
			Spec: gaiev1alpha2.InferenceModelSpec{
				ModelName: adapter.Source.Name,
				PoolRef: gaiev1alpha2.PoolObjectReference{
					Name: gaiev1alpha2.ObjectName(workspaceObj.Name),
				},
			},
		})
	}

	return models
}

// GenerateEndpointPickerComponents generates the necessary components for the Endpoint Picker
// of a given Workspace object. See https://gateway-api-inference-extension.sigs.k8s.io/guides/implementers/#callout-extension
// for more details.
func GenerateEndpointPickerComponents(workspaceObj *v1beta1.Workspace) []client.Object {
	eppName := fmt.Sprintf("%s-epp", workspaceObj.Name)
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: v1.ObjectMeta{
			Name:      eppName,
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:  workspaceObj.Name,
				kaitov1beta1.LabelEndpointPicker: eppName,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
	}

	role := &rbacv1.Role{
		ObjectMeta: v1.ObjectMeta{
			Name:      eppName,
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:  workspaceObj.Name,
				kaitov1beta1.LabelEndpointPicker: eppName,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"inference.networking.x-k8s.io"},
				Resources: []string{"inferencemodels", "inferencepools"},
				Verbs:     []string{"get", "watch", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "watch", "list"},
			},
		},
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: v1.ObjectMeta{
			Name:      eppName,
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:  workspaceObj.Name,
				kaitov1beta1.LabelEndpointPicker: eppName,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     eppName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      fmt.Sprintf("%s-epp", workspaceObj.Name),
				Namespace: workspaceObj.Namespace,
			},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      eppName,
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:  workspaceObj.Name,
				kaitov1beta1.LabelEndpointPicker: eppName,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr(int32(1)),
			Selector: &v1.LabelSelector{
				MatchLabels: map[string]string{
					kaitov1beta1.LabelEndpointPicker: eppName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{
					Labels: map[string]string{
						kaitov1beta1.LabelEndpointPicker: eppName,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: fmt.Sprintf("%s-epp", workspaceObj.Name),
					Containers: []corev1.Container{
						{
							Name:            "epp",
							Image:           utils.DefaultGatewayAPIInferenceExtensionEPPImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args: []string{
								"-poolName", workspaceObj.Name,
								"-poolNamespace", workspaceObj.Namespace,
								"-v", "3",
								"-grpcPort", "9002",
								"-grpcHealthPort", "9003",
								"-metricsPort", "9090",
							},
							Env: []corev1.EnvVar{
								{
									// prefix caching requires scheduler v2
									Name:  "EXPERIMENTAL_USE_SCHEDULER_V2",
									Value: "true",
								},
								{
									// enables prefix cache scheduling
									Name:  "ENABLE_PREFIX_CACHE_SCHEDULING",
									Value: "true",
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 9002,
								},
								{
									Name:          "grpc-health",
									ContainerPort: 9003,
								},
								{
									Name:          "metrics",
									ContainerPort: 9090,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    9003,
										Service: lo.ToPtr("inference-extension"),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    9003,
										Service: lo.ToPtr("inference-extension"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      eppName,
			Namespace: workspaceObj.Namespace,
			Labels: map[string]string{
				kaitov1beta1.LabelWorkspaceName:  workspaceObj.Name,
				kaitov1beta1.LabelEndpointPicker: eppName,
			},
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(workspaceObj, v1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				kaitov1beta1.LabelEndpointPicker: eppName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       9002,
					TargetPort: intstr.FromInt(9002),
				},
				{
					Name:       "metrics",
					Port:       9090,
					TargetPort: intstr.FromInt(9090),
				},
			},
		},
	}

	return []client.Object{
		serviceAccount,
		role,
		roleBinding,
		deployment,
		service,
	}
}
