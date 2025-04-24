// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package manifests

import (
	"context"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"
)

func GenerateRAGLWSManifest(ctx context.Context, ragEngineObj *kaitov1alpha1.RAGEngine, revisionNum string, imageName string,
	imagePullSecretRefs []corev1.LocalObjectReference, replicas int, commands []string, containerPorts []corev1.ContainerPort,
	livenessProbe, readinessProbe *corev1.Probe, resourceRequirements corev1.ResourceRequirements,
	tolerations []corev1.Toleration, volumes []corev1.Volume, volumeMount []corev1.VolumeMount) *lwsv1.LeaderWorkerSet {

	nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(ragEngineObj.Spec.Compute.LabelSelector.MatchLabels))
	for key, value := range ragEngineObj.Spec.Compute.LabelSelector.MatchLabels {
		nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
			Key:      key,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{value},
		})
	}

	initContainers := []corev1.Container{}

	envs := RAGSetEnv(ragEngineObj)

	return &lwsv1.LeaderWorkerSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      ragEngineObj.Name,
			Namespace: ragEngineObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				{
					APIVersion: kaitov1alpha1.GroupVersion.String(),
					Kind:       "RAGEngine",
					UID:        ragEngineObj.UID,
					Name:       ragEngineObj.Name,
					Controller: &controller,
				},
			},
			Annotations: map[string]string{
				kaitov1alpha1.RAGEngineRevisionAnnotation: revisionNum,
			},
		},
		Spec: lwsv1.LeaderWorkerSetSpec{
			Replicas: lo.ToPtr(int32(replicas)),
			RolloutStrategy: lwsv1.RolloutStrategy{
				Type: lwsv1.RollingUpdateStrategyType,
				RollingUpdateConfiguration: &lwsv1.RollingUpdateConfiguration{
					MaxSurge: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxUnavailable: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				}, // Configuration for rolling updates: allows no extra pods during the update and permits at most one unavailable pod at a time。
			},
			StartupPolicy: lwsv1.LeaderCreatedStartupPolicy,
			LeaderWorkerTemplate: lwsv1.LeaderWorkerTemplate{
				WorkerTemplate: corev1.PodTemplateSpec{
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
						InitContainers: initContainers,
						Containers: []corev1.Container{
							{
								Name:           ragEngineObj.Name,
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
		},
	}
}
