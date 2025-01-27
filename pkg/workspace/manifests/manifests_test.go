// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package manifests

import (
	"context"
	"fmt"
	"reflect"

	"github.com/kaito-project/kaito/pkg/utils/test"

	"testing"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

func TestGenerateStatefulSetManifest(t *testing.T) {

	t.Run("generate statefulset with headlessSvc", func(t *testing.T) {

		workspace := test.MockWorkspaceWithPreset

		obj := GenerateStatefulSetManifest(context.TODO(), workspace,
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
		)

		if obj.Spec.ServiceName != fmt.Sprintf("%s-headless", workspace.Name) {
			t.Errorf("headless service name is wrong in statefullset spec")
		}

		appSelector := map[string]string{
			kaitov1alpha1.LabelWorkspaceName: workspace.Name,
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

		obj := GenerateDeploymentManifest(context.TODO(), workspace, test.MockWorkspaceWithPresetHash,
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
		)

		appSelector := map[string]string{
			kaitov1alpha1.LabelWorkspaceName: workspace.Name,
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

		obj := GenerateDeploymentManifestWithPodTemplate(context.TODO(), workspace, nil)

		appSelector := map[string]string{
			kaitov1alpha1.LabelWorkspaceName: workspace.Name,
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
			obj := GenerateServiceManifest(context.TODO(), workspace, v1.ServiceTypeClusterIP, isStatefulSet)

			svcSelector := map[string]string{
				kaitov1alpha1.LabelWorkspaceName: workspace.Name,
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
		obj := GenerateHeadlessServiceManifest(context.TODO(), workspace)

		svcSelector := map[string]string{
			kaitov1alpha1.LabelWorkspaceName: workspace.Name,
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
