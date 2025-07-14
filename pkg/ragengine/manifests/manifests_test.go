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
		obj := GenerateRAGDeploymentManifest(ragEngine, test.MockRAGEngineWithPresetHash,
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

		// Verify owner references
		if len(obj.OwnerReferences) != 1 {
			t.Errorf("Expected 1 owner reference, got %d", len(obj.OwnerReferences))
		}
		ownerRef := obj.OwnerReferences[0]
		if ownerRef.APIVersion != kaitov1alpha1.GroupVersion.String() {
			t.Errorf("Expected owner reference APIVersion %s, got %s", kaitov1alpha1.GroupVersion.String(), ownerRef.APIVersion)
		}
		if ownerRef.Kind != "RAGEngine" {
			t.Errorf("Expected owner reference Kind %s, got %s", "RAGEngine", ownerRef.Kind)
		}
		if ownerRef.Name != ragEngine.Name {
			t.Errorf("Expected owner reference Name %s, got %s", ragEngine.Name, ownerRef.Name)
		}
		if string(ownerRef.UID) != string(ragEngine.UID) {
			t.Errorf("Expected owner reference UID %s, got %s", string(ragEngine.UID), string(ownerRef.UID))
		}
		if ownerRef.Controller == nil || !*ownerRef.Controller {
			t.Error("Expected owner reference Controller to be true")
		}
	})
}

func TestGenerateRAGServiceManifest(t *testing.T) {
	t.Run("generate RAG service", func(t *testing.T) {
		// Mocking the RAGEngine object for the test
		ragEngine := test.MockRAGEngineWithPreset
		serviceName := "test-rag-service"
		serviceType := v1.ServiceTypeClusterIP

		// Generate the service manifest
		service := GenerateRAGServiceManifest(ragEngine, serviceName, serviceType)

		// Verify service name
		if service.Name != serviceName {
			t.Errorf("Expected service name %s, got %s", serviceName, service.Name)
		}

		// Verify namespace
		if service.Namespace != ragEngine.Namespace {
			t.Errorf("Expected namespace %s, got %s", ragEngine.Namespace, service.Namespace)
		}

		// Verify service type
		if service.Spec.Type != serviceType {
			t.Errorf("Expected service type %s, got %s", serviceType, service.Spec.Type)
		}

		// Verify selector
		expectedSelector := map[string]string{
			kaitov1alpha1.LabelRAGEngineName: ragEngine.Name,
		}
		if !reflect.DeepEqual(service.Spec.Selector, expectedSelector) {
			t.Errorf("Expected selector %v, got %v", expectedSelector, service.Spec.Selector)
		}

		// Verify ports
		if len(service.Spec.Ports) != 1 {
			t.Errorf("Expected 1 port, got %d", len(service.Spec.Ports))
		}

		port := service.Spec.Ports[0]
		if port.Name != "http" || port.Port != 80 || port.TargetPort.IntVal != 5000 {
			t.Errorf("Port configuration is incorrect")
		}

		// Enhanced owner reference verification
		if len(service.OwnerReferences) != 1 {
			t.Errorf("Expected 1 owner reference, got %d", len(service.OwnerReferences))
		}
		ownerRef := service.OwnerReferences[0]
		if ownerRef.APIVersion != kaitov1alpha1.GroupVersion.String() {
			t.Errorf("Expected owner reference APIVersion %s, got %s", kaitov1alpha1.GroupVersion.String(), ownerRef.APIVersion)
		}
		if ownerRef.Kind != "RAGEngine" {
			t.Errorf("Expected owner reference Kind %s, got %s", "RAGEngine", ownerRef.Kind)
		}
		if ownerRef.Name != ragEngine.Name {
			t.Errorf("Expected owner reference Name %s, got %s", ragEngine.Name, ownerRef.Name)
		}
		if string(ownerRef.UID) != string(ragEngine.UID) {
			t.Errorf("Expected owner reference UID %s, got %s", string(ragEngine.UID), string(ownerRef.UID))
		}
		if ownerRef.Controller == nil || !*ownerRef.Controller {
			t.Error("Expected owner reference Controller to be true")
		}
	})
}
