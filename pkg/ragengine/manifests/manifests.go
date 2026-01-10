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
	"fmt"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func GenerateRAGDeploymentManifest(ragEngineObj *kaitov1beta1.RAGEngine, revisionNum string, imageName string,
	imagePullSecretRefs []corev1.LocalObjectReference, commands []string, containerPorts []corev1.ContainerPort,
	livenessProbe, readinessProbe *corev1.Probe, resourceRequirements corev1.ResourceRequirements,
	tolerations []corev1.Toleration, volumes []corev1.Volume, volumeMount []corev1.VolumeMount) *appsv1.Deployment {

	nodeRequirements := make([]corev1.NodeSelectorRequirement, 0, len(ragEngineObj.Spec.Compute.LabelSelector.MatchLabels))
	for key, value := range ragEngineObj.Spec.Compute.LabelSelector.MatchLabels {
		nodeRequirements = append(nodeRequirements, corev1.NodeSelectorRequirement{
			Key:      key,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{value},
		})
	}

	selector := map[string]string{
		kaitov1beta1.LabelRAGEngineName: ragEngineObj.Name,
	}
	labelselector := &v1.LabelSelector{
		MatchLabels: selector,
	}
	initContainers := []corev1.Container{}

	envs := RAGSetEnv(ragEngineObj)

	return &appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      ragEngineObj.Name,
			Namespace: ragEngineObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(ragEngineObj, kaitov1beta1.GroupVersion.WithKind("RAGEngine")),
			},
			Annotations: map[string]string{
				kaitov1beta1.RAGEngineRevisionAnnotation: revisionNum,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: lo.ToPtr(int32(1)), // RAGEngine requires exactly 1 replica
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
					TerminationGracePeriodSeconds: lo.ToPtr(int64(60)),
					ImagePullSecrets:              imagePullSecretRefs,
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
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"python3",
											"/app/ragengine/lifecycle/hooks.py",
											"poststart",
										},
									},
								},
								PreStop: &corev1.LifecycleHandler{
									Exec: &corev1.ExecAction{
										Command: []string{
											"/bin/sh",
											"-c",
											"python3 /app/ragengine/lifecycle/hooks.py prestop && sleep 5",
										},
									},
								},
							},
						},
					},
					Tolerations: tolerations,
					Volumes:     volumes,
				},
			},
		},
	}
}

func RAGSetEnv(ragEngineObj *kaitov1beta1.RAGEngine) []corev1.EnvVar {
	var envs []corev1.EnvVar

	// Add Pod metadata as environment variables for lifecycle hooks
	envs = append(envs, corev1.EnvVar{
		Name: "POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			},
		},
	})
	envs = append(envs, corev1.EnvVar{
		Name: "POD_UID",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.uid",
			},
		},
	})

	var embeddingType string
	if ragEngineObj.Spec.Embedding.Local != nil {
		embeddingType = "local"
		if ragEngineObj.Spec.Embedding.Local.ModelID != "" {
			modelID := ragEngineObj.Spec.Embedding.Local.ModelID
			modelIDEnv := corev1.EnvVar{
				Name:  "MODEL_ID",
				Value: modelID,
			}
			envs = append(envs, modelIDEnv)
		}
		if ragEngineObj.Spec.Embedding.Local.ModelAccessSecret != "" {
			accessSecret := ragEngineObj.Spec.Embedding.Local.ModelAccessSecret
			accessSecretEnv := corev1.EnvVar{
				Name:  "ACCESS_SECRET",
				Value: accessSecret,
			}
			envs = append(envs, accessSecretEnv)
		}
	} else if ragEngineObj.Spec.Embedding.Remote != nil {
		embeddingType = "remote"
		// TODO: Model ID Env
	}
	embeddingTypeEnv := corev1.EnvVar{
		Name:  "EMBEDDING_TYPE",
		Value: embeddingType,
	}
	envs = append(envs, embeddingTypeEnv)

	stoageEnv := corev1.EnvVar{
		Name:  "VECTOR_DB_TYPE",
		Value: "faiss", // TODO: get storage done
	}
	envs = append(envs, stoageEnv)

	// Set the vector database persist directory based on storage configuration
	persistDir := "storage" // default in-memory/ephemeral storage
	if ragEngineObj.Spec.Storage != nil && ragEngineObj.Spec.Storage.PersistentVolume != nil {
		mountPath := "/mnt/data"
		if ragEngineObj.Spec.Storage.PersistentVolume.MountPath != "" {
			mountPath = ragEngineObj.Spec.Storage.PersistentVolume.MountPath
		}
		// Append RAGEngine name to ensure unique directory per instance
		persistDir = fmt.Sprintf("%s/%s", mountPath, ragEngineObj.Name)
	}
	persistDirEnv := corev1.EnvVar{
		Name:  "DEFAULT_VECTOR_DB_PERSIST_DIR",
		Value: persistDir,
	}
	envs = append(envs, persistDirEnv)
	inferenceServiceURL := ragEngineObj.Spec.InferenceService.URL
	inferenceServiceURLEnv := corev1.EnvVar{
		Name:  "LLM_INFERENCE_URL",
		Value: inferenceServiceURL,
	}
	envs = append(envs, inferenceServiceURLEnv)

	if ragEngineObj.Spec.InferenceService.AccessSecret != "" {
		accessSecretEnv := corev1.EnvVar{
			Name: "LLM_ACCESS_SECRET",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ragEngineObj.Spec.InferenceService.AccessSecret,
					},
					Key: "LLM_ACCESS_SECRET",
				},
			},
		}
		envs = append(envs, accessSecretEnv)
	}

	contextWindowEnv := corev1.EnvVar{
		Name:  "LLM_CONTEXT_WINDOW",
		Value: fmt.Sprintf("%d", ragEngineObj.Spec.InferenceService.ContextWindowSize),
	}
	envs = append(envs, contextWindowEnv)

	return envs
}

func GenerateRAGServiceManifest(ragObj *kaitov1beta1.RAGEngine, serviceName string, serviceType corev1.ServiceType) *corev1.Service {
	selector := map[string]string{
		kaitov1beta1.LabelRAGEngineName: ragObj.Name,
	}

	servicePorts := []corev1.ServicePort{
		{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt32(5000),
		},
	}

	return &corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      serviceName,
			Namespace: ragObj.Namespace,
			OwnerReferences: []v1.OwnerReference{
				*v1.NewControllerRef(ragObj, kaitov1beta1.GroupVersion.WithKind("RAGEngine")),
			},
		},
		Spec: corev1.ServiceSpec{
			Type:                     serviceType,
			Ports:                    servicePorts,
			Selector:                 selector,
			PublishNotReadyAddresses: true,
		},
	}
}
