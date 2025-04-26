// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package manifests

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func kvInNodeRequirement(key, val string, nodeReq []v1.NodeSelectorRequirement) bool {
	for _, each := range nodeReq {
		if each.Key == key && each.Values[0] == val && each.Operator == v1.NodeSelectorOpIn {
			return true
		}
	}
	return false
}

func TestGenerateRAGDeploymentManifest(t *testing.T) {
	t.Run("generate RAG deployment", func(t *testing.T) {

		// Mocking the RAGEngine object for the test
		ragEngine := test.MockRAGEngineWithPreset

		// Calling the function to generate the deployment manifest
		obj := GenerateRAGDeploymentManifest(context.TODO(), ragEngine, test.MockRAGEngineWithPresetHash,
			"",                            // imageName
			nil,                           // imagePullSecretRefs
			*ragEngine.Spec.Compute.Count, // replicas
			nil,                           // commands
			nil,                           // containerPorts
			nil,                           // livenessProbe
			nil,                           // readinessProbe
			v1.ResourceRequirements{},
			nil, // tolerations
			nil, // volumes
			nil, // volumeMount
		)

		// Expected label selector for the deployment
		appSelector := map[string]string{
			kaitov1alpha1.LabelRAGEngineName: ragEngine.Name,
		}

		// Check if the deployment's selector is correct
		if !reflect.DeepEqual(appSelector, obj.Spec.Selector.MatchLabels) {
			t.Errorf("RAGEngine workload selector is wrong")
		}

		// Check if the template labels match the expected labels
		if !reflect.DeepEqual(appSelector, obj.Spec.Template.ObjectMeta.Labels) {
			t.Errorf("RAGEngine template label is wrong")
		}

		// Extract node selector requirements from the deployment manifest
		nodeReq := obj.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions

		// Validate if the node requirements match the RAGEngine's label selector
		for key, value := range ragEngine.Spec.Compute.LabelSelector.MatchLabels {
			if !kvInNodeRequirement(key, value, nodeReq) {
				t.Errorf("Node affinity requirements are wrong for key %s and value %s", key, value)
			}
		}
	})
}
