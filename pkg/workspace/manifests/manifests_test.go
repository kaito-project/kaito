// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package manifests

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
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

func kvInNodeRequirement(key, val string, nodeReq []v1.NodeSelectorRequirement) bool {
	for _, each := range nodeReq {
		if each.Key == key && each.Values[0] == val && each.Operator == v1.NodeSelectorOpIn {
			return true
		}
	}
	return false
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
