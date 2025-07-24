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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

	// Setup the mock kubernetes client
	mockClient := test.NewClient()
	mockClient.On("Get", mock.IsType(context.Background()), mock.Anything, mock.IsType(&corev1.ConfigMap{}), mock.Anything).Return(nil)

	components, err := GenerateEndpointPickerComponents(context.Background(), mockClient, workspace)
	assert.NoError(t, err, "Expected no error generating Endpoint Picker components")
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
				"--v", "3",
				"-grpcPort", "9002",
				"-grpcHealthPort", "9003",
				"-metricsPort", "9090",
				"-configFile", "/mnt/config/epp-config.yaml",
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
