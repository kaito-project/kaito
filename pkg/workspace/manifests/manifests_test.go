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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	gaiev1alpha2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"

	"github.com/kaito-project/kaito/api/v1beta1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestGenerateStatefulSetManifest(t *testing.T) {

	t.Run("generate statefulset with headlessSvc", func(t *testing.T) {

		workspace := test.MockWorkspaceWithPreset

		obj := GenerateStatefulSetManifest(workspace, test.MockWorkspaceWithPresetHash,
			"",  //imageName
			nil, //imagePullSecretRefs
			*workspace.Resource.Count,
			nil, //commands
			nil, //containerPorts
			nil, //livenessProbe
			nil, //readinessProbe
			v1.ResourceRequirements{},
			nil, //tolerations
			nil, //volumes
			nil, //volumeMount
			nil, //envVars
			nil, //initContainers
			nil, //pvc
		)

		assert.Contains(t, obj.GetAnnotations(), v1beta1.WorkspaceRevisionAnnotation)
		assert.Equal(t, test.MockWorkspaceWithPresetHash, obj.GetAnnotations()[v1beta1.WorkspaceRevisionAnnotation])
		assert.Len(t, obj.OwnerReferences, 1, "Expected 1 OwnerReference")
		ownerRef := obj.OwnerReferences[0]
		assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
		assert.Equal(t, "Workspace", ownerRef.Kind)
		assert.Equal(t, workspace.Name, ownerRef.Name)
		assert.Equal(t, workspace.UID, ownerRef.UID)
		assert.True(t, *ownerRef.Controller)

		if obj.Spec.ServiceName != fmt.Sprintf("%s-headless", workspace.Name) {
			t.Errorf("headless service name is wrong in statefullset spec")
		}

		appSelector := map[string]string{
			v1beta1.LabelWorkspaceName: workspace.Name,
		}

		if !reflect.DeepEqual(appSelector, obj.Spec.Selector.MatchLabels) {
			t.Errorf("workload selector is wrong")
		}
		if !reflect.DeepEqual(appSelector, obj.Spec.Template.ObjectMeta.Labels) {
			t.Errorf("template label is wrong")
		}

		nodeReq := obj.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions

		for key, value := range workspace.Resource.LabelSelector.MatchLabels {
			if !kvInNodeRequirement(key, value, nodeReq) {
				t.Errorf("nodel affinity is wrong")
			}
		}
	})
}

func TestGenerateDeploymentManifest(t *testing.T) {
	t.Run("generate deployment", func(t *testing.T) {

		workspace := test.MockWorkspaceWithPreset

		obj := GenerateDeploymentManifest(workspace, test.MockWorkspaceWithPresetHash,
			"",  //imageName
			nil, //imagePullSecretRefs
			*workspace.Resource.Count,
			nil, //commands
			nil, //containerPorts
			nil, //livenessProbe
			nil, //readinessProbe
			v1.ResourceRequirements{},
			nil, //tolerations
			nil, //volumes
			nil, //volumeMount
			nil, //envVars
			nil, //initContainers
		)

		assert.Contains(t, obj.GetAnnotations(), v1beta1.WorkspaceRevisionAnnotation)
		assert.Equal(t, test.MockWorkspaceWithPresetHash, obj.GetAnnotations()[v1beta1.WorkspaceRevisionAnnotation])
		assert.Len(t, obj.OwnerReferences, 1, "Expected 1 OwnerReference")
		ownerRef := obj.OwnerReferences[0]
		assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
		assert.Equal(t, "Workspace", ownerRef.Kind)
		assert.Equal(t, workspace.Name, ownerRef.Name)
		assert.Equal(t, workspace.UID, ownerRef.UID)
		assert.True(t, *ownerRef.Controller)

		appSelector := map[string]string{
			v1beta1.LabelWorkspaceName: workspace.Name,
		}

		if !reflect.DeepEqual(appSelector, obj.Spec.Selector.MatchLabels) {
			t.Errorf("workload selector is wrong")
		}
		if !reflect.DeepEqual(appSelector, obj.Spec.Template.ObjectMeta.Labels) {
			t.Errorf("template label is wrong")
		}

		nodeReq := obj.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions

		for key, value := range workspace.Resource.LabelSelector.MatchLabels {
			if !kvInNodeRequirement(key, value, nodeReq) {
				t.Errorf("nodel affinity is wrong")
			}
		}
	})
}

func TestGenerateDeploymentManifestWithPodTemplate(t *testing.T) {
	t.Run("generate deployment with pod template", func(t *testing.T) {

		workspace := test.MockWorkspaceWithInferenceTemplate

		obj := GenerateDeploymentManifestWithPodTemplate(workspace, nil)

		assert.Len(t, obj.OwnerReferences, 1, "Expected 1 OwnerReference")
		ownerRef := obj.OwnerReferences[0]
		assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
		assert.Equal(t, "Workspace", ownerRef.Kind)
		assert.Equal(t, workspace.Name, ownerRef.Name)
		assert.Equal(t, workspace.UID, ownerRef.UID)
		assert.True(t, *ownerRef.Controller)

		appSelector := map[string]string{
			v1beta1.LabelWorkspaceName: workspace.Name,
		}

		if !reflect.DeepEqual(appSelector, obj.Spec.Selector.MatchLabels) {
			t.Errorf("workload selector is wrong")
		}
		if !reflect.DeepEqual(appSelector, obj.Spec.Template.ObjectMeta.Labels) {
			t.Errorf("template label is wrong")
		}

		nodeReq := obj.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions

		for key, value := range workspace.Resource.LabelSelector.MatchLabels {
			if !kvInNodeRequirement(key, value, nodeReq) {
				t.Errorf("nodel affinity is wrong")
			}
		}
	})
}

func TestGenerateServiceManifest(t *testing.T) {
	options := []bool{true, false}

	for _, isStatefulSet := range options {
		t.Run(fmt.Sprintf("generate service, isStatefulSet %v", isStatefulSet), func(t *testing.T) {
			workspace := test.MockWorkspaceWithPreset
			obj := GenerateServiceManifest(workspace, v1.ServiceTypeClusterIP, isStatefulSet)

			assert.Len(t, obj.OwnerReferences, 1, "Expected 1 OwnerReference")
			ownerRef := obj.OwnerReferences[0]
			assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
			assert.Equal(t, "Workspace", ownerRef.Kind)
			assert.Equal(t, workspace.Name, ownerRef.Name)
			assert.Equal(t, workspace.UID, ownerRef.UID)
			assert.True(t, *ownerRef.Controller)

			svcSelector := map[string]string{
				v1beta1.LabelWorkspaceName: workspace.Name,
			}
			if isStatefulSet {
				svcSelector["statefulset.kubernetes.io/pod-name"] = fmt.Sprintf("%s-0", workspace.Name)
			}
			if !reflect.DeepEqual(svcSelector, obj.Spec.Selector) {
				t.Errorf("svc selector is wrong")
			}
		})
	}
}

func TestGenerateHeadlessServiceManifest(t *testing.T) {

	t.Run("generate headless service", func(t *testing.T) {
		workspace := test.MockWorkspaceWithPreset
		obj := GenerateHeadlessServiceManifest(workspace)

		assert.Len(t, obj.OwnerReferences, 1, "Expected 1 OwnerReference")
		ownerRef := obj.OwnerReferences[0]
		assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
		assert.Equal(t, "Workspace", ownerRef.Kind)
		assert.Equal(t, workspace.Name, ownerRef.Name)
		assert.Equal(t, workspace.UID, ownerRef.UID)
		assert.True(t, *ownerRef.Controller)

		svcSelector := map[string]string{
			v1beta1.LabelWorkspaceName: workspace.Name,
		}
		if !reflect.DeepEqual(svcSelector, obj.Spec.Selector) {
			t.Errorf("svc selector is wrong")
		}
		if obj.Spec.ClusterIP != "None" {
			t.Errorf("svc ClusterIP is wrong")
		}
		if obj.Name != fmt.Sprintf("%s-headless", workspace.Name) {
			t.Errorf("svc Name is wrong")
		}
	})
}

func TestGenerateInferencePool(t *testing.T) {
	tests := []struct {
		name                      string
		isStatefulSet             bool
		expectedInferencePoolSpec gaiev1alpha2.InferencePoolSpec
	}{
		{
			name:          "statefulset inference pool",
			isStatefulSet: true,
			expectedInferencePoolSpec: gaiev1alpha2.InferencePoolSpec{
				TargetPortNumber: consts.PortInferenceServer,
				Selector: map[gaiev1alpha2.LabelKey]gaiev1alpha2.LabelValue{
					kaitov1beta1.LabelWorkspaceName: gaiev1alpha2.LabelValue(test.MockWorkspaceWithPreset.Name),
					appsv1.PodIndexLabel:            gaiev1alpha2.LabelValue("0"), // Pod index label for statefulset
				},
				EndpointPickerConfig: gaiev1alpha2.EndpointPickerConfig{
					ExtensionRef: &gaiev1alpha2.Extension{
						ExtensionReference: gaiev1alpha2.ExtensionReference{
							Name: gaiev1alpha2.ObjectName(fmt.Sprintf("%s-epp", test.MockWorkspaceWithPreset.Name)),
						},
					},
				},
			},
		},
		{
			name:          "deployment inference pool",
			isStatefulSet: false,
			expectedInferencePoolSpec: gaiev1alpha2.InferencePoolSpec{
				TargetPortNumber: consts.PortInferenceServer,
				Selector: map[gaiev1alpha2.LabelKey]gaiev1alpha2.LabelValue{
					kaitov1beta1.LabelWorkspaceName: gaiev1alpha2.LabelValue(test.MockWorkspaceWithPreset.Name),
				},
				EndpointPickerConfig: gaiev1alpha2.EndpointPickerConfig{
					ExtensionRef: &gaiev1alpha2.Extension{
						ExtensionReference: gaiev1alpha2.ExtensionReference{
							Name: gaiev1alpha2.ObjectName(fmt.Sprintf("%s-epp", test.MockWorkspaceWithPreset.Name)),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inferencePool := GenerateInferencePool(test.MockWorkspaceWithPreset, tt.isStatefulSet)

			assert.Len(t, inferencePool.OwnerReferences, 1, "Expected 1 OwnerReference")
			ownerRef := inferencePool.OwnerReferences[0]
			assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
			assert.Equal(t, "Workspace", ownerRef.Kind)
			assert.Equal(t, test.MockWorkspaceWithPreset.Name, ownerRef.Name)
			assert.Equal(t, test.MockWorkspaceWithPreset.UID, ownerRef.UID)
			assert.True(t, *ownerRef.Controller)

			assert.Equal(t, tt.expectedInferencePoolSpec, inferencePool.Spec)
		})
	}
}

func TestGenerateInferenceModels(t *testing.T) {
	workspace := &test.MockWorkspaceWithUpdatedDeployment
	inferenceModels := GenerateInferenceModels(workspace)
	expectedSpec := map[string]gaiev1alpha2.InferenceModelSpec{
		"testWorkspace-test-model": {
			ModelName: "test-model",
			PoolRef: gaiev1alpha2.PoolObjectReference{
				Name: gaiev1alpha2.ObjectName(workspace.Name),
			},
		},
		"testWorkspace-adapter-1": {
			ModelName: "Adapter-1",
			PoolRef: gaiev1alpha2.PoolObjectReference{
				Name: gaiev1alpha2.ObjectName(workspace.Name),
			},
		},
	}

	assert.Len(t, inferenceModels, 2)
	for _, model := range inferenceModels {
		assert.Len(t, model.OwnerReferences, 1, "Expected 1 OwnerReference")
		ownerRef := model.OwnerReferences[0]
		assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
		assert.Equal(t, "Workspace", ownerRef.Kind)
		assert.Equal(t, workspace.Name, ownerRef.Name)
		assert.Equal(t, workspace.UID, ownerRef.UID)
		assert.True(t, *ownerRef.Controller)
		assert.Equal(t, model.Spec, expectedSpec[model.Name])
	}
}

func TestGenerateEndpointPickerComponents(t *testing.T) {
	workspace := test.MockWorkspaceWithPreset
	eppName := fmt.Sprintf("%s-epp", workspace.Name)
	components := GenerateEndpointPickerComponents(workspace)
	assert.Len(t, components, 5, "Expected 5 components for Endpoint Picker - 1 Deployment, 1 Service, 1 ServiceAccount, 1 Role, 1 RoleBinding")
	for _, component := range components {
		assert.Equal(t, component.GetName(), eppName)
		assert.Equal(t, component.GetLabels(), map[string]string{
			kaitov1beta1.LabelWorkspaceName:  workspace.Name,
			kaitov1beta1.LabelEndpointPicker: eppName,
		})
		assert.Len(t, component.GetOwnerReferences(), 1, "Expected 1 OwnerReference")
		ownerRef := component.GetOwnerReferences()[0]
		assert.Equal(t, v1beta1.GroupVersion.String(), ownerRef.APIVersion)
		assert.Equal(t, "Workspace", ownerRef.Kind)
		assert.Equal(t, workspace.Name, ownerRef.Name)
		assert.Equal(t, workspace.UID, ownerRef.UID)
		assert.True(t, *ownerRef.Controller)

		// component-specific assertions
		switch obj := component.(type) {
		case *appsv1.Deployment:
			assert.Equal(t, obj.Spec.Template.Spec.ServiceAccountName, eppName)
			assert.Equal(t, obj.Spec.Selector.MatchLabels, map[string]string{
				kaitov1beta1.LabelEndpointPicker: eppName,
			})
			assert.Equal(t, obj.Spec.Template.Labels, map[string]string{
				kaitov1beta1.LabelEndpointPicker: eppName,
			})
			assert.Equal(t, obj.Spec.Template.Spec.Containers[0].Image, utils.DefaultGatewayAPIInferenceExtensionEPPImage)
			assert.Equal(t, obj.Spec.Template.Spec.Containers[0].Args, []string{
				"-poolName", workspace.Name,
				"-poolNamespace", workspace.Namespace,
				"-v", "3",
				"-grpcPort", "9002",
				"-grpcHealthPort", "9003",
				"-metricsPort", "9090",
			})
			assert.Equal(t, obj.Spec.Template.Spec.Containers[0].Env, []v1.EnvVar{
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
			})
		case *rbacv1.Role:
			assert.Equal(t, obj.Rules, []rbacv1.PolicyRule{
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
			})
		case *rbacv1.RoleBinding:
			assert.Equal(t, obj.RoleRef.APIGroup, rbacv1.SchemeGroupVersion.Group)
			assert.Equal(t, obj.RoleRef.Name, eppName)
			assert.Equal(t, obj.RoleRef.Kind, "Role")
		case *v1.Service:
			assert.Equal(t, obj.Spec.Type, v1.ServiceTypeClusterIP)
			assert.Equal(t, obj.Spec.Selector, map[string]string{
				kaitov1beta1.LabelEndpointPicker: eppName,
			})
			assert.Equal(t, obj.Spec.Ports, []corev1.ServicePort{
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
			})
		case *v1.ServiceAccount: // no specific assertions needed
		default:
			t.Errorf("Unexpected component type: %T", component)
		}
	}
}

func kvInNodeRequirement(key, val string, nodeReq []v1.NodeSelectorRequirement) bool {
	for _, each := range nodeReq {
		if each.Key == key && each.Values[0] == val && each.Operator == v1.NodeSelectorOpIn {
			return true
		}
	}
	return false
}
