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

// The blank imports of pkg/cache/dacs and pkg/cache/noop below self-register each
// provider's conformance Expectations. The provider-agnostic specs iterate
// cache.ListExpectations(), so any newly added, E2E-capable provider is automatically
// exercised end-to-end with no changes to this file.
import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
	_ "github.com/kaito-project/kaito/pkg/cache/dacs"
	_ "github.com/kaito-project/kaito/pkg/cache/noop"
	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

// These tests validate the distributed cache integration with the workspace
// controller. They require:
// - KAITO operator deployed with distributedCache feature gate enabled
// - A cache backend for each E2E-capable provider installed and Ready
//
// Set TEST_CACHE_ENABLED=true to run these tests.
//
// The tests are provider-agnostic: they are driven by the conformance expectations
// table (pkg/cache). Each provider declares, in its own package, what pod mutations
// it is expected to inject; these tests assert the deployed StatefulSet matches that
// contract using cache.AssertPodSpec. Adding a provider requires no edits here.

var _ = Describe("Cache Integration", Label("cache"), func() {

	BeforeEach(func() {
		if utils.GetEnv("TEST_CACHE_ENABLED") != "true" {
			Skip("Cache integration tests disabled (set TEST_CACHE_ENABLED=true)")
		}
		// Provider-specific prerequisites (serving image, backend env) are checked
		// per provider by its E2EWorkspace hook, which skips the affected specs.
		// Ensure the vllm-sa ServiceAccount exists in the test namespace so
		// StatefulSet pods can be created. When AZURE_CLIENT_ID is set the SA
		// is annotated for workload identity so the pod receives a token.
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vllm-sa",
				Namespace: namespaceName,
			},
		}
		if clientID := utils.GetEnv("AZURE_CLIENT_ID"); clientID != "" {
			sa.Annotations = map[string]string{
				"azure.workload.identity/client-id": clientID,
			}
		}
		_ = utils.TestingCluster.KubeClient.Create(ctx, sa) // ignore AlreadyExists
	})

	// Iterate every provider discovered from the conformance registry so new
	// providers are covered automatically. e2e coverage is the default; a provider
	// is only skipped here if it explicitly opts out via E2EExempt.
	for _, exp := range cache.ListExpectations() {
		exp := exp
		if !exp.RunsE2E() {
			continue
		}
		provider := exp.Provider

		Context(fmt.Sprintf("Provider %q: model weights cache", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should inject the provider's declared model-weights mutations into the inference pod", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-mw-", rand.Intn(1000))

				By("Creating a workspace with model weights cache enabled")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{
						Provider: provider,
						Mode:     cache.ModeOpportunistic,
					},
				})
				createAndValidateWorkspace(workspaceObj)

				By("Verifying the StatefulSet conforms to the provider's model-weights expectations")
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernModelWeights)

				By("Verifying the ModelCacheReady condition is set")
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		Context(fmt.Sprintf("Provider %q: KV cache", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should inject the provider's declared KV-cache mutations into the inference pod", func() {
				if !exp.KVCache.Supported {
					Skip(fmt.Sprintf("provider %q does not support the KV cache concern", provider))
				}
				uniqueID := fmt.Sprint("cache-kv-", rand.Intn(1000))

				By("Creating a workspace with KV cache enabled")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					KVCache: &cache.ConcernConfig{
						Provider: provider,
						Mode:     cache.ModeOpportunistic,
					},
				})
				createAndValidateWorkspace(workspaceObj)

				By("Verifying the StatefulSet conforms to the provider's KV-cache expectations")
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernKVCache)

				By("Verifying the KVCacheReady condition is set")
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady))
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		Context(fmt.Sprintf("Provider %q: Required mode sets ModelCacheReady condition", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should set ModelCacheReady condition when cache is configured in Required mode", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-req-", rand.Intn(1000))

				By("Creating a workspace with Required mode cache")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{
						Provider: provider,
						Mode:     cache.ModeRequired,
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
					workspaceObj = nil
				}
			})
		})

		// Required mode with unavailable cache blocks workload creation.
		Context(fmt.Sprintf("Provider %q: Required mode blocks workload when cache is unavailable", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should set ModelCacheReady=False and not create a StatefulSet", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				if len(exp.ModelWeights.RequiredLabels) == 0 && len(exp.ModelWeights.RequiredEnvVars) == 0 {
					Skip(fmt.Sprintf("provider %q has no injection artifacts; unavailability cannot be tested", provider))
				}
				uniqueID := fmt.Sprint("cache-req-block-", rand.Intn(10000))

				By("Creating a workspace with Required mode and a nonexistent cacheName")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{
						Provider:   provider,
						Mode:       cache.ModeRequired,
						Parameters: map[string]string{"cacheName": "nonexistent-cache-cr-e2e"},
					},
				})
				Eventually(func() error {
					return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
				}, utils.PollTimeout, utils.PollInterval).Should(Succeed())

				By("Verifying ModelCacheReady condition is False with a clear reason")
				Eventually(func(g Gomega) {
					err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, workspaceObj)
					g.Expect(err).NotTo(HaveOccurred())
					cond, found := lo.Find(workspaceObj.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady)
					})
					g.Expect(found).To(BeTrue(), "ModelCacheReady condition not set")
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(cond.Reason).NotTo(BeEmpty(), "reason must explain why cache is unavailable")
				}, 3*time.Minute, utils.PollInterval).Should(Succeed())

				By("Verifying no StatefulSet is created (workload blocked)")
				Consistently(func() bool {
					sts := &appsv1.StatefulSet{}
					err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, sts)
					return err != nil // should remain not-found
				}, 30*time.Second, 5*time.Second).Should(BeTrue(),
					"StatefulSet should not be created when Required cache is unavailable")
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// Opportunistic mode with unavailable cache proceeds without injection.
		Context(fmt.Sprintf("Provider %q: Opportunistic mode proceeds when cache is unavailable", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should create StatefulSet without cache injection labels", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				if len(exp.ModelWeights.RequiredLabels) == 0 && len(exp.ModelWeights.RequiredEnvVars) == 0 {
					Skip(fmt.Sprintf("provider %q has no injection artifacts; unavailability cannot be tested", provider))
				}
				uniqueID := fmt.Sprint("cache-opp-proceed-", rand.Intn(10000))

				By("Creating a workspace with Opportunistic mode and a nonexistent cacheName")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{
						Provider:   provider,
						Mode:       cache.ModeOpportunistic,
						Parameters: map[string]string{"cacheName": "nonexistent-cache-cr-e2e"},
					},
				})
				createAndValidateWorkspace(workspaceObj)

				By("Verifying StatefulSet exists but does NOT carry the provider's injection labels")
				me := exp.ForConcern(cache.CacheConcernModelWeights)
				Eventually(func() error {
					sts := &appsv1.StatefulSet{}
					if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, sts); err != nil {
						return err
					}
					for k, v := range me.RequiredLabels {
						if actual, ok := sts.Spec.Template.Labels[k]; ok && actual == v {
							return fmt.Errorf("injection label %s=%s present despite unavailable cache", k, v)
						}
					}
					return nil
				}, 10*time.Minute, utils.PollInterval).Should(Succeed())
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// User-defined env vars are preserved after cache injection.
		Context(fmt.Sprintf("Provider %q: user env vars preserved after cache injection", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should retain user-defined env vars on the model container alongside injected cache vars", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-env-pres-", rand.Intn(10000))

				By("Creating a workspace with a custom user env var")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{
						Provider: provider,
						Mode:     cache.ModeOpportunistic,
					},
				})
				// Add a user env var to the model container.
				if workspaceObj.Inference != nil && workspaceObj.Inference.Template != nil &&
					len(workspaceObj.Inference.Template.Spec.Containers) > 0 {
					workspaceObj.Inference.Template.Spec.Containers[0].Env = append(
						workspaceObj.Inference.Template.Spec.Containers[0].Env,
						corev1.EnvVar{Name: "USER_CUSTOM_ENV", Value: "/custom/user/path"},
					)
				}
				createAndValidateWorkspace(workspaceObj)

				By("Verifying cache is injected AND user env var is preserved")
				me := exp.ForConcern(cache.CacheConcernModelWeights)
				Eventually(func() error {
					sts := &appsv1.StatefulSet{}
					if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, sts); err != nil {
						return err
					}
					// Check injection happened.
					for k, v := range me.RequiredLabels {
						if actual := sts.Spec.Template.Labels[k]; actual != v {
							return fmt.Errorf("cache not injected yet (label %s=%q, want %q)", k, actual, v)
						}
					}
					// Check user env preserved.
					if len(sts.Spec.Template.Spec.Containers) == 0 {
						return fmt.Errorf("no containers in StatefulSet")
					}
					for _, e := range sts.Spec.Template.Spec.Containers[0].Env {
						if e.Name == "USER_CUSTOM_ENV" && e.Value == "/custom/user/path" {
							return nil
						}
					}
					return fmt.Errorf("user env var USER_CUSTOM_ENV not found after cache injection")
				}, 10*time.Minute, utils.PollInterval).Should(Succeed())
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// Cache condition transitions to False when backend becomes unavailable.
		Context(fmt.Sprintf("Provider %q: cache condition transitions to False when backend removed", provider), func() {
			It("should set ModelCacheReady=False after cache backend is deleted", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				Skip("Not yet implemented: controller does not re-evaluate cache readiness after initial setup")
			})
		})

		// Cache backend scale down/up emits Ready/NotReady transitions.
		Context(fmt.Sprintf("Provider %q: cache backend scale emits condition transitions", provider), func() {
			It("should transition ModelCacheReady between True and False as backend scales", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				Skip("Not yet implemented: controller does not watch backend pod events")
			})
		})

		// t3/t15: Disabled mode injects nothing and sets no cache condition.
		Context(fmt.Sprintf("Provider %q: Disabled mode injects nothing", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should create the StatefulSet without cache labels/env and set no cache condition", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-dis-", rand.Intn(10000))

				By("Creating a workspace with model cache Disabled")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{
						Provider: provider,
						Mode:     cache.ModeDisabled,
					},
				})
				createAndValidateWorkspace(workspaceObj)

				By("Verifying the StatefulSet is created but carries none of the provider's cache mutations")
				me := exp.ForConcern(cache.CacheConcernModelWeights)
				Eventually(func() error {
					sts := &appsv1.StatefulSet{}
					if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, sts); err != nil {
						return err
					}
					return assertNoCacheMutations(&sts.Spec.Template, me)
				}, 10*time.Minute, utils.PollInterval).Should(Succeed())

				By("Verifying no ModelCacheReady condition is set for Disabled mode")
				Consistently(func() bool {
					_, found, _ := getWorkspaceCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
					return found
				}, 20*time.Second, 5*time.Second).Should(BeFalse(),
					"Disabled mode must not set the ModelCacheReady condition")
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// t20: model weights + KV cache configured together; both concerns injected
		// and both conditions set.
		Context(fmt.Sprintf("Provider %q: model + KV cache together", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should inject both concerns and set both cache conditions", func() {
				if !exp.ModelWeights.Supported || !exp.KVCache.Supported {
					Skip(fmt.Sprintf("provider %q does not support both model weights and KV cache", provider))
				}
				uniqueID := fmt.Sprint("cache-both-", rand.Intn(10000))

				By("Creating a workspace with both model and KV cache enabled")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
					KVCache:      &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				createAndValidateWorkspace(workspaceObj)

				By("Verifying the StatefulSet conforms to both concerns")
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernModelWeights)
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernKVCache)

				By("Verifying both cache conditions are set")
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeKVCacheReady))
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// t17: cache attachment changes trigger Workspace reconciliation through the
		// ConfigMap watch and update the rendered StatefulSet.
		Context(fmt.Sprintf("Provider %q: cache attachment changes reconcile the running workspace", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should keep the originally injected cache mutations unchanged after a live mode update", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-upd-", rand.Intn(10000))

				By("Creating the workspace initially Opportunistic so cache mutations are injected at STS creation")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				createAndValidateWorkspace(workspaceObj)
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernModelWeights)

				By("Updating the cache attachment to Disabled and confirming cache mutations are removed")
				me := exp.ForConcern(cache.CacheConcernModelWeights)
				updateCacheMode(workspaceObj, cache.ModeDisabled)
				Eventually(func() error {
					sts := &appsv1.StatefulSet{}
					if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: workspaceObj.Namespace, Name: workspaceObj.Name}, sts); err != nil {
						return err
					}
					for name, value := range me.RequiredLabels {
						if sts.Spec.Template.Labels[name] == value {
							return fmt.Errorf("cache label %s=%s is still present", name, value)
						}
					}
					for _, container := range sts.Spec.Template.Spec.Containers {
						for _, env := range container.Env {
							if slices.Contains(me.RequiredEnvVars, env.Name) {
								return fmt.Errorf("cache environment variable %s is still present", env.Name)
							}
						}
					}
					return nil
				}, 5*time.Minute, utils.PollInterval).Should(Succeed())
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// t29: deleting and recreating a workspace with the same name leaves no stale
		// conditions; the recreated workspace reconciles cleanly.
		Context(fmt.Sprintf("Provider %q: condition cleanup on delete/recreate", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should not carry stale cache conditions after delete/recreate", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-recr-", rand.Intn(10000))

				By("Creating, validating, then deleting the workspace")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				createAndValidateWorkspace(workspaceObj)
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
				Expect(deleteWorkspace(workspaceObj)).To(Succeed())
				Eventually(func() bool {
					err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: workspaceObj.Namespace, Name: workspaceObj.Name}, &kaitov1beta1.Workspace{})
					return client.IgnoreNotFound(err) == nil && err != nil
				}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "workspace should be deleted")

				By("Recreating the workspace with the same name and verifying it reconciles cleanly")
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				createAndValidateWorkspace(workspaceObj)
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernModelWeights)
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// t25: several workspaces created concurrently all reconcile. They reuse the
		// shared BYO node (LabelSelector), so no extra nodes are provisioned.
		Context(fmt.Sprintf("Provider %q: concurrent workspace creation", provider), func() {
			var workspaces []*kaitov1beta1.Workspace

			It("should reconcile all concurrently-created cache workspaces", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				const n = 3
				By(fmt.Sprintf("Creating %d cache workspaces concurrently on the shared node", n))
				for i := 0; i < n; i++ {
					ws := generateCacheWorkspace(fmt.Sprint("cache-conc-", rand.Intn(100000)), namespaceName, provider, cache.Attachment{
						ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
					})
					Expect(utils.TestingCluster.KubeClient.Create(ctx, ws)).To(Succeed())
					workspaces = append(workspaces, ws)
				}

				By("Verifying every workspace's StatefulSet conforms")
				for _, ws := range workspaces {
					validateStatefulSetConformance(ws, exp, cache.CacheConcernModelWeights)
				}
			})

			AfterEach(func() {
				for _, ws := range workspaces {
					cleanupWorkspace(ws)
				}
				workspaces = nil
			})
		})

		// t7: an InferenceSet propagates its cache attachment to every child Workspace.
		Context(fmt.Sprintf("Provider %q: InferenceSet propagates cache to children", provider), func() {
			var isObj *kaitov1beta1.InferenceSet

			It("should create child workspaces that inherit the cache attachment", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-is-", rand.Intn(10000))
				isObj = generateCacheInferenceSet(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				Expect(utils.TestingCluster.KubeClient.Create(ctx, isObj)).To(Succeed())

				By("Verifying at least one child workspace is created carrying the cache attachment")
				Eventually(func() error {
					list := &kaitov1beta1.WorkspaceList{}
					if err := utils.TestingCluster.KubeClient.List(ctx, list,
						client.InNamespace(namespaceName),
						client.MatchingLabels{consts.WorkspaceCreatedByInferenceSetLabel: isObj.Name}); err != nil {
						return err
					}
					if len(list.Items) == 0 {
						return fmt.Errorf("no child workspaces yet")
					}
					expectedConfigMap := isObj.Spec.Template.Annotations[cache.ConfigAnnotation]
					for i := range list.Items {
						child := &list.Items[i]
						if child.Annotations[cache.ConfigAnnotation] != expectedConfigMap {
							return fmt.Errorf("child %s did not inherit cache attachment %q", child.Name, expectedConfigMap)
						}
					}
					return nil
				}, 5*time.Minute, utils.PollInterval).Should(Succeed())
			})

			AfterEach(func() {
				if isObj != nil {
					_ = deleteInferenceSet(isObj)
					isObj = nil
				}
			})
		})

		// t24: two workspaces in different namespaces each get their own cache
		// injection independently (no cross-contamination). Both reuse the shared
		// BYO node, so no extra nodes are provisioned.
		Context(fmt.Sprintf("Provider %q: multi-tenant namespace isolation", provider), func() {
			var wsA, wsB *kaitov1beta1.Workspace
			var otherNS string

			It("should inject cache independently for workspaces in separate namespaces", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				otherNS = fmt.Sprintf("%s-cache-tenant-%d", namespaceName, rand.Intn(10000))
				ensureCacheNamespace(otherNS)

				wsA = generateCacheWorkspace(fmt.Sprint("cache-ta-", rand.Intn(10000)), namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				wsB = generateCacheWorkspace(fmt.Sprint("cache-tb-", rand.Intn(10000)), otherNS, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				createAndValidateWorkspace(wsA)
				createAndValidateWorkspace(wsB)

				By("Verifying each namespace's StatefulSet conforms independently")
				validateStatefulSetConformance(wsA, exp, cache.CacheConcernModelWeights)
				validateStatefulSetConformance(wsB, exp, cache.CacheConcernModelWeights)
			})

			AfterEach(func() {
				if wsA != nil {
					cleanupWorkspace(wsA)
					wsA = nil
				}
				if wsB != nil {
					cleanupWorkspace(wsB)
					wsB = nil
				}
				if otherNS != "" {
					_ = utils.TestingCluster.KubeClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNS}})
					otherNS = ""
				}
			})
		})

		// t13: the cache condition converges to Ready and stays there (no flapping /
		// stuck states) for an Opportunistic workspace.
		Context(fmt.Sprintf("Provider %q: readiness converges without flapping", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should converge ModelCacheReady to a stable value", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-race-", rand.Intn(10000))
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				createAndValidateWorkspace(workspaceObj)

				By("Waiting for ModelCacheReady to be set")
				validateCacheCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))

				By("Verifying the condition status remains stable (no flapping)")
				var last metav1.ConditionStatus
				Consistently(func() error {
					c, found, err := getWorkspaceCondition(workspaceObj, string(kaitov1beta1.WorkspaceConditionTypeModelCacheReady))
					if err != nil || !found {
						return fmt.Errorf("condition missing")
					}
					if last != "" && c.Status != last {
						return fmt.Errorf("condition flapped %s -> %s", last, c.Status)
					}
					last = c.Status
					return nil
				}, 30*time.Second, 5*time.Second).Should(Succeed())
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// t16: a workspace configured with both an adapter and cache still receives
		// the cache mutations (no conflict between adapter and cache injection).
		Context(fmt.Sprintf("Provider %q: cache coexists with adapters", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should inject cache mutations even when adapters are configured", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-adpt-", rand.Intn(10000))
				workspaceObj = generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				strength := "0.5"
				workspaceObj.Inference.Adapters = []kaitov1beta1.AdapterSpec{{
					Source:   &kaitov1beta1.DataSource{Name: "e2e-adapter", Image: testRegistryImage("e2e-adapter:noop")},
					Strength: &strength,
				}}
				createAndValidateWorkspace(workspaceObj)

				By("Verifying cache mutations are still injected alongside the adapter")
				validateStatefulSetConformance(workspaceObj, exp, cache.CacheConcernModelWeights)
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// t30: a Tuning workspace with a cache attachment is accepted by admission (cache
		// validation is orthogonal to Tuning). Uses a dry-run create so nothing is
		// persisted and no nodes are provisioned.
		Context(fmt.Sprintf("Provider %q: cache accepted on a Tuning workspace", provider), func() {
			It("should pass admission for a Tuning workspace with cache configured", func() {
				if !exp.ModelWeights.Supported {
					Skip(fmt.Sprintf("provider %q does not support the model weights concern", provider))
				}
				uniqueID := fmt.Sprint("cache-tune-", rand.Intn(10000))
				ws := generateCacheWorkspace(uniqueID, namespaceName, provider, cache.Attachment{
					ModelWeights: &cache.ConcernConfig{Provider: provider, Mode: cache.ModeOpportunistic},
				})
				// Convert to a Tuning workspace.
				ws.Inference = nil
				ws.Tuning = &kaitov1beta1.TuningSpec{
					Method: kaitov1beta1.TuningMethodLora,
					Input:  &kaitov1beta1.DataSource{Name: "e2e-tune-input", Image: testRegistryImage("e2e-tune:input")},
					Output: &kaitov1beta1.DataDestination{Image: testRegistryImage("e2e-tune:output"), ImagePushSecret: "acr-secret"},
				}

				By("Dry-run creating the Tuning+cache workspace (admission only)")
				err := utils.TestingCluster.KubeClient.Create(ctx, ws, client.DryRunAll)
				if err != nil {
					Expect(strings.ToLower(err.Error())).NotTo(ContainSubstring("cache"),
						"Tuning+cache was rejected for a cache-related reason: %v", err)
				}
			})
		})

		// Data-plane: warm cache hit validation. Creates a workspace twice —
		// first run warms the cache, second run validates cache hits.
		// Provider controls skip logic, workspace customization, and validation.
		Context(fmt.Sprintf("Provider %q: cache hit on warm reload", provider), func() {
			var workspaceObj *kaitov1beta1.Workspace

			It("should validate cache hits on second load", func() {
				if exp.CacheWarm == nil {
					Skip(fmt.Sprintf("provider %q does not support cache warm testing", provider))
				}
				cfg := exp.CacheWarm()
				if cfg.SkipReason != "" {
					Skip(fmt.Sprintf("provider %q: %s", provider, cfg.SkipReason))
				}

				// Use provider-specified namespace (where workload identity
				// federation is pre-configured) or fall back to the default
				// e2e test namespace.
				wsNamespace := namespaceName
				if cfg.Namespace != "" {
					wsNamespace = cfg.Namespace
				}

				h := newE2EHarnessInNamespace(provider, wsNamespace)
				uniqueID := fmt.Sprint("cache-hit-", rand.Intn(10000))

				buildCacheHitWorkspace := func(name string) *kaitov1beta1.Workspace {
					ws := generateCacheWorkspace(name, wsNamespace, provider, cache.Attachment{
						ModelWeights: &cache.ConcernConfig{
							Provider: provider,
							Mode:     cache.ModeOpportunistic,
						},
					})
					if cfg.WorkspaceCustomizer != nil {
						cfg.WorkspaceCustomizer(ws)
					}
					return ws
				}

				By("First run: warming the cache")
				workspaceObj = buildCacheHitWorkspace(uniqueID + "-warm")
				createAndValidateWorkspace(workspaceObj)

				By("Waiting for model to finish loading (InferenceReady=True)")
				Eventually(func() bool {
					_ = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, workspaceObj)
					for _, c := range workspaceObj.Status.Conditions {
						if c.Type == string(kaitov1beta1.WorkspaceConditionTypeInferenceStatus) && c.Status == metav1.ConditionTrue {
							return true
						}
					}
					return false
				}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Model did not finish loading on warm-up run")

				if cfg.ValidatePreWarm != nil {
					By("Validating pre-warm expectations")
					Expect(cfg.ValidatePreWarm(h, workspaceObj)).To(Succeed())
				}

				By("Deleting the warm-up workspace")
				cleanupWorkspace(workspaceObj)
				workspaceObj = nil

				By("Second run: expecting cache hits")
				workspaceObj = buildCacheHitWorkspace(uniqueID + "-hit")
				createAndValidateWorkspace(workspaceObj)

				By("Waiting for second model load to complete (InferenceReady=True)")
				Eventually(func() bool {
					_ = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
						Namespace: workspaceObj.Namespace, Name: workspaceObj.Name,
					}, workspaceObj)
					for _, c := range workspaceObj.Status.Conditions {
						if c.Type == string(kaitov1beta1.WorkspaceConditionTypeInferenceStatus) && c.Status == metav1.ConditionTrue {
							return true
						}
					}
					return false
				}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Second workspace did not finish loading")

				By("Validating post-warm expectations")
				Expect(cfg.ValidatePostWarm(h, workspaceObj)).To(Succeed())
			})

			AfterEach(func() {
				if workspaceObj != nil {
					cleanupWorkspace(workspaceObj)
					workspaceObj = nil
				}
			})
		})

		// Provider-declared e2e scenarios (provider-specific behaviour). Discovered
		// automatically from the expectations registry; capability-gated so data-plane
		// and disruptive scenarios only run when explicitly opted-in.
		for _, sc := range exp.E2EScenarios {
			sc := sc
			Context(fmt.Sprintf("Provider %q scenario", provider), func() {
				It(sc.Name, func() {
					h := newE2EHarness(provider)
					Expect(sc.Run(h)).To(Succeed())
				})
			})
		}
	}
})

// generateCacheWorkspace creates a workspace manifest with cache configuration
// using template inference to allow running on CPU nodes without GPU validation.
//
// The pod template is provider-neutral scaffolding: the provider's serving
// specifics (image, model args, backend env) are stamped on by its registered
// E2EWorkspace hook (see applyProviderE2EWorkspace), keeping this helper free of
// any single provider's details. A provider whose e2e prerequisites are unmet
// skips the running spec via that hook.
func generateCacheWorkspace(name, namespace string, provider cache.ProviderName, cacheSpec cache.Attachment) *kaitov1beta1.Workspace {
	configMapName := name + "-cache"
	attachmentData, err := yaml.Marshal(cacheSpec)
	Expect(err).NotTo(HaveOccurred())
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespace},
		Data:       map[string]string{cache.ConfigKey: string(attachmentData)},
	}
	err = utils.TestingCluster.KubeClient.Create(ctx, configMap)
	Expect(err == nil || apierrors.IsAlreadyExists(err)).To(BeTrue(),
		"failed to create cache attachment ConfigMap: %v", err)

	ws := &kaitov1beta1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				kaitov1beta1.AnnotationWorkspaceRuntime: string(model.RuntimeNameVLLM),
				cache.ConfigAnnotation:                  configMapName,
			},
		},
	}
	ws.Resource = kaitov1beta1.ResourceSpec{
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
						Name: "vllm",
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/health",
									Port: intstr.FromInt(8000),
								},
							},
							InitialDelaySeconds: 30,
							PeriodSeconds:       10,
							FailureThreshold:    120,
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("8"),
								corev1.ResourceMemory: resource.MustParse("110Gi"),
							},
						},
					},
				},
			},
		},
	}
	applyProviderE2EWorkspace(provider, ws)
	return ws
}

// applyProviderE2EWorkspace stamps the provider-specific pod template onto ws via
// the provider's registered E2EWorkspace hook, or skips the running spec when the
// provider's e2e prerequisites are unmet. A provider with no hook keeps the neutral
// skeleton unchanged, so provider onboarding requires no edits here.
func applyProviderE2EWorkspace(provider cache.ProviderName, ws *kaitov1beta1.Workspace) {
	exp, ok := cache.GetExpectations(provider)
	if !ok || exp.E2EWorkspace == nil {
		return
	}
	cfg := exp.E2EWorkspace()
	if cfg == nil {
		return
	}
	if cfg.SkipReason != "" {
		Skip(fmt.Sprintf("provider %q: %s", provider, cfg.SkipReason))
	}
	if cfg.Customizer != nil {
		cfg.Customizer(ws)
	}
}

// ensureCacheNamespace creates a namespace (idempotently) so a cache workspace can
// run there. Used by the multi-tenant test. The StorageIntercept config is
// auto-generated by the cache provider, so no ConfigMap seeding is required.
func ensureCacheNamespace(ns string) {
	_ = utils.TestingCluster.KubeClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	Eventually(func() error {
		return utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: ns}, &corev1.Namespace{})
	}, time.Minute, utils.PollInterval).Should(Succeed())
}

// generateCacheInferenceSet builds an InferenceSet whose template carries the given
// cache attachment, with a single replica to stay within the node budget.
func generateCacheInferenceSet(name, namespace string, provider cache.ProviderName, cacheSpec cache.Attachment) *kaitov1beta1.InferenceSet {
	replicas := int32(1)
	base := generateCacheWorkspace(name, namespace, provider, cacheSpec)
	return &kaitov1beta1.InferenceSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: kaitov1beta1.InferenceSetSpec{
			Replicas:       &replicas,
			NodeCountLimit: 1,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"apps": "vllm-cache"},
			},
			Template: kaitov1beta1.InferenceSetTemplate{
				Resource:  kaitov1beta1.InferenceSetResourceSpec{},
				Inference: *base.Inference,
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						cache.ConfigAnnotation: base.Annotations[cache.ConfigAnnotation],
					},
				},
			},
		},
	}
}

// validateStatefulSetConformance verifies that the workspace's StatefulSet matches
// the provider's declared expectations for the given cache concern, using the shared
// provider-agnostic assertion helper (cache.AssertPodSpec). This is the same contract
// enforced by the unit-level conformance suite, ensuring offline and online checks
// stay in lockstep.
func validateStatefulSetConformance(workspaceObj *kaitov1beta1.Workspace, exp cache.Expectations, concern cache.CacheConcern) {
	me := exp.ForConcern(concern)
	Eventually(func() string {
		sts := &appsv1.StatefulSet{}
		err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: workspaceObj.Namespace,
			Name:      workspaceObj.Name,
		}, sts)
		if err != nil {
			return fmt.Sprintf("StatefulSet not found yet: %v", err)
		}

		errs := cache.AssertPodSpec(sts.Spec.Template.Labels, &sts.Spec.Template.Spec, me)
		if len(errs) == 0 {
			return ""
		}
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		report := strings.Join(msgs, "; ")
		GinkgoWriter.Printf("[%s/%s] not conformant yet: %s\n", exp.Provider, concern, report)
		return report
	}, 15*time.Minute, utils.PollInterval).Should(BeEmpty(),
		fmt.Sprintf("StatefulSet should conform to provider %q expectations for concern %s", exp.Provider, concern))
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

// assertNoCacheMutations returns an error if any of the provider's declared cache
// mutations (label, env vars, volumes) are present on the pod template. Used to
// verify Disabled mode and post-update removal.
func assertNoCacheMutations(tmpl *corev1.PodTemplateSpec, me cache.MutationExpectation) error {
	for k := range me.RequiredLabels {
		if _, ok := tmpl.Labels[k]; ok {
			return fmt.Errorf("unexpected cache label %q present", k)
		}
	}
	if len(tmpl.Spec.Containers) > 0 {
		present := map[string]struct{}{}
		for _, e := range tmpl.Spec.Containers[0].Env {
			present[e.Name] = struct{}{}
		}
		for _, name := range me.RequiredEnvVars {
			if _, ok := present[name]; ok {
				return fmt.Errorf("unexpected cache env var %q present", name)
			}
		}
	}
	vols := map[string]struct{}{}
	for _, v := range tmpl.Spec.Volumes {
		vols[v.Name] = struct{}{}
	}
	for _, name := range me.RequiredVolumes {
		if _, ok := vols[name]; ok {
			return fmt.Errorf("unexpected cache volume %q present", name)
		}
	}
	return nil
}

// getWorkspaceCondition re-fetches the workspace and returns the named condition.
func getWorkspaceCondition(ws *kaitov1beta1.Workspace, condType string) (metav1.Condition, bool, error) {
	fresh := &kaitov1beta1.Workspace{}
	if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: ws.Namespace, Name: ws.Name}, fresh); err != nil {
		return metav1.Condition{}, false, err
	}
	c, found := lo.Find(fresh.Status.Conditions, func(c metav1.Condition) bool { return c.Type == condType })
	return c, found, nil
}

// updateCacheMode patches the workspace's model-weights attachment mode with conflict retry.
func updateCacheMode(ws *kaitov1beta1.Workspace, mode cache.Mode) {
	Eventually(func() error {
		configMapName := ws.Annotations[cache.ConfigAnnotation]
		if configMapName == "" {
			return fmt.Errorf("workspace has no %s annotation", cache.ConfigAnnotation)
		}
		configMap := &corev1.ConfigMap{}
		if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: ws.Namespace, Name: configMapName}, configMap); err != nil {
			return err
		}
		attachment := cache.Attachment{}
		if err := yaml.UnmarshalStrict([]byte(configMap.Data[cache.ConfigKey]), &attachment); err != nil {
			return fmt.Errorf("decoding cache attachment: %w", err)
		}
		if attachment.ModelWeights == nil {
			return fmt.Errorf("cache attachment has no modelWeights concern")
		}
		attachment.ModelWeights.Mode = mode
		data, err := yaml.Marshal(attachment)
		if err != nil {
			return fmt.Errorf("encoding cache attachment: %w", err)
		}
		configMap.Data[cache.ConfigKey] = string(data)
		if err := utils.TestingCluster.KubeClient.Update(ctx, configMap); err != nil {
			return err
		}
		return nil
	}, 2*time.Minute, utils.PollInterval).Should(Succeed(), "failed to update cache mode to %s", mode)
}

// e2eHarness implements cache.E2EHarness so provider-declared scenarios (which live
// in the provider package) can drive the cluster without importing the e2e utils.
type e2eHarness struct {
	provider  cache.ProviderName
	namespace string
}

func newE2EHarness(provider cache.ProviderName) cache.E2EHarness {
	return &e2eHarness{provider: provider, namespace: namespaceName}
}

func newE2EHarnessInNamespace(provider cache.ProviderName, ns string) cache.E2EHarness {
	return &e2eHarness{provider: provider, namespace: ns}
}

func (h *e2eHarness) Ctx() context.Context         { return ctx }
func (h *e2eHarness) Client() client.Client        { return utils.TestingCluster.KubeClient }
func (h *e2eHarness) Namespace() string            { return h.namespace }
func (h *e2eHarness) Provider() cache.ProviderName { return h.provider }
func (h *e2eHarness) Logf(format string, args ...any) {
	GinkgoWriter.Printf(format+"\n", args...)
}

func (h *e2eHarness) PodLogs(podName string) (string, error) {
	clientset, err := utils.GetK8sClientset()
	if err != nil {
		return "", fmt.Errorf("getting clientset: %w", err)
	}
	return utils.GetPodLogs(clientset, h.namespace, podName, "")
}

func (h *e2eHarness) NewCacheWorkspace(idPrefix string, spec cache.Attachment) *kaitov1beta1.Workspace {
	return generateCacheWorkspace(fmt.Sprintf("%s-%d", idPrefix, rand.Intn(100000)), h.namespace, h.provider, spec)
}

func (h *e2eHarness) Poll(timeout time.Duration, fn func() error) error {
	var lastErr error
	deadline := time.Now().Add(timeout)
	for {
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s: %w", timeout, lastErr)
		}
		time.Sleep(utils.PollInterval)
	}
}

// testRegistryImage returns a fully qualified image reference for e2e helper
// images (adapters, tuning) by prefixing them with a test container registry. The
// registry is read from TEST_CACHE_REGISTRY; for backward compatibility it falls
// back to deriving the registry from the DACS_CACHE_VLLM_IMAGE reference. When
// neither is set the bare image is returned (the provider's E2EWorkspace hook will
// have already skipped provider specs that need a registry).
func testRegistryImage(imageAndTag string) string {
	if registry := utils.GetEnv("TEST_CACHE_REGISTRY"); registry != "" {
		return strings.TrimSuffix(registry, "/") + "/" + imageAndTag
	}
	base := utils.GetEnv("DACS_CACHE_VLLM_IMAGE")
	if base == "" {
		return imageAndTag
	}
	// Extract registry from "registry/repo:tag" → "registry"
	idx := strings.LastIndex(base, "/")
	if idx < 0 {
		return imageAndTag
	}
	registry := base[:idx]
	return registry + "/" + imageAndTag
}
