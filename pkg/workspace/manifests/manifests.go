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
	"encoding/json"
	"fmt"
	"path"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/generator"
	"github.com/kaito-project/kaito/pkg/workspace/image"
)

func GenerateHeadlessServiceManifest(workspaceObj *kaitov1beta1.Workspace) *corev1.Service {
	serviceName := fmt.Sprintf("%s-headless", workspaceObj.Name)
	selector := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
					TargetPort: intstr.FromInt32(consts.PortInferenceServer),
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
		labelselector := &metav1.LabelSelector{
			MatchLabels: selector,
		}

		ss.ObjectMeta = metav1.ObjectMeta{
			Name:      ctx.Workspace.Name,
			Namespace: ctx.Workspace.Namespace,
			Annotations: map[string]string{
				kaitov1beta1.WorkspaceRevisionAnnotation: revisionNum,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ctx.Workspace, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
				ObjectMeta: metav1.ObjectMeta{
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

func GenerateTuningJobManifest(revisionNum string) func(*generator.WorkspaceGeneratorContext, *batchv1.Job) error {
	return func(ctx *generator.WorkspaceGeneratorContext, j *batchv1.Job) error {
		labels := map[string]string{
			kaitov1beta1.LabelWorkspaceName: ctx.Workspace.Name,
		}

		j.ObjectMeta = metav1.ObjectMeta{
			Name:      ctx.Workspace.Name,
			Namespace: ctx.Workspace.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				kaitov1beta1.WorkspaceRevisionAnnotation: revisionNum,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ctx.Workspace, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		}

		j.Spec = batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
			},
		}
		return nil
	}
}

func SetJobPodSpec(podSpec *corev1.PodSpec) func(*generator.WorkspaceGeneratorContext, *batchv1.Job) error {
	return func(ctx *generator.WorkspaceGeneratorContext, j *batchv1.Job) error {
		if len(podSpec.Containers) > 1 {
			podSpec.ShareProcessNamespace = ptr.To(true)
		}
		j.Spec.Template.Spec = *podSpec
		return nil
	}
}

func GenerateDeploymentManifest(revisionNum string, replicas int) func(*generator.WorkspaceGeneratorContext, *appsv1.Deployment) error {
	return func(ctx *generator.WorkspaceGeneratorContext, d *appsv1.Deployment) error {
		selector := map[string]string{
			kaitov1beta1.LabelWorkspaceName: ctx.Workspace.Name,
		}
		labelselector := &metav1.LabelSelector{
			MatchLabels: selector,
		}

		d.ObjectMeta = metav1.ObjectMeta{
			Name:      ctx.Workspace.Name,
			Namespace: ctx.Workspace.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ctx.Workspace, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
				ObjectMeta: metav1.ObjectMeta{
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

		var volumeMount corev1.VolumeMount
		var volume corev1.Volume
		if len(source.ImagePullSecrets) > 0 {
			volume, volumeMount = utils.ConfigImagePullSecretVolume(sourceName+"-inference-adapter", source.ImagePullSecrets)
			volumes = append(volumes, volume)
		}

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
	labelselector := &metav1.LabelSelector{
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      workspaceObj.Name,
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
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
func GenerateModelPullerContainer(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, presetObj *pkgmodel.PresetParam) []corev1.Container {
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

// GenerateInferencePoolOCIRepository generates a Flux OCIRepository for the inference pool.
func GenerateInferencePoolOCIRepository(workspaceObj *kaitov1beta1.Workspace) *sourcev1.OCIRepository {
	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.InferencePoolName(workspaceObj.Name),
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: sourcev1.OCIRepositorySpec{
			// Chart source for Gateway API Inference Extension inference pool;
			// keep in sync with consts.InferencePoolChartVersion when upgrading.
			URL: consts.InferencePoolChartURL,
			Reference: &sourcev1.OCIRepositoryRef{
				Tag: consts.InferencePoolChartVersion,
			},
		},
	}
}

// GenerateInferencePoolHelmRelease generates a Flux HelmRelease for the inference pool.
func GenerateInferencePoolHelmRelease(workspaceObj *kaitov1beta1.Workspace, isStatefulSet bool) (*helmv2.HelmRelease, error) {
	matchLabels := map[string]string{
		kaitov1beta1.LabelWorkspaceName: workspaceObj.Name,
	}
	if isStatefulSet {
		// Endpoint Picker from Gateway API Inference Extension expects to pick an endpoint that can serve traffic.
		// In a multi-node inference environment, this means we need to select the leader pod (with pod index 0)
		// since only the leader pod is capable of serving traffic.
		matchLabels[appsv1.PodIndexLabel] = "0"
	}

	// Based on https://github.com/kubernetes-sigs/gateway-api-inference-extension/blob/v0.5.1/config/charts/inferencepool/values.yaml
	helmValues := map[string]any{
		"inferenceExtension": map[string]any{
			"image": map[string]string{
				"hub":        consts.GatewayAPIInferenceExtensionImageRepository,
				"tag":        consts.InferencePoolChartVersion,
				"pullPolicy": string(corev1.PullIfNotPresent),
			},
			"pluginsConfigFile": "plugins-v2.yaml",
		},
		"inferencePool": map[string]any{
			"targetPortNumber": consts.PortInferenceServer,
			"modelServers": map[string]any{
				"matchLabels": matchLabels,
			},
		},
	}
	rawHelmValues, err := json.Marshal(helmValues)
	if err != nil {
		return nil, err
	}

	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.InferencePoolName(workspaceObj.Name),
			Namespace: workspaceObj.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(workspaceObj, kaitov1beta1.GroupVersion.WithKind("Workspace")),
			},
		},
		Spec: helmv2.HelmReleaseSpec{
			// Referencing the OCIRepository created above
			ChartRef: &helmv2.CrossNamespaceSourceReference{
				Kind:      sourcev1.OCIRepositoryKind,
				Namespace: workspaceObj.Namespace,
				Name:      utils.InferencePoolName(workspaceObj.Name),
			},
			Values: &apiextensionsv1.JSON{
				Raw: rawHelmValues,
			},
		},
	}, nil
}
