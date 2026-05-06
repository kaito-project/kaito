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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/test"
)

func TestGenerateRAGDeploymentManifestDifferentConfigurations(t *testing.T) {
	testcases := map[string]struct {
		ragEngine    *kaitov1beta1.RAGEngine
		expectedEnvs map[string]string
	}{
		"test-rag-with-no-compute-resource-and-inference-service": {
			ragEngine: test.MockRAGEngineWithNoComputeResourceAndInferenceService,
			expectedEnvs: map[string]string{
				"VECTOR_DB_TYPE": "faiss",
				"EMBEDDING_TYPE": "local",
				"MODEL_ID":       "BAAI/bge-small-en-v1.5",
			},
		},
		"test-rag-with-no-compute-resource": {
			ragEngine: test.MockRAGEngineWithNoComputeResource,
			expectedEnvs: map[string]string{
				"VECTOR_DB_TYPE":     "faiss",
				"EMBEDDING_TYPE":     "local",
				"LLM_CONTEXT_WINDOW": "512",
				"MODEL_ID":           "BAAI/bge-small-en-v1.5",
				"LLM_INFERENCE_URL":  "http://localhost:5000/chat",
			},
		},
		"test-rag-with-no-inference-service": {
			ragEngine: test.MockRAGEngineWithNoInferenceService,
			expectedEnvs: map[string]string{
				"VECTOR_DB_TYPE": "faiss",
				"EMBEDDING_TYPE": "local",
				"MODEL_ID":       "BAAI/bge-small-en-v1.5",
			},
		},
		"test-rag-with-preset": {
			ragEngine: test.MockRAGEngineWithPreset,
			expectedEnvs: map[string]string{
				"VECTOR_DB_TYPE":     "faiss",
				"EMBEDDING_TYPE":     "local",
				"LLM_CONTEXT_WINDOW": "512",
				"MODEL_ID":           "BAAI/bge-small-en-v1.5",
				"LLM_INFERENCE_URL":  "http://localhost:5000/chat",
			},
		},
	}

	for k, tc := range testcases {
		t.Run(k, func(t *testing.T) {
			// Generate the deployment manifest
			obj := GenerateRAGDeploymentManifest(tc.ragEngine, test.MockRAGEngineWithPresetHash,
				"",  // imageName
				nil, // imagePullSecretRefs
				nil, // commands
				nil, // containerPorts
				nil, // livenessProbe
				nil, // readinessProbe
				v1.ResourceRequirements{},
				nil, // tolerations
				nil, // volumes
				nil, // volumeMount
			)

			// Expected label selector for the deployment
			appSelector := map[string]string{
				kaitov1beta1.LabelRAGEngineName: tc.ragEngine.Name,
			}

			// Check if the deployment's selector is correct
			if !reflect.DeepEqual(appSelector, obj.Spec.Selector.MatchLabels) {
				t.Errorf("RAGEngine workload selector is wrong")
			}

			// Check if the template labels match the expected labels
			if !reflect.DeepEqual(appSelector, obj.Spec.Template.ObjectMeta.Labels) {
				t.Errorf("RAGEngine template label is wrong")
			}

			// Verify owner references
			if len(obj.OwnerReferences) != 1 {
				t.Errorf("Expected 1 owner reference, got %d", len(obj.OwnerReferences))
			}
			ownerRef := obj.OwnerReferences[0]
			if ownerRef.APIVersion != kaitov1beta1.GroupVersion.String() {
				t.Errorf("Expected owner reference APIVersion %s, got %s", kaitov1beta1.GroupVersion.String(), ownerRef.APIVersion)
			}
			if ownerRef.Kind != "RAGEngine" {
				t.Errorf("Expected owner reference Kind %s, got %s", "RAGEngine", ownerRef.Kind)
			}
			if ownerRef.Name != tc.ragEngine.Name {
				t.Errorf("Expected owner reference Name %s, got %s", tc.ragEngine.Name, ownerRef.Name)
			}
			if string(ownerRef.UID) != string(tc.ragEngine.UID) {
				t.Errorf("Expected owner reference UID %s, got %s", string(tc.ragEngine.UID), string(ownerRef.UID))
			}
			if ownerRef.Controller == nil || !*ownerRef.Controller {
				t.Error("Expected owner reference Controller to be true")
			}

			// Verify the environment variables in the container
			envs := obj.Spec.Template.Spec.Containers[0].Env
			for i := range envs {
				if _, exists := tc.expectedEnvs[envs[i].Name]; exists {
					if tc.expectedEnvs[envs[i].Name] != envs[i].Value {
						t.Errorf("Expected %s to be %s, got %s", envs[i].Name, tc.expectedEnvs[envs[i].Name], envs[i].Value)
					}
					delete(tc.expectedEnvs, envs[i].Name)
				}
			}
			if len(tc.expectedEnvs) > 0 {
				t.Errorf("Missing required environment variables: %v", tc.expectedEnvs)
			}

			// Verify Lifecycle hooks
			lifecycle := obj.Spec.Template.Spec.Containers[0].Lifecycle
			if lifecycle == nil {
				t.Errorf("Expected Lifecycle to be configured")
			} else {
				// Verify PostStart hook
				if lifecycle.PostStart == nil || lifecycle.PostStart.Exec == nil {
					t.Errorf("Expected PostStart hook to be configured")
				} else {
					expectedPostStart := []string{"python3", "/app/ragengine/lifecycle/hooks.py", "poststart"}
					if !reflect.DeepEqual(lifecycle.PostStart.Exec.Command, expectedPostStart) {
						t.Errorf("Expected PostStart command %v, got %v", expectedPostStart, lifecycle.PostStart.Exec.Command)
					}
				}
				// Verify PreStop hook
				if lifecycle.PreStop == nil || lifecycle.PreStop.Exec == nil {
					t.Errorf("Expected PreStop hook to be configured")
				} else {
					expectedPreStop := []string{"/bin/sh", "-c", "python3 /app/ragengine/lifecycle/hooks.py prestop && sleep 5"}
					if !reflect.DeepEqual(lifecycle.PreStop.Exec.Command, expectedPreStop) {
						t.Errorf("Expected PreStop command %v, got %v", expectedPreStop, lifecycle.PreStop.Exec.Command)
					}
				}
			}

		})
	}
}

func TestRAGSetEnvGuardrails(t *testing.T) {
	findEnv := func(envs []v1.EnvVar, name string) (v1.EnvVar, bool) {
		for _, e := range envs {
			if e.Name == name {
				return e, true
			}
		}
		return v1.EnvVar{}, false
	}

	baseSpec := func() *kaitov1beta1.RAGEngineSpec {
		return &kaitov1beta1.RAGEngineSpec{
			Embedding: &kaitov1beta1.EmbeddingSpec{
				Local: &kaitov1beta1.LocalEmbeddingSpec{ModelID: "BAAI/bge-small-en-v1.5"},
			},
		}
	}

	t.Run("guardrails unset emits no guardrails envs", func(t *testing.T) {
		re := &kaitov1beta1.RAGEngine{
			ObjectMeta: metav1.ObjectMeta{Name: "rg", Namespace: "ns"},
			Spec:       baseSpec(),
		}
		envs := RAGSetEnv(re)
		for _, name := range []string{"OUTPUT_GUARDRAILS_ENABLED", "OUTPUT_GUARDRAILS_POLICY_PATH"} {
			if _, ok := findEnv(envs, name); ok {
				t.Errorf("expected %s to be absent when Guardrails is nil", name)
			}
		}
	})

	t.Run("guardrails enabled with policy", func(t *testing.T) {
		spec := baseSpec()
		spec.Guardrails = &kaitov1beta1.GuardrailsSpec{
			Enabled:      true,
			ConfigMapRef: &kaitov1beta1.ConfigMapReference{Name: "policy-cm"},
		}
		re := &kaitov1beta1.RAGEngine{
			ObjectMeta: metav1.ObjectMeta{Name: "rg", Namespace: "ns"},
			Spec:       spec,
		}
		envs := RAGSetEnv(re)
		want := map[string]string{
			"OUTPUT_GUARDRAILS_ENABLED":     "true",
			"OUTPUT_GUARDRAILS_POLICY_PATH": GuardrailsPolicyMountPath + "/" + GuardrailsPolicyFileName,
		}
		for name, expected := range want {
			got, ok := findEnv(envs, name)
			if !ok {
				t.Errorf("missing env %s", name)
				continue
			}
			if got.Value != expected {
				t.Errorf("env %s = %q, want %q", name, got.Value, expected)
			}
		}
	})

	t.Run("guardrails without ConfigMap emits no policy path", func(t *testing.T) {
		spec := baseSpec()
		spec.Guardrails = &kaitov1beta1.GuardrailsSpec{
			Enabled: true,
		}
		re := &kaitov1beta1.RAGEngine{
			ObjectMeta: metav1.ObjectMeta{Name: "rg", Namespace: "ns"},
			Spec:       spec,
		}
		envs := RAGSetEnv(re)
		if _, ok := findEnv(envs, "OUTPUT_GUARDRAILS_POLICY_PATH"); ok {
			t.Errorf("OUTPUT_GUARDRAILS_POLICY_PATH should be absent without ConfigMapRef")
		}
	})

	t.Run("guardrails disabled still emits explicit envs", func(t *testing.T) {
		spec := baseSpec()
		spec.Guardrails = &kaitov1beta1.GuardrailsSpec{Enabled: false}
		re := &kaitov1beta1.RAGEngine{
			ObjectMeta: metav1.ObjectMeta{Name: "rg", Namespace: "ns"},
			Spec:       spec,
		}
		envs := RAGSetEnv(re)
		got, ok := findEnv(envs, "OUTPUT_GUARDRAILS_ENABLED")
		if !ok {
			t.Fatalf("missing OUTPUT_GUARDRAILS_ENABLED")
		}
		if got.Value != "false" {
			t.Errorf("OUTPUT_GUARDRAILS_ENABLED = %q, want %q", got.Value, "false")
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
			kaitov1beta1.LabelRAGEngineName: ragEngine.Name,
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
		if ownerRef.APIVersion != kaitov1beta1.GroupVersion.String() {
			t.Errorf("Expected owner reference APIVersion %s, got %s", kaitov1beta1.GroupVersion.String(), ownerRef.APIVersion)
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

func TestRAGSetEnv(t *testing.T) {
	t.Run("test RAG environment variables", func(t *testing.T) {
		ragEngine := test.MockRAGEngineWithPreset

		envs := RAGSetEnv(ragEngine)

		// Check for required environment variables
		envMap := make(map[string]string)
		for _, env := range envs {
			envMap[env.Name] = env.Value
		}

		if envMap["EMBEDDING_TYPE"] != "local" {
			t.Errorf("expected EMBEDDING_TYPE 'local', got %s", envMap["EMBEDDING_TYPE"])
		}
		if envMap["VECTOR_DB_TYPE"] != "faiss" {
			t.Errorf("expected VECTOR_DB_TYPE 'faiss', got %s", envMap["VECTOR_DB_TYPE"])
		}
		if envMap["MODEL_ID"] != "BAAI/bge-small-en-v1.5" {
			t.Errorf("expected MODEL_ID 'BAAI/bge-small-en-v1.5', got %s", envMap["MODEL_ID"])
		}
	})
}
