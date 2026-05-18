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

package e2e

import (
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

// These tests validate the distributed cache integration with the workspace
// controller. They require:
// - KAITO operator deployed with distributedCache feature gate enabled
// - Tachyon cache operator installed (Cache CRD available)
// - Tachyon cache CR in Ready state
// - Azure Blob Storage configured in the Tachyon provider
//
// Set TEST_CACHE_ENABLED=true to run these tests.

var _ = Describe("Cache Integration", Label("cache"), func() {

	BeforeEach(func() {
		if utils.GetEnv("TEST_CACHE_ENABLED") != "true" {
			Skip("Cache integration tests disabled (set TEST_CACHE_ENABLED=true)")
		}
	})

	Context("Workspace with model weights cache", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should inject cache label and KAITO_MODEL_PATH into inference pod", func() {
			uniqueID := fmt.Sprint("cache-mw-", rand.Intn(1000))

			By("Creating a workspace with model weights cache enabled")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
					Provider: "tachyon",
					Mode:     kaitov1beta1.CacheModeOpportunistic,
				},
			})
			createAndValidateWorkspace(workspaceObj)

			By("Verifying the StatefulSet has cache mutations")
			validateCacheMutationsInStatefulSet(workspaceObj)

			By("Verifying the ModelWeightsCacheReady condition is set")
			validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady))
		})

		AfterEach(func() {
			if workspaceObj != nil {
				cleanupWorkspace(workspaceObj)
			}
		})
	})

	Context("Workspace with KV cache", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should inject KV transfer config into inference pod", func() {
			uniqueID := fmt.Sprint("cache-kv-", rand.Intn(1000))

			By("Creating a workspace with KV cache enabled")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				KVCache: &kaitov1beta1.KVCacheConfig{
					Provider: "tachyon",
					Mode:     kaitov1beta1.CacheModeOpportunistic,
				},
			})
			createAndValidateWorkspace(workspaceObj)

			By("Verifying the StatefulSet has VLLM_KV_TRANSFER_CONFIG")
			validateKVCacheEnvVar(workspaceObj)

			By("Verifying the KVCacheReady condition is set")
			validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady))
		})

		AfterEach(func() {
			if workspaceObj != nil {
				cleanupWorkspace(workspaceObj)
			}
		})
	})

	Context("Workspace with both caches", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should inject both cache label and KV config", func() {
			uniqueID := fmt.Sprint("cache-both-", rand.Intn(1000))

			By("Creating a workspace with both model weights and KV cache")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
					Provider: "tachyon",
					Mode:     kaitov1beta1.CacheModeOpportunistic,
				},
				KVCache: &kaitov1beta1.KVCacheConfig{
					Provider: "tachyon",
					Mode:     kaitov1beta1.CacheModeOpportunistic,
				},
			})
			createAndValidateWorkspace(workspaceObj)

			By("Verifying both cache mutations are present")
			validateCacheMutationsInStatefulSet(workspaceObj)
			validateKVCacheEnvVar(workspaceObj)
		})

		AfterEach(func() {
			if workspaceObj != nil {
				cleanupWorkspace(workspaceObj)
			}
		})
	})

	Context("Prewarm Job lifecycle", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should create a prewarm Job for the model", func() {
			uniqueID := fmt.Sprint("cache-pw-", rand.Intn(1000))

			By("Creating a workspace with required model weights cache")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
					Provider: "tachyon",
					Mode:     kaitov1beta1.CacheModeRequired,
				},
			})
			createAndValidateWorkspace(workspaceObj)

			By("Verifying a prewarm Job is created")
			validatePrewarmJobCreated(workspaceObj)
		})

		AfterEach(func() {
			if workspaceObj != nil {
				cleanupWorkspace(workspaceObj)
			}
		})
	})

	Context("Required mode blocks without cache", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should block deployment when cache infrastructure is not ready", func() {
			uniqueID := fmt.Sprint("cache-block-", rand.Intn(1000))

			By("Creating a workspace with required mode and non-existent provider")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				ModelWeights: &kaitov1beta1.ModelWeightsCacheConfig{
					Provider: "nonexistent-provider",
					Mode:     kaitov1beta1.CacheModeRequired,
				},
			})

			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed())

			By("Verifying the workspace condition shows cache not ready")
			Eventually(func() bool {
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: workspaceObj.Namespace,
					Name:      workspaceObj.Name,
				}, workspaceObj, &client.GetOptions{})
				if err != nil {
					return false
				}
				_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
					return condition.Type == string(kaitov1beta1.WorkspaceConditionTypeModelWeightsCacheReady) &&
						condition.Status == metav1.ConditionFalse
				})
				return conditionFound
			}, 5*time.Minute, utils.PollInterval).Should(BeTrue(),
				"Expected ModelWeightsCacheReady=False condition when provider is unavailable")
		})

		AfterEach(func() {
			if workspaceObj != nil {
				cleanupWorkspace(workspaceObj)
			}
		})
	})
})

// generateCacheWorkspace creates a workspace manifest with cache configuration.
func generateCacheWorkspace(name, namespace string, cacheSpec kaitov1beta1.CacheSpec) *kaitov1beta1.Workspace {
	ws := utils.GenerateInferenceWorkspaceManifestWithVLLM(
		name, namespace, "",
		1, "Standard_NV36ads_A10_v5",
		&metav1.LabelSelector{
			MatchLabels: map[string]string{"kaito-workspace": "cache-e2e-test"},
		},
		nil,
		PresetPhi4MiniModel,
		nil, nil, nil, "", "",
	)

	// Set runtime annotation.
	if ws.Annotations == nil {
		ws.Annotations = make(map[string]string)
	}
	ws.Annotations[kaitov1beta1.AnnotationWorkspaceRuntime] = string(model.RuntimeNameVLLM)

	// Set cache spec.
	ws.Cache = &cacheSpec
	return ws
}

// validateCacheMutationsInStatefulSet checks that the StatefulSet pod template
// has the expected cache mutations: injection label + KAITO_MODEL_PATH env var.
func validateCacheMutationsInStatefulSet(workspaceObj *kaitov1beta1.Workspace) {
	Eventually(func() bool {
		sts := &appsv1.StatefulSet{}
		err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: workspaceObj.Namespace,
			Name:      workspaceObj.Name,
		}, sts)
		if err != nil {
			GinkgoWriter.Printf("StatefulSet not found yet: %v\n", err)
			return false
		}

		// Check injection label on pod template.
		labels := sts.Spec.Template.Labels
		if labels == nil || labels["tachyon.azure.com/inject"] != "true" {
			GinkgoWriter.Println("tachyon.azure.com/inject label not found on pod template")
			return false
		}

		// Check env vars on first container.
		podSpec := sts.Spec.Template.Spec
		if len(podSpec.Containers) == 0 {
			return false
		}
		envMap := make(map[string]string)
		for _, e := range podSpec.Containers[0].Env {
			envMap[e.Name] = e.Value
		}

		// Verify KAITO_MODEL_PATH is set and starts with /mnt/models/.
		modelPath := envMap["KAITO_MODEL_PATH"]
		if len(modelPath) < 12 || modelPath[:12] != "/mnt/models/" {
			GinkgoWriter.Printf("KAITO_MODEL_PATH has unexpected value: %s\n", modelPath)
			return false
		}

		return true
	}, 15*time.Minute, utils.PollInterval).Should(BeTrue(),
		"StatefulSet should have cache injection label and KAITO_MODEL_PATH")
}

// validateKVCacheEnvVar checks that VLLM_KV_TRANSFER_CONFIG env var is set.
func validateKVCacheEnvVar(workspaceObj *kaitov1beta1.Workspace) {
	Eventually(func() bool {
		sts := &appsv1.StatefulSet{}
		err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: workspaceObj.Namespace,
			Name:      workspaceObj.Name,
		}, sts)
		if err != nil {
			return false
		}

		if len(sts.Spec.Template.Spec.Containers) == 0 {
			return false
		}

		for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "VLLM_KV_TRANSFER_CONFIG" && e.Value != "" {
				return true
			}
		}
		return false
	}, 15*time.Minute, utils.PollInterval).Should(BeTrue(),
		"StatefulSet should have VLLM_KV_TRANSFER_CONFIG env var")
}

// validateCacheCondition checks that the specified cache condition is set.
func validateCacheCondition(workspaceObj *kaitov1beta1.Workspace, conditionType string) {
	Eventually(func() bool {
		err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: workspaceObj.Namespace,
			Name:      workspaceObj.Name,
		}, workspaceObj, &client.GetOptions{})
		if err != nil {
			return false
		}

		_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == conditionType
		})
		return conditionFound
	}, 10*time.Minute, utils.PollInterval).Should(BeTrue(),
		fmt.Sprintf("Expected %s condition to be set", conditionType))
}

// validatePrewarmJobCreated checks that a prewarm Job was created for the workspace.
func validatePrewarmJobCreated(workspaceObj *kaitov1beta1.Workspace) {
	Eventually(func() bool {
		jobList := &batchv1.JobList{}
		err := utils.TestingCluster.KubeClient.List(ctx, jobList,
			client.InNamespace(workspaceObj.Namespace),
			client.MatchingLabels{
				"kaito.sh/cache-prewarm": "true",
			},
		)
		if err != nil {
			GinkgoWriter.Printf("Error listing prewarm jobs: %v\n", err)
			return false
		}

		for _, job := range jobList.Items {
			// Check owner reference points to our workspace.
			for _, ref := range job.OwnerReferences {
				if ref.Kind == "Workspace" && ref.Name == workspaceObj.Name {
					GinkgoWriter.Printf("Found prewarm Job: %s\n", job.Name)
					return true
				}
			}
		}
		return false
	}, 10*time.Minute, utils.PollInterval).Should(BeTrue(),
		"Expected a prewarm Job to be created for the workspace")
}

// cleanupWorkspace deletes the workspace and waits for cleanup.
func cleanupWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	By(fmt.Sprintf("Cleaning up workspace %s", workspaceObj.Name))
	err := utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj)
	if err != nil {
		GinkgoWriter.Printf("Error deleting workspace: %v\n", err)
	}
}
