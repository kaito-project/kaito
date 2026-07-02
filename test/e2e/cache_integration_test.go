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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	dacs "github.com/kaito-project/kaito/pkg/cache/dacs"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

// These tests validate the distributed cache integration with the workspace
// controller. They require:
// - KAITO operator deployed with distributedCache feature gate enabled
// - DACS cache operator installed (Cache CRD available)
// - DACS cache CR in Ready state
//
// Set TEST_CACHE_ENABLED=true to run these tests.

var _ = Describe("Cache Integration", Label("cache"), func() {

	BeforeEach(func() {
		if utils.GetEnv("TEST_CACHE_ENABLED") != "true" {
			Skip("Cache integration tests disabled (set TEST_CACHE_ENABLED=true)")
		}
		// Ensure si-config ConfigMap exists in the test namespace.
		siCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "si-config",
				Namespace: namespaceName,
			},
			Data: map[string]string{
				"storageIntercept.config": "storagePath /mnt/cache\ntype blob\nazBlobDynamicAccount true\nazBlobDynamicContainer true\nazBlobUseAzureIdentitySDK true\ncacheEnable true\ncacheEnableRemote true\ncacheServerPort 9065\ncacheServerDiscoveryEnabled true\ncacheServerDiscoveryEndpoint cache-sample-discovery.dacs-cache-system.svc.cluster.local\n",
			},
		}
		_ = utils.TestingCluster.KubeClient.Create(ctx, siCM)
	})

	Context("Workspace with model weights cache", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should inject ImageVolume, cache env vars, and label into inference pod", func() {
			uniqueID := fmt.Sprint("cache-mw-", rand.Intn(1000))

			By("Creating a workspace with model weights cache enabled")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				ModelCache: &kaitov1beta1.ModelCacheSpec{
					Provider: "dacs",
					Mode:     kaitov1beta1.CacheModeOpportunistic,
				},
			})
			createAndValidateWorkspace(workspaceObj)

			By("Verifying the StatefulSet has cache-client ImageVolume")
			validateImageVolumeInStatefulSet(workspaceObj)

			By("Verifying the StatefulSet has cache env vars")
			validateCacheEnvVarsInStatefulSet(workspaceObj)

			By("Verifying the ModelCacheReady condition is set")
			validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
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
				KVCache: &kaitov1beta1.KVCacheSpec{
					Provider: "dacs",
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

	Context("Required mode sets ModelCacheReady condition", func() {
		var workspaceObj *kaitov1beta1.Workspace

		It("should set ModelCacheReady condition when cache is configured in Required mode", func() {
			uniqueID := fmt.Sprint("cache-req-", rand.Intn(1000))

			By("Creating a workspace with Required mode cache")
			workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, kaitov1beta1.CacheSpec{
				ModelCache: &kaitov1beta1.ModelCacheSpec{
					Provider: "dacs",
					Mode:     kaitov1beta1.CacheModeRequired,
				},
			})

			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed())

			By("Verifying the ModelCacheReady condition is set")
			Eventually(func() bool {
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: workspaceObj.Namespace,
					Name:      workspaceObj.Name,
				}, workspaceObj, &client.GetOptions{})
				if err != nil {
					return false
				}
				_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
					return condition.Type == string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady)
				})
				return conditionFound
			}, 2*time.Minute, utils.PollInterval).Should(BeTrue(),
				"Expected ModelCacheReady condition to be set for Required mode workspace")
		})

		AfterEach(func() {
			if workspaceObj != nil {
				cleanupWorkspace(workspaceObj)
			}
		})
	})
})

// generateCacheWorkspace creates a workspace manifest with cache configuration
// using template inference to allow running on CPU nodes without GPU validation.
func generateCacheWorkspace(name, namespace string, cacheSpec kaitov1beta1.CacheSpec) *kaitov1beta1.Workspace {
	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				kaitov1beta1.AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
			},
		},
	}
	ws.Resource = kaitov1beta1.ResourceSpec{
		InstanceType: "Standard_D32s_v3",
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"apps": "vllm-cache"},
		},
	}
	ws.Inference = &kaitov1beta1.InferenceSpec{
		Template: &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"azure.workload.identity/use": "true",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "vllm-sa",
				Containers: []corev1.Container{
					{
						Name:    "vllm",
						Image:   "hariazstortest.azurecr.io/vllm-cpu-streamer:v0.23.0-nocache",
						Command: []string{"python3", "-m", "vllm.entrypoints.openai.api_server"},
						Args: []string{
							"--model=az://qwen/Qwen2.5-Coder-7B-Instruct",
							"--dtype=float32",
							"--max-model-len=512",
							"--load-format=runai_streamer",
							"--model-loader-extra-config={\"concurrency\":8}",
						},
						Env: []corev1.EnvVar{
							{Name: "AZURE_STORAGE_ACCOUNT_NAME", Value: "harikaito"},
							{Name: "STORAGE_INTERCEPT_CONFIG_FILEPATH", Value: "/etc/si-config/storageIntercept.config"},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("8"),
								corev1.ResourceMemory: resource.MustParse("32Gi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "si-config", MountPath: "/etc/si-config", ReadOnly: true},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "si-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "si-config"},
							},
						},
					},
				},
			},
		},
	}
	ws.Cache = &cacheSpec
	return ws
}

// validateImageVolumeInStatefulSet checks that the StatefulSet has the cache-client
// ImageVolume and corresponding volume mount.
func validateImageVolumeInStatefulSet(workspaceObj *kaitov1beta1.Workspace) {
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

		// Check cache-client volume exists with Image source.
		volumeFound := false
		for _, vol := range sts.Spec.Template.Spec.Volumes {
			if vol.Name == dacs.ClientVolumeName && vol.Image != nil {
				volumeFound = true
				break
			}
		}
		if !volumeFound {
			GinkgoWriter.Println("cache-client ImageVolume not found")
			return false
		}

		// Check volume mount on first container.
		if len(sts.Spec.Template.Spec.Containers) == 0 {
			return false
		}
		mountFound := false
		for _, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == dacs.ClientVolumeName && mount.MountPath == dacs.ClientMountPath {
				mountFound = true
				break
			}
		}
		if !mountFound {
			GinkgoWriter.Println("cache-client volume mount not found")
		}
		return mountFound
	}, 15*time.Minute, utils.PollInterval).Should(BeTrue(),
		"StatefulSet should have cache-client ImageVolume and mount")
}

// validateCacheEnvVarsInStatefulSet checks that the expected cache environment
// variables are set on the inference container.
func validateCacheEnvVarsInStatefulSet(workspaceObj *kaitov1beta1.Workspace) {
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
		envMap := make(map[string]string)
		for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
			envMap[e.Name] = e.Value
		}

		requiredEnvVars := []string{
			"RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_ENABLED",
			"RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB",
			"LD_LIBRARY_PATH",
			"RUNAI_STREAMER_CACHE_ENABLED",
			"CACHE_DISCOVERY_URL",
			"CACHE_SERVER_PORT",
		}
		for _, name := range requiredEnvVars {
			if _, ok := envMap[name]; !ok {
				GinkgoWriter.Printf("Missing env var: %s\n", name)
				return false
			}
		}

		if envMap["RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB"] != dacs.ClientLibPath {
			GinkgoWriter.Printf("AZURE_CACHE_LIB has unexpected value: %s\n",
				envMap["RUNAI_STREAMER_EXPERIMENTAL_AZURE_CACHE_LIB"])
			return false
		}

		labels := sts.Spec.Template.Labels
		if labels == nil || labels[dacs.InjectLabelKey] != dacs.InjectLabelValue {
			GinkgoWriter.Println("dacs inject label not found")
			return false
		}

		return true
	}, 15*time.Minute, utils.PollInterval).Should(BeTrue(),
		"StatefulSet should have all cache env vars and injection label")
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

// cleanupWorkspace deletes the workspace and waits for cleanup.
func cleanupWorkspace(workspaceObj *kaitov1beta1.Workspace) {
	By(fmt.Sprintf("Cleaning up workspace %s", workspaceObj.Name))
	err := utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj)
	if err != nil {
		GinkgoWriter.Printf("Error deleting workspace: %v\n", err)
	}
}
