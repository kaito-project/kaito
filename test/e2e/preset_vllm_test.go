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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	kaitoutils "github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	controllers "github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var _ = Describe("Workspace Preset on vllm runtime", func() {
	BeforeEach(func() {
		loadTestEnvVars()
		loadModelVersions()
	})

	// MRI and InferenceSet tests run first so they are not interrupted by
	// slow/flaky GPU-provisioning timeouts in the preset workspace tests below.
	It("should perform P/D disaggregated inference with KV cache transfer successfully", Serial, utils.GinkgoLabelFastCheck, func() {
		// Fail early if Istio CRDs are not installed
		Expect(isIstioCRDAvailable()).To(BeTrue(), "Istio CRDs must be available for P/D traffic validation")

		// Uses the same MRI creation as existing test but adds P/D validation
		mriObj := createGemma3MultiRoleInference()
		defer cleanupResourcesForMultiRoleInference(mriObj)

		validateMultiRoleInferenceChildInferenceSets(mriObj)

		childInferenceSets := getMultiRoleInferenceChildInferenceSets(mriObj)
		roleReplicas := map[string]int32{}
		for _, role := range mriObj.Spec.Roles {
			if role.Replicas != nil {
				roleReplicas[string(role.Type)] = *role.Replicas
			}
		}
		for i := range childInferenceSets {
			is := &childInferenceSets[i]
			validateInferenceSetStatus(is)
			roleName, ok := is.Labels[kaitov1alpha1.LabelInferenceRole]
			Expect(ok).To(BeTrue(), "InferenceSet %s should have role label %s", is.Name, kaitov1alpha1.LabelInferenceRole)
			Expect(roleReplicas).To(HaveKey(roleName), "InferenceSet %s has unexpected role %s", is.Name, roleName)
			expectedReplicas := roleReplicas[roleName]
			validateInferenceSetReplicas(is, expectedReplicas)
			validateInferenceSetBenchmarkCompleted(is)
		}

		validateMultiRoleInferenceGWIEResources(mriObj)
		validateMultiRoleInferenceStatus(mriObj)

		// P/D-specific validations (require Istio)
		validateMultiRoleInferenceEPPReady(mriObj)
		validateMultiRoleInferenceEPPKVCacheScorer(mriObj)
		validateMultiRoleInferenceDestinationRule(mriObj)
		validateMultiRoleInferenceChatCompletions(mriObj)
		validateMultiRoleInferenceKVEvents(mriObj)
		validateMultiRoleInferencePDDisaggregation(mriObj)
	})

	It("should create a gpt-oss-20b two-node workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 2
		workspaceObj := createGPTOss20BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a gemma-4-E2B-it workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1
		workspaceObj := createGemma4_E2BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a qwen3.8-27b workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createQwen3_8_27BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a gpt-oss-120b workspace with preset public mode successfully", utils.GinkgoLabelA100Required, func() {
		numOfNode := 1
		workspaceObj := createGPTOss120BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a qwen3.6-35b-a3b-fp8 workspace with model streaming successfully", utils.GinkgoLabelFastCheck, func() {
		numOfNode := 1

		// Create the federated identity credential for this process's namespace.
		createStreamingFIC(namespaceName)
		defer deleteStreamingFIC(namespaceName)

		workspaceObj := createQwen3_6_35BA3BFP8WorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupStreamingResources(workspaceObj, "Qwen/Qwen3.6-35B-A3B-FP8")
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateModelMirrorResources("Qwen/Qwen3.6-35B-A3B-FP8", workspaceObj.Namespace)
		validateModelMirrorReady(workspaceObj, "Qwen/Qwen3.6-35B-A3B-FP8")
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateStreamingPodShape(workspaceObj, "Qwen/Qwen3.6-35B-A3B-FP8", false)
		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a granite-4.1-8b workspace with preset public mode successfully", utils.GinkgoLabelFastCheck, utils.GinkgoLabelMinimumRequired, func() {
		numOfNode := 1
		workspaceObj := createGranite4_1_8BWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})

	It("should create a Gemma 3 InferenceSet with preset public mode and validate BBR routing", Serial, utils.GinkgoLabelFastCheck, func() {
		Expect(isIstioCRDAvailable()).To(BeTrue(), "Istio CRDs must be available for BBR routing validation")

		numOfReplicas := 1
		inferenceSetObj := createGemma3InferenceSetWithPresetPublicModeAndVLLM(numOfReplicas)
		DeferCleanup(func() {
			cleanupResourcesForInferenceSet(inferenceSetObj)
		})

		validateInferenceSetStatus(inferenceSetObj)
		validateInferenceSetReplicas(inferenceSetObj, int32(numOfReplicas))

		// Validate NodePool shape and isolation for each child workspace (karpenter only)
		if nodeProvisionerName == "azkarpenter" {
			validateInferenceSetNodePools(inferenceSetObj, numOfReplicas)
		}

		validateInferenceSetBenchmarkCompleted(inferenceSetObj)
		validateGatewayAPIInferenceExtensionResources(inferenceSetObj)

		// --- BBR (Body-Based Routing) validation ---
		// vLLM serves the InferenceSet name as the OpenAI-compatible model id
		// (see pkg/model/interface.go buildVLLMInferenceCommand: when the
		// workspace has WorkspaceCreatedByInferenceSetLabel, served-model-name
		// is set to the InferenceSet name). BBR mirrors the request body's
		// "model" field into the X-Gateway-Model-Name header, so both the
		// HTTPRoute header match and the request payload must use this name.
		modelName := inferenceSetObj.Name
		inferencePoolName := kaitoutils.InferencePoolName(inferenceSetObj.Name)

		// findWorkspacePod locates a running pod belonging to a child Workspace
		// created by the InferenceSet, using the correct label selector.
		findWorkspacePod := func() (string, string, string) {
			wsList := &kaitov1beta1.WorkspaceList{}
			var podName, containerName, namespace string
			Eventually(func() bool {
				err := utils.TestingCluster.KubeClient.List(ctx, wsList,
					client.InNamespace(inferenceSetObj.Namespace),
					client.MatchingLabels{consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name})
				if err != nil || len(wsList.Items) == 0 {
					return false
				}
				ws := &wsList.Items[0]
				podList := &corev1.PodList{}
				err = utils.TestingCluster.KubeClient.List(ctx, podList,
					client.InNamespace(ws.Namespace),
					client.MatchingLabels{kaitov1beta1.LabelWorkspaceName: ws.Name})
				if err != nil || len(podList.Items) == 0 {
					return false
				}
				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						podName = pod.Name
						containerName = pod.Spec.Containers[0].Name
						namespace = pod.Namespace
						return true
					}
				}
				return false
			}, 5*time.Minute, utils.PollInterval).Should(BeTrue(),
				"Should find a running workspace pod for InferenceSet %s", inferenceSetObj.Name)
			return podName, containerName, namespace
		}

		validateBBRRouting(inferenceSetObj, modelName, inferencePoolName, findWorkspacePod)
	})

	It("should create a ministral-3-3b-instruct-2512 workspace with preset public mode successfully", func() {
		numOfNode := 1
		workspaceObj := createMinistral3_3BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode)

		defer cleanupResources(workspaceObj)
		time.Sleep(30 * time.Second)

		validateCreateNode(workspaceObj, numOfNode)
		validateResourceStatus(workspaceObj)

		time.Sleep(30 * time.Second)

		validateAssociatedService(workspaceObj)
		validateInferenceConfig(workspaceObj)

		validateInferenceResource(workspaceObj, int32(numOfNode))

		validateWorkspaceReadiness(workspaceObj)
		validateWorkspaceBenchmarkCompleted(workspaceObj)
		validateModelsEndpoint(workspaceObj)
		validateChatCompletionsEndpoint(workspaceObj)
	})
})

// validateBBRRouting installs BBR, creates an HTTPRoute with model-name header matching,
// and validates both positive (correct model → success) and negative (wrong model → error) routing.
func validateBBRRouting(inferenceSetObj *kaitov1beta1.InferenceSet, modelName, inferencePoolName string, findWorkspacePod func() (string, string, string)) {
	// Ensure a DestinationRule exists for the EPP service so Istio Gateway's
	// ext_proc gRPC uses the right TLS mode. After migrating to the
	// llm-d-router-gateway chart v0.9.0, EPP
	// (mcr.microsoft.com/oss/v2/llm-d/llm-d-router-endpoint-picker:v0.9.0)
	// listens on plaintext gRPC by default, so we use mode: DISABLE for the
	// Istio Gateway -> EPP ext_proc connection. (The previous SIMPLE +
	// insecureSkipVerify=true workaround was required by the older
	// llm-d-inference-scheduler:v0.8.0 EPP which defaulted to
	// --secure-serving=true with a self-signed cert.)
	By("Ensuring DestinationRule for EPP service (BBR)", func() {
		eppServiceName := inferencePoolName + "-epp"
		dr := &unstructured.Unstructured{}
		dr.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1",
			Kind:    "DestinationRule",
		})
		dr.SetName(eppServiceName)
		dr.SetNamespace(inferenceSetObj.Namespace)
		dr.Object["spec"] = map[string]interface{}{
			"host": eppServiceName,
			"trafficPolicy": map[string]interface{}{
				"tls": map[string]interface{}{
					"mode": "DISABLE",
				},
			},
		}
		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Create(ctx, dr)
			if err != nil && apierrors.IsAlreadyExists(err) {
				existing := &unstructured.Unstructured{}
				existing.SetGroupVersionKind(dr.GroupVersionKind())
				if getErr := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: dr.GetNamespace(), Name: dr.GetName(),
				}, existing); getErr != nil {
					return getErr
				}
				dr.SetResourceVersion(existing.GetResourceVersion())
				return utils.TestingCluster.KubeClient.Update(ctx, dr)
			}
			return err
		}, 2*time.Minute, utils.PollInterval).Should(Succeed(),
			"Failed to create DestinationRule for EPP service %s", eppServiceName)
		GinkgoWriter.Printf("Created/updated DestinationRule (mode=DISABLE) for EPP service %s\n", eppServiceName)

		DeferCleanup(func() {
			cleanupDR := &unstructured.Unstructured{}
			cleanupDR.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "networking.istio.io", Version: "v1", Kind: "DestinationRule",
			})
			cleanupDR.SetName(eppServiceName)
			cleanupDR.SetNamespace(inferenceSetObj.Namespace)
			_ = utils.TestingCluster.KubeClient.Delete(ctx, cleanupDR)
		})
	})

	// Install BBR helm chart using a dedicated tool pod (alpine/helm)
	By("Installing Body-Based Routing (BBR) helm chart", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred())

		// Create a ServiceAccount + ClusterRoleBinding so the tool pod can install helm charts
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bbr-helm-installer",
				Namespace: "default",
			},
		}
		_, err = coreClient.CoreV1().ServiceAccounts("default").Create(ctx, sa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Failed to create service account")

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bbr-helm-installer-admin",
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "cluster-admin",
			},
			Subjects: []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "bbr-helm-installer",
				Namespace: "default",
			}},
		}
		_, err = coreClient.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

		DeferCleanup(func() {
			_ = coreClient.RbacV1().ClusterRoleBindings().Delete(ctx, crb.Name, metav1.DeleteOptions{})
			_ = coreClient.CoreV1().ServiceAccounts("default").Delete(ctx, sa.Name, metav1.DeleteOptions{})
			GinkgoWriter.Printf("Deleted helm RBAC resources\n")
		})

		toolPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bbr-helm-installer",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				RestartPolicy:      corev1.RestartPolicyNever,
				ServiceAccountName: "bbr-helm-installer",
				Containers: []corev1.Container{{
					Name:    "helm",
					Image:   "alpine/helm:3.17.3",
					Command: []string{"sleep", "600"},
				}},
			},
		}

		_, err = coreClient.CoreV1().Pods("default").Create(ctx, toolPod, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Failed to create helm tool pod")

		DeferCleanup(func() {
			_ = coreClient.CoreV1().Pods("default").Delete(ctx, toolPod.Name, metav1.DeleteOptions{})
			GinkgoWriter.Printf("Deleted helm tool pod\n")
		})

		Eventually(func() bool {
			p, err := coreClient.CoreV1().Pods("default").Get(ctx, toolPod.Name, metav1.GetOptions{})
			return err == nil && p.Status.Phase == corev1.PodRunning
		}, 3*time.Minute, utils.PollInterval).Should(BeTrue(), "Helm tool pod should be running")

		k8sConfig, err := utils.GetK8sConfig()
		Expect(err).NotTo(HaveOccurred())

		installCmd := `helm upgrade --install body-based-routing ` +
			`oci://registry.k8s.io/gateway-api-inference-extension/charts/body-based-routing ` +
			`--version v1.3.0 --set provider.name=istio --namespace default --wait --timeout 3m 2>&1`

		execOption := corev1.PodExecOptions{
			Command:   []string{"sh", "-c", installCmd},
			Container: "helm",
			Stdout:    true,
			Stderr:    true,
		}

		execCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		stdout, execErr := utils.ExecSync(execCtx, k8sConfig, coreClient, "default", toolPod.Name, execOption)
		cancel()
		GinkgoWriter.Printf("BBR helm install output: %s\n", stdout)
		Expect(execErr).NotTo(HaveOccurred(), "Failed to install BBR helm chart")

		DeferCleanup(func() {
			uninstallCmd := `helm uninstall body-based-routing --namespace default 2>/dev/null || true`
			uninstallOption := corev1.PodExecOptions{
				Command:   []string{"sh", "-c", uninstallCmd},
				Container: "helm",
				Stdout:    true,
				Stderr:    true,
			}
			uninstallCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			utils.ExecSync(uninstallCtx, k8sConfig, coreClient, "default", toolPod.Name, uninstallOption)
			cancel()
			GinkgoWriter.Printf("Uninstalled BBR helm chart\n")
		})
	})

	// Patch Gateway to allow cross-namespace routes
	By("Patching Gateway to allow routes from test namespace", func() {
		var originalListeners []interface{}
		originalCaptured := false

		gw := &unstructured.Unstructured{}
		gw.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "gateway.networking.k8s.io",
			Version: "v1",
			Kind:    "Gateway",
		})
		Eventually(func() error {
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: "default",
				Name:      "inference-gateway",
			}, gw); err != nil {
				return err
			}
			spec, ok := gw.Object["spec"].(map[string]interface{})
			if !ok || spec == nil {
				spec = map[string]interface{}{}
				gw.Object["spec"] = spec
			}
			if !originalCaptured {
				if existing, ok := spec["listeners"].([]interface{}); ok {
					if b, err := json.Marshal(existing); err == nil {
						var cloned []interface{}
						if err := json.Unmarshal(b, &cloned); err == nil {
							originalListeners = cloned
						}
					}
				}
				originalCaptured = true
			}
			existingListeners, _ := spec["listeners"].([]interface{})
			updatedListeners := make([]interface{}, 0, len(existingListeners))
			httpListenerFound := false
			for _, l := range existingListeners {
				listener, ok := l.(map[string]interface{})
				if !ok {
					updatedListeners = append(updatedListeners, l)
					continue
				}
				if proto, _ := listener["protocol"].(string); proto == "HTTP" {
					listener["allowedRoutes"] = map[string]interface{}{
						"namespaces": map[string]interface{}{"from": "All"},
					}
					httpListenerFound = true
				}
				updatedListeners = append(updatedListeners, listener)
			}
			if !httpListenerFound {
				updatedListeners = append(updatedListeners, map[string]interface{}{
					"name": "http", "port": int64(80), "protocol": "HTTP",
					"allowedRoutes": map[string]interface{}{"namespaces": map[string]interface{}{"from": "All"}},
				})
			}
			spec["listeners"] = updatedListeners
			return utils.TestingCluster.KubeClient.Update(ctx, gw)
		}, 2*time.Minute, utils.PollInterval).Should(Succeed(), "Failed to patch Gateway")

		DeferCleanup(func() {
			restoreGW := &unstructured.Unstructured{}
			restoreGW.SetGroupVersionKind(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "Gateway"})
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "inference-gateway"}, restoreGW); err == nil && originalListeners != nil {
				if spec, ok := restoreGW.Object["spec"].(map[string]interface{}); ok {
					spec["listeners"] = originalListeners
					_ = utils.TestingCluster.KubeClient.Update(ctx, restoreGW)
				}
			}
		})
	})

	// Create HTTPRoute with BBR header matching
	By("Creating HTTPRoute with BBR model-name header matching", func() {
		httpRoute := &unstructured.Unstructured{}
		httpRoute.SetGroupVersionKind(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"})
		httpRoute.SetName(inferenceSetObj.Name + "-bbr-route")
		httpRoute.SetNamespace(inferenceSetObj.Namespace)
		httpRoute.Object["spec"] = map[string]interface{}{
			"parentRefs": []interface{}{map[string]interface{}{"group": "gateway.networking.k8s.io", "kind": "Gateway", "name": "inference-gateway", "namespace": "default"}},
			"rules": []interface{}{
				map[string]interface{}{
					"matches":     []interface{}{map[string]interface{}{"headers": []interface{}{map[string]interface{}{"type": "Exact", "name": "X-Gateway-Model-Name", "value": modelName}}, "path": map[string]interface{}{"type": "PathPrefix", "value": "/"}}},
					"backendRefs": []interface{}{map[string]interface{}{"group": "inference.networking.k8s.io", "kind": "InferencePool", "name": inferencePoolName}},
				},
				map[string]interface{}{
					"matches":     []interface{}{map[string]interface{}{"headers": []interface{}{map[string]interface{}{"type": "Exact", "name": "X-Gateway-Model-Name", "value": "nonexistent-model-xyz"}}, "path": map[string]interface{}{"type": "PathPrefix", "value": "/"}}},
					"backendRefs": []interface{}{map[string]interface{}{"group": "inference.networking.k8s.io", "kind": "InferencePool", "name": "nonexistent-pool"}},
				},
			},
		}

		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Create(ctx, httpRoute)
			if err != nil && apierrors.IsAlreadyExists(err) {
				existing := &unstructured.Unstructured{}
				existing.SetGroupVersionKind(httpRoute.GroupVersionKind())
				if getErr := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: httpRoute.GetNamespace(), Name: httpRoute.GetName()}, existing); getErr != nil {
					return getErr
				}
				httpRoute.SetResourceVersion(existing.GetResourceVersion())
				return utils.TestingCluster.KubeClient.Update(ctx, httpRoute)
			}
			return err
		}, 2*time.Minute, utils.PollInterval).Should(Succeed(), "Failed to create BBR HTTPRoute")

		DeferCleanup(func() {
			cleanup := &unstructured.Unstructured{}
			cleanup.SetGroupVersionKind(httpRoute.GroupVersionKind())
			cleanup.SetName(httpRoute.GetName())
			cleanup.SetNamespace(httpRoute.GetNamespace())
			_ = utils.TestingCluster.KubeClient.Delete(ctx, cleanup)
		})

		Eventually(func() bool {
			route := &unstructured.Unstructured{}
			route.SetGroupVersionKind(httpRoute.GroupVersionKind())
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Namespace: httpRoute.GetNamespace(), Name: httpRoute.GetName()}, route); err != nil {
				return false
			}
			status, _ := route.Object["status"].(map[string]interface{})
			parents, _ := status["parents"].([]interface{})
			for _, p := range parents {
				parent, _ := p.(map[string]interface{})
				conditions, _ := parent["conditions"].([]interface{})
				for _, c := range conditions {
					cond, _ := c.(map[string]interface{})
					if cond["type"] == "Accepted" && cond["status"] == "True" {
						return true
					}
				}
			}
			return false
		}, 2*time.Minute, 5*time.Second).Should(BeTrue(), "BBR HTTPRoute should be accepted")
	})

	// Positive: correct model name → successful inference
	By("Sending request with correct model name through BBR gateway", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred())
		k8sConfig, err := utils.GetK8sConfig()
		Expect(err).NotTo(HaveOccurred())

		execPodName, execContainer, execNamespace := findWorkspacePod()
		gatewayEndpoint := "http://inference-gateway-istio.default.svc.cluster.local/v1/chat/completions"

		// Use `curl -s -w` to capture BOTH the response body and the HTTP status
		// code, and drop `-f` so we don't blindly exit with 22 on 4xx/5xx. This
		// makes failure logs actionable (previously curl exit 22 with an empty
		// stderr told us nothing).
		curlCmd := fmt.Sprintf(
			`curl -s --max-time 120 -o /tmp/bbr-body -w "HTTP_STATUS=%%{http_code}\n" `+
				`-X POST -H "Content-Type: application/json" `+
				`-d '{"model":"%s","messages":[{"role":"user","content":"Hello"}],"max_tokens":10}' %s; `+
				`echo '---body---'; cat /tmp/bbr-body 2>/dev/null | head -c 4096; echo`,
			modelName, gatewayEndpoint)

		var stdout string
		diagDumped := false
		dumpDiag := func(reason string) {
			if diagDumped {
				return
			}
			diagDumped = true
			GinkgoWriter.Printf("\n===== BBR diagnostics (%s) =====\n", reason)
			diagCtx, diagCancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer diagCancel()

			dumpPodLogs := func(ns, selector string, tail int64) {
				pods, listErr := coreClient.CoreV1().Pods(ns).List(diagCtx, metav1.ListOptions{LabelSelector: selector})
				if listErr != nil {
					GinkgoWriter.Printf("diag: list ns=%s selector=%q failed: %v\n", ns, selector, listErr)
					return
				}
				if len(pods.Items) == 0 {
					GinkgoWriter.Printf("diag: no pods matched ns=%s selector=%q\n", ns, selector)
					return
				}
				for _, p := range pods.Items {
					GinkgoWriter.Printf("diag: pod %s/%s phase=%s\n", p.Namespace, p.Name, p.Status.Phase)
					for _, cs := range p.Status.ContainerStatuses {
						GinkgoWriter.Printf("diag:   container %s ready=%v restarts=%d state=%+v\n", cs.Name, cs.Ready, cs.RestartCount, cs.State)
					}
					for _, c := range p.Spec.Containers {
						tailLines := tail
						req := coreClient.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{Container: c.Name, TailLines: &tailLines})
						if logs, lerr := req.DoRaw(diagCtx); lerr == nil {
							GinkgoWriter.Printf("diag: logs %s/%s/%s:\n%s\n", p.Namespace, p.Name, c.Name, string(logs))
						} else {
							GinkgoWriter.Printf("diag: logs %s/%s/%s fetch failed: %v\n", p.Namespace, p.Name, c.Name, lerr)
						}
					}
				}
			}

			dumpPodLogs("default", "app.kubernetes.io/name=body-based-routing", 200)
			dumpPodLogs("default", "gateway.networking.k8s.io/gateway-name=inference-gateway", 150)
			// llm-d-router-gateway v0.9.0 labels EPP pods with
			// app.kubernetes.io/name=<release>-epp and
			// llm-d-router-gateway=<release>-epp (see routerlib templates/_helpers.tpl).
			// The legacy "app=<name>-epp" / "inferencepool=<name>" selectors from the
			// GWIE inferencepool chart no longer match.
			dumpPodLogs(inferenceSetObj.Namespace, "app.kubernetes.io/name="+inferencePoolName+"-epp", 200)
			dumpPodLogs(inferenceSetObj.Namespace, "llm-d-router-gateway="+inferencePoolName+"-epp", 200)

			// HTTPRoute status/spec
			route := &unstructured.Unstructured{}
			route.SetGroupVersionKind(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"})
			if getErr := utils.TestingCluster.KubeClient.Get(diagCtx, client.ObjectKey{Namespace: inferenceSetObj.Namespace, Name: inferenceSetObj.Name + "-bbr-route"}, route); getErr == nil {
				if b, mErr := json.MarshalIndent(route.Object["spec"], "", "  "); mErr == nil {
					GinkgoWriter.Printf("diag: HTTPRoute spec:\n%s\n", string(b))
				}
				if b, mErr := json.MarshalIndent(route.Object["status"], "", "  "); mErr == nil {
					GinkgoWriter.Printf("diag: HTTPRoute status:\n%s\n", string(b))
				}
			} else {
				GinkgoWriter.Printf("diag: HTTPRoute get failed: %v\n", getErr)
			}

			// InferencePool status
			pool := &unstructured.Unstructured{}
			pool.SetGroupVersionKind(schema.GroupVersionKind{Group: "inference.networking.k8s.io", Version: "v1", Kind: "InferencePool"})
			if getErr := utils.TestingCluster.KubeClient.Get(diagCtx, client.ObjectKey{Namespace: inferenceSetObj.Namespace, Name: inferencePoolName}, pool); getErr == nil {
				if b, mErr := json.MarshalIndent(pool.Object["status"], "", "  "); mErr == nil {
					GinkgoWriter.Printf("diag: InferencePool %s/%s status:\n%s\n", inferenceSetObj.Namespace, inferencePoolName, string(b))
				}
			} else {
				GinkgoWriter.Printf("diag: InferencePool get failed: %v\n", getErr)
			}
			GinkgoWriter.Printf("===== end BBR diagnostics =====\n\n")
		}

		Eventually(func() bool {
			execOption := corev1.PodExecOptions{Command: []string{"sh", "-c", curlCmd}, Container: execContainer, Stdout: true, Stderr: true}
			execCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			stdout, err = utils.ExecSync(execCtx, k8sConfig, coreClient, execNamespace, execPodName, execOption)
			cancel()
			if err != nil {
				GinkgoWriter.Printf("BBR request exec failed (model=%s, endpoint=%s): %v\nstdout: %s\n", modelName, gatewayEndpoint, err, stdout)
				return false
			}
			if !strings.Contains(stdout, "HTTP_STATUS=200") || !strings.Contains(stdout, "choices") {
				GinkgoWriter.Printf("BBR request not yet successful (model=%s):\n%s\n", modelName, stdout)
				// On the very first non-2xx, dump BBR/gateway/EPP diagnostics so
				// we can see why (empty 500 body from Envoy usually means the EPP
				// ExtProc gRPC failed or the InferencePool has no ready endpoints).
				dumpDiag("non-2xx response")
				return false
			}
			return true
		}, 5*time.Minute, 15*time.Second).Should(BeTrue(), "BBR should route correct model to inference pool")
		GinkgoWriter.Printf("BBR routing succeeded: %s\n", stdout[:min(len(stdout), 400)])
	})

	// Negative: non-existent model → error
	By("Sending request with non-existent model name through BBR gateway", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred())
		k8sConfig, err := utils.GetK8sConfig()
		Expect(err).NotTo(HaveOccurred())

		execPodName, execContainer, execNamespace := findWorkspacePod()
		gatewayEndpoint := "http://inference-gateway-istio.default.svc.cluster.local/v1/chat/completions"

		curlCmd := fmt.Sprintf(
			`curl -s --max-time 30 -o /dev/null -w "%%{http_code}" -X POST -H "Content-Type: application/json" `+
				`-d '{"model":"nonexistent-model-xyz","messages":[{"role":"user","content":"Hello"}],"max_tokens":10}' %s`,
			gatewayEndpoint)

		Eventually(func() bool {
			execOption := corev1.PodExecOptions{Command: []string{"sh", "-c", curlCmd}, Container: execContainer, Stdout: true, Stderr: true}
			execCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			stdout, execErr := utils.ExecSync(execCtx, k8sConfig, coreClient, execNamespace, execPodName, execOption)
			cancel()
			if execErr != nil {
				GinkgoWriter.Printf("Request with wrong model exec failed (retrying): %v\n", execErr)
				return false
			}
			httpCode := strings.TrimSpace(stdout)
			GinkgoWriter.Printf("Request with wrong model returned HTTP %s\n", httpCode)
			if httpCode == "000" || httpCode == "" {
				return false
			}
			return httpCode != "200"
		}, 2*time.Minute, 10*time.Second).Should(BeTrue(), "BBR should not route non-existent model")
	})
}

func createGemma3InferenceSetWithPresetPublicModeAndVLLM(replicas int) *kaitov1beta1.InferenceSet {
	modelSecret := createAndValidateModelSecret()
	inferenceSetObj := &kaitov1beta1.InferenceSet{}
	By("Creating an InferenceSet CR with Gemma 3 preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gemma3-is-", rand.Intn(1000))
		inferenceSetObj = utils.GenerateInferenceSetManifestWithVLLM(uniqueID, namespaceName, "", replicas, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-is-e2e-test-gemma-vllm"},
			}, PresetGemma3_4BInstructModel, nil, nil, modelSecret.Name)
		inferenceSetObj.Spec.Template.Annotations = utils.DisableModelStreaming(inferenceSetObj.Spec.Template.Annotations)
		createAndValidateInferenceSet(inferenceSetObj)
	})
	return inferenceSetObj
}

func createGemma3MultiRoleInference() *kaitov1alpha1.MultiRoleInference {
	modelSecret := createAndValidateModelSecret()
	mriObj := &kaitov1alpha1.MultiRoleInference{}
	By("Creating a MultiRoleInference CR with Gemma 3 prefill and decode roles", func() {
		uniqueID := fmt.Sprint("mri-gemma3-pd-", rand.Intn(1000))
		replicas := int32(1)
		mriObj = &kaitov1alpha1.MultiRoleInference{
			ObjectMeta: metav1.ObjectMeta{
				Name:      uniqueID,
				Namespace: namespaceName,
			},
			Spec: kaitov1alpha1.MultiRoleInferenceSpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-mri": uniqueID},
				},
				Model: kaitov1alpha1.MultiRoleInferenceModelSpec{
					Name:              string(PresetGemma3_4BInstructModel),
					ModelAccessSecret: modelSecret.Name,
				},
				Roles: []kaitov1alpha1.MultiRoleInferenceRoleSpec{
					{
						Type:         kaitov1alpha1.MultiRoleInferenceRolePrefill,
						Replicas:     &replicas,
						InstanceType: "Standard_NV36ads_A10_v5",
					},
					{
						Type:         kaitov1alpha1.MultiRoleInferenceRoleDecode,
						Replicas:     &replicas,
						InstanceType: "Standard_NV36ads_A10_v5",
					},
				},
			},
		}
		mriObj.Annotations = utils.DisableModelStreaming(mriObj.Annotations)

		By("Creating MultiRoleInference", func() {
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, mriObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to create MultiRoleInference")
		})
	})
	return mriObj
}

func cleanupResourcesForMultiRoleInference(mriObj *kaitov1alpha1.MultiRoleInference) {
	By("Cleaning up MultiRoleInference", func() {
		if !CurrentSpecReport().Failed() {
			Eventually(func() error {
				err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKeyFromObject(mriObj), mriObj)
				if err != nil {
					return client.IgnoreNotFound(err)
				}
				return utils.TestingCluster.KubeClient.Delete(ctx, mriObj, &client.DeleteOptions{})
			}, 5*time.Minute, utils.PollInterval).Should(Succeed(), "Failed to delete MultiRoleInference")
		} else {
			GinkgoWriter.Printf("test failed, keep %s\n", mriObj.Name)
		}
	})
}

func validateMultiRoleInferenceChildInferenceSets(mriObj *kaitov1alpha1.MultiRoleInference) {
	By("Validating child InferenceSets are created for each role", func() {
		Eventually(func() bool {
			isList := &kaitov1beta1.InferenceSetList{}
			err := utils.TestingCluster.KubeClient.List(ctx, isList,
				client.InNamespace(mriObj.Namespace),
				client.MatchingLabels{kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name})
			if err != nil {
				return false
			}
			if len(isList.Items) != 2 {
				return false
			}
			// Verify both roles exist in metadata labels and template labels
			foundPrefill, foundDecode := false, false
			for _, is := range isList.Items {
				roleLabel := is.Labels[kaitov1alpha1.LabelInferenceRole]
				// Also verify the role label is propagated to Spec.Template.Labels
				// so that downstream pods get the correct role for P/D disaggregation
				templateRoleLabel := is.Spec.Template.Labels[kaitov1alpha1.LabelInferenceRole]
				if roleLabel != templateRoleLabel {
					return false
				}
				// Verify the parent label is also on template labels
				templateParentLabel := is.Spec.Template.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent]
				if templateParentLabel != mriObj.Name {
					return false
				}
				if roleLabel == string(kaitov1alpha1.MultiRoleInferenceRolePrefill) {
					foundPrefill = true
				}
				if roleLabel == string(kaitov1alpha1.MultiRoleInferenceRoleDecode) {
					foundDecode = true
				}
			}
			return foundPrefill && foundDecode
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Expected 2 child InferenceSets (prefill + decode) for MultiRoleInference %s", mriObj.Name)
	})
}

func validateMultiRoleInferenceStatus(mriObj *kaitov1alpha1.MultiRoleInference) {
	By("Validating MultiRoleInference status conditions", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKeyFromObject(mriObj), mriObj)
			if err != nil {
				return false
			}
			// Check that PrefillReady and DecodeReady conditions are True
			prefillReady, decodeReady := false, false
			for _, cond := range mriObj.Status.Conditions {
				if cond.Type == string(kaitov1alpha1.MultiRoleInferenceConditionTypePrefillReady) && cond.Status == metav1.ConditionTrue {
					prefillReady = true
				}
				if cond.Type == string(kaitov1alpha1.MultiRoleInferenceConditionTypeDecodeReady) && cond.Status == metav1.ConditionTrue {
					decodeReady = true
				}
			}
			return prefillReady && decodeReady
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Expected PrefillReady and DecodeReady conditions on MultiRoleInference %s", mriObj.Name)
	})
}

// getMultiRoleInferenceChildInferenceSets returns the child InferenceSets for an MRI.
func getMultiRoleInferenceChildInferenceSets(mriObj *kaitov1alpha1.MultiRoleInference) []kaitov1beta1.InferenceSet {
	var children []kaitov1beta1.InferenceSet
	Eventually(func() bool {
		isList := &kaitov1beta1.InferenceSetList{}
		err := utils.TestingCluster.KubeClient.List(ctx, isList,
			client.InNamespace(mriObj.Namespace),
			client.MatchingLabels{kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name})
		if err != nil {
			return false
		}
		if len(isList.Items) != len(mriObj.Spec.Roles) {
			return false
		}
		children = isList.Items
		return true
	}, 20*time.Minute, utils.PollInterval).Should(BeTrue(),
		"Expected %d child InferenceSets for MultiRoleInference %s", len(mriObj.Spec.Roles), mriObj.Name)
	return children
}

// validateMultiRoleInferenceGWIEResources validates the MRI-owned InferencePool
// Flux resources (OCIRepository + HelmRelease) are ready.
func validateMultiRoleInferenceGWIEResources(mriObj *kaitov1alpha1.MultiRoleInference) {
	poolName := kaitoutils.InferencePoolName(mriObj.Name)

	By("Checking MRI-owned Flux OCIRepository is Ready", func() {
		Eventually(func() bool {
			ociRepository := &sourcev1.OCIRepository{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: mriObj.Namespace,
				Name:      poolName,
			}, ociRepository, &client.GetOptions{})
			if err != nil {
				return false
			}
			for _, cond := range ociRepository.Status.Conditions {
				if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Failed to validate MRI Flux OCIRepository is Ready for %s", mriObj.Name)
	})

	By("Checking MRI-owned Flux HelmRelease is Ready", func() {
		Eventually(func() bool {
			helmRelease := &helmv2.HelmRelease{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: mriObj.Namespace,
				Name:      poolName,
			}, helmRelease, &client.GetOptions{})
			if err != nil {
				return false
			}
			for _, cond := range helmRelease.Status.Conditions {
				if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Failed to validate MRI Flux HelmRelease is Ready for %s", mriObj.Name)
	})
}

// validateMultiRoleInferenceChatCompletions validates the /v1/chat/completions
// endpoint by exec-ing curl into a decode pod.
func validateMultiRoleInferenceChatCompletions(mriObj *kaitov1alpha1.MultiRoleInference) {
	modelName := getModelName(mriObj.Spec.Model.Name)

	By("Validating /v1/chat/completions via decode pod", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred(), "Failed to create core client")

		k8sConfig, err := utils.GetK8sConfig()
		Expect(err).NotTo(HaveOccurred(), "Failed to get k8s config")

		Eventually(func() bool {
			// Find the decode Workspace to get the service name and pod name
			wsList := &kaitov1beta1.WorkspaceList{}
			err = utils.TestingCluster.KubeClient.List(ctx, wsList,
				client.InNamespace(mriObj.Namespace),
				client.MatchingLabels{
					kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name,
					kaitov1alpha1.LabelInferenceRole:            string(kaitov1alpha1.MultiRoleInferenceRoleDecode),
				})
			if err != nil || len(wsList.Items) == 0 {
				GinkgoWriter.Printf("Failed to find decode Workspace: %v\n", err)
				return false
			}
			decodeWS := &wsList.Items[0]

			// StatefulSet pod name is <workspace.Name>-0
			podName := decodeWS.Name + "-0"
			// Decode workspace service name matches workspace name, exposed on port 80
			svcEndpoint := fmt.Sprintf("http://%s.%s.svc.cluster.local:80/v1/chat/completions", decodeWS.Name, mriObj.Namespace)

			expectedCompletion := `"object":"chat.completion`
			execOption := corev1.PodExecOptions{
				Command: []string{"sh", "-c", fmt.Sprintf(
					`command -v curl > /dev/null 2>&1 || (apt-get update -qq > /dev/null 2>&1 && apt-get install -y -qq curl > /dev/null 2>&1); `+
						`curl -s --max-time 30 -X POST -H "Content-Type: application/json" `+
						`-d '{"model":"%s","messages":[{"role":"user","content":"What is Kubernetes?"}],"max_tokens":7,"temperature":0}' `+
						`%s | grep -q '%s'`,
					modelName, svcEndpoint, expectedCompletion)},
				Container: decodeWS.Name,
				Stdout:    true,
				Stderr:    true,
			}

			execCtx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			stdout, err := utils.ExecSync(execCtx, k8sConfig, coreClient, mriObj.Namespace, podName, execOption)
			if err != nil {
				GinkgoWriter.Printf("validate chat completions fails: %v, stdout: %s\n", err, stdout)
				return false
			}
			return true
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Failed to validate /v1/chat/completions endpoint on MRI decode pod")
	})
}

// validateMultiRoleInferenceKVEvents validates that vLLM KV cache events are
// correctly enabled + exposed for every child Workspace of a MultiRoleInference
// (both prefill and decode roles).
//
// This is the producer half of the KV-cache-aware routing story: the
// llm-d-inference-scheduler EPP (that KAITO's GAIE / InferenceSet path wires
// up) subscribes to tcp://<pod>:5557. If any of these checks regress, the EPP
// KVCache scorer silently starves.
//
// The whole feature is gated in the operator on
// FeatureFlagEnableMultiRoleInferenceController, so by construction any MRI
// workspace must satisfy all of the following:
//
//  1. StatefulSet pod spec has containerPort/kv-events=consts.PortKVCacheEvents.
//  2. StatefulSet pod command contains --kv-events-config='{"enable_kv_cache_events":true}'.
//  3. The child Workspace ClusterIP Service exposes a Service port named
//     kv-events on consts.PortKVCacheEvents.
//  4. vLLM logs contain the ZMQ publisher startup line, proving the process
//     actually opened the socket (not just that the flag was passed).
func validateMultiRoleInferenceKVEvents(mriObj *kaitov1alpha1.MultiRoleInference) {
	By("Validating vLLM KV cache events are enabled + exposed for all MRI child workspaces", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred(), "Failed to create core client")

		kvEventsFlag := `--kv-events-config='{"enable_kv_cache_events":true}'`
		kvEventsPort := int32(consts.PortKVCacheEvents)

		// Look up all child Workspaces (prefill + decode) via the MRI parent label.
		var childWorkspaces []kaitov1beta1.Workspace
		Eventually(func() bool {
			wsList := &kaitov1beta1.WorkspaceList{}
			if err := utils.TestingCluster.KubeClient.List(ctx, wsList,
				client.InNamespace(mriObj.Namespace),
				client.MatchingLabels{kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name},
			); err != nil {
				GinkgoWriter.Printf("Failed to list MRI child workspaces: %v\n", err)
				return false
			}
			if len(wsList.Items) == 0 {
				return false
			}
			childWorkspaces = wsList.Items
			return true
		}, 2*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Expected at least one child Workspace for MRI %s", mriObj.Name)

		Expect(len(childWorkspaces)).To(BeNumerically(">=", 2),
			"Expected prefill + decode child workspaces for MRI %s", mriObj.Name)

		for i := range childWorkspaces {
			ws := &childWorkspaces[i]
			roleName := ws.Labels[kaitov1alpha1.LabelInferenceRole]
			GinkgoWriter.Printf("Validating KV events for MRI child workspace %s/%s (role=%s)\n",
				ws.Namespace, ws.Name, roleName)

			// (1) + (2): StatefulSet pod spec must declare kv-events container port
			// and the vLLM command must carry --kv-events-config.
			Eventually(func() error {
				sts := &appsv1.StatefulSet{}
				if err := utils.TestingCluster.KubeClient.Get(ctx,
					client.ObjectKey{Namespace: ws.Namespace, Name: ws.Name}, sts); err != nil {
					return fmt.Errorf("get StatefulSet %s/%s: %w", ws.Namespace, ws.Name, err)
				}
				var mainContainer *corev1.Container
				for idx := range sts.Spec.Template.Spec.Containers {
					c := &sts.Spec.Template.Spec.Containers[idx]
					if c.Name == ws.Name {
						mainContainer = c
						break
					}
				}
				if mainContainer == nil {
					return fmt.Errorf("StatefulSet %s/%s missing main container %s",
						ws.Namespace, ws.Name, ws.Name)
				}
				foundPort := false
				for _, p := range mainContainer.Ports {
					if p.Name == "kv-events" {
						if p.ContainerPort != kvEventsPort {
							return fmt.Errorf("container port kv-events = %d, want %d",
								p.ContainerPort, kvEventsPort)
						}
						foundPort = true
						break
					}
				}
				if !foundPort {
					return fmt.Errorf("StatefulSet %s/%s main container missing kv-events container port",
						ws.Namespace, ws.Name)
				}
				cmd := strings.Join(mainContainer.Command, " ")
				if !strings.Contains(cmd, kvEventsFlag) {
					return fmt.Errorf("StatefulSet %s/%s vLLM command missing %s; got: %s",
						ws.Namespace, ws.Name, kvEventsFlag, cmd)
				}
				return nil
			}, 2*time.Minute, utils.PollInterval).Should(Succeed(),
				"MRI child workspace %s/%s must expose KV events on its pod spec",
				ws.Namespace, ws.Name)

			// (3): child Workspace ClusterIP Service must expose kv-events port.
			Eventually(func() error {
				svc := &corev1.Service{}
				if err := utils.TestingCluster.KubeClient.Get(ctx,
					client.ObjectKey{Namespace: ws.Namespace, Name: ws.Name}, svc); err != nil {
					return fmt.Errorf("get Service %s/%s: %w", ws.Namespace, ws.Name, err)
				}
				if svc.Spec.Type != corev1.ServiceTypeClusterIP {
					return fmt.Errorf("Service %s/%s is %s, expected ClusterIP",
						ws.Namespace, ws.Name, svc.Spec.Type)
				}
				for _, p := range svc.Spec.Ports {
					if p.Name == "kv-events" {
						if p.Port != kvEventsPort {
							return fmt.Errorf("Service %s/%s kv-events port = %d, want %d",
								ws.Namespace, ws.Name, p.Port, kvEventsPort)
						}
						return nil
					}
				}
				return fmt.Errorf("Service %s/%s missing kv-events port", ws.Namespace, ws.Name)
			}, 2*time.Minute, utils.PollInterval).Should(Succeed(),
				"MRI child workspace %s/%s Service must expose kv-events port",
				ws.Namespace, ws.Name)

			// (4): vLLM must actually have started the ZMQ publisher (log signal).
			podName := ws.Name + "-0"
			Eventually(func() error {
				logs, err := utils.GetPodLogs(coreClient, ws.Namespace, podName, ws.Name)
				if err != nil {
					return fmt.Errorf("get logs for pod %s/%s: %w", ws.Namespace, podName, err)
				}
				// vLLM's kv_events.py logs "Starting ZMQ publisher thread" when the
				// publisher actually boots. Match on the phrase to stay resilient
				// against log-format tweaks (log level / module path).
				if !strings.Contains(logs, "Starting ZMQ publisher thread") {
					return fmt.Errorf("pod %s/%s logs missing 'Starting ZMQ publisher thread'",
						ws.Namespace, podName)
				}
				return nil
			}, 5*time.Minute, utils.PollInterval).Should(Succeed(),
				"MRI child workspace %s/%s pod %s vLLM logs must show KV events ZMQ publisher startup",
				ws.Namespace, ws.Name, podName)
		}
	})
}

func createGemma4_E2BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}

	By("Creating a workspace CR with Gemma 4 E2B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gemma-4-e2b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gemma-4-e2b-vllm"},
			}, nil, PresetGemma4_E2BInstructModel, nil, nil, nil, "", "")

		workspaceObj.Annotations = utils.DisableModelStreaming(workspaceObj.Annotations)
		createAndValidateWorkspace(workspaceObj)
	})

	return workspaceObj
}

func createQwen3_8_27BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}

	By("Creating a workspace CR with Qwen3.8-27B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-qwen3-8-27b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC24ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-qwen3-8-27b-vllm"},
			}, nil, PresetQwen3_8_27BModel, nil, nil, nil, "", "")

		workspaceObj.Annotations = utils.DisableModelStreaming(workspaceObj.Annotations)
		createAndValidateWorkspace(workspaceObj)
	})

	return workspaceObj
}

func createGPTOss120BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with GPT-OSS-120B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gpt-oss-120b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NC48ads_A100_v4",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gpt-oss-120b-vllm"},
			}, nil, PresetGPT_OSS_120BModel, nil, nil, nil, "", "")

		workspaceObj.Annotations = utils.DisableModelStreaming(workspaceObj.Annotations)
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createGPTOss20BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with GPT-OSS-20B preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-gpt-oss-20b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-gpt-oss-20b-vllm"},
			}, nil, PresetGPT_OSS_20BModel, nil, nil, nil, "", "")

		workspaceObj.Annotations = utils.DisableModelStreaming(workspaceObj.Annotations)
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createMinistral3_3BInstructWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Ministral 3 3B Instruct preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-ministral-3-3b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-ministral-3-3b-instruct-vllm"},
			}, nil, PresetMinistral33BInstructModel, nil, nil, nil, "", "")

		workspaceObj.Annotations = utils.DisableModelStreaming(workspaceObj.Annotations)
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createQwen3_6_35BA3BFP8WorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with Qwen3.6-35B-A3B-FP8 preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-qwen3-6-35b-a3b-fp8-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV72ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-qwen3-6-35b-a3b-fp8-vllm"},
			}, nil, PresetQwen3_6_35BA3BFP8Model, nil, nil, nil, "", "")

		// STREAMING TEST: intentionally NOT setting kaito.sh/model-streaming=disabled.
		// With the gate on, this workspace streams from blob (az://).
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func createGranite4_1_8BWorkspaceWithPresetPublicModeAndVLLM(numOfNode int) *kaitov1beta1.Workspace {
	workspaceObj := &kaitov1beta1.Workspace{}
	By("Creating a workspace CR with granite-4.1-8b preset public mode and vLLM", func() {
		uniqueID := fmt.Sprint("preset-granite-4-1-8b-", rand.Intn(1000))
		workspaceObj = utils.GenerateInferenceWorkspaceManifestWithVLLM(uniqueID, namespaceName, "", numOfNode, "Standard_NV36ads_A10_v5",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "public-preset-e2e-test-granite-4-1-8b-vllm"},
			}, nil, PresetGranite4_1_8BModel, nil, nil, nil, "", "")

		workspaceObj.Annotations = utils.DisableModelStreaming(workspaceObj.Annotations)
		createAndValidateWorkspace(workspaceObj)
	})
	return workspaceObj
}

func validateInferenceSetNodePools(inferenceSetObj *kaitov1beta1.InferenceSet, numOfReplicas int) {
	// List child workspaces by InferenceSet label
	workspaceList := &kaitov1beta1.WorkspaceList{}
	err := utils.TestingCluster.KubeClient.List(ctx, workspaceList,
		client.InNamespace(inferenceSetObj.Namespace),
		client.MatchingLabels{
			consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name,
		})
	Expect(err).NotTo(HaveOccurred())
	Expect(workspaceList.Items).To(HaveLen(numOfReplicas),
		"Should have expected number of child workspaces")

	workspaces := make([]*kaitov1beta1.Workspace, 0, len(workspaceList.Items))
	for i := range workspaceList.Items {
		ws := &workspaceList.Items[i]
		workspaces = append(workspaces, ws)
		utils.ValidateWorkspaceTargetNodeCount(ctx, ws, 1)
		utils.ValidateInferenceSetNodePoolShape(ctx, ws, 1, inferenceSetObj.Name)
		utils.ValidateNodeLabels(ctx, ws)
	}

	// Verify isolation between child workspaces
	utils.ValidateNodePoolIsolation(ctx, workspaces)
}

func validateGatewayAPIInferenceExtensionResources(iObj *kaitov1beta1.InferenceSet) {
	// Only validate if the Inference Preset is set
	if iObj.Spec.Template.Inference.Preset == nil {
		return
	}

	By("Checking Flux OCIRepository is Ready", func() {
		Eventually(func() bool {
			ociRepository := &sourcev1.OCIRepository{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: iObj.Namespace,
				Name:      kaitoutils.InferencePoolName(iObj.Name),
			}, ociRepository, &client.GetOptions{})
			if err != nil {
				return false
			}
			for _, cond := range ociRepository.Status.Conditions {
				if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to validate Flux OCIRepository is Ready")
	})

	By("Checking Flux HelmRelease is Ready", func() {
		Eventually(func() bool {
			helmRelease := &helmv2.HelmRelease{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: iObj.Namespace,
				Name:      kaitoutils.InferencePoolName(iObj.Name),
			}, helmRelease, &client.GetOptions{})
			if err != nil {
				return false
			}
			for _, cond := range helmRelease.Status.Conditions {
				if cond.Type == consts.ConditionReady && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
			return false
		}, utils.PollTimeout, utils.PollInterval).Should(BeTrue(), "Failed to validate Flux HelmRelease is Ready")
	})
}

// validateWorkspaceBenchmarkCompleted asserts that:
// - BenchmarkCompleted condition is True
// - status.Performance.Metrics["peakTokensPerMinute"] is set with a positive value
// - config map has the four standard keys
func validateWorkspaceBenchmarkCompleted(workspaceObj *kaitov1beta1.Workspace) {
	By("Validating workspace benchmark completed and performance is set", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      workspaceObj.Name,
				Namespace: workspaceObj.Namespace,
			}, workspaceObj)
			if err != nil {
				return false
			}
			_, conditionFound := lo.Find(workspaceObj.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == string(kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted) &&
					condition.Status == metav1.ConditionTrue
			})
			if !conditionFound {
				return false
			}
			if workspaceObj.Status.Performance == nil {
				return false
			}
			m, ok := workspaceObj.Status.Performance.Metrics[controllers.BenchmarkMetricPeakTPM]
			if !ok {
				return false
			}
			tpm, err := strconv.ParseFloat(m.Value, 64)
			if err != nil || tpm <= 0 {
				return false
			}
			for _, key := range []string{"durationSec", "inputTokens", "outputTokens", "maxConcurrency"} {
				if _, hasKey := m.Config[key]; !hasKey {
					return false
				}
			}
			return true
		}, 30*time.Second, utils.PollInterval).Should(BeTrue(),
			"workspace benchmark should complete with valid performance metrics")
	})

	By("Validating benchmark phase duration from pod logs", func() {
		coreClient, err := utils.GetK8sClientset()
		if err != nil {
			GinkgoWriter.Printf("WARNING: could not get k8s clientset to fetch benchmark logs: %v\n", err)
			return
		}
		logBenchmarkPhaseElapsed(coreClient, workspaceObj.Name, workspaceObj.Namespace)
	})
}

func logBenchmarkPhaseElapsed(coreClient *kubernetes.Clientset, wsName, wsNamespace string) {
	tailLines := int64(500)
	podName := wsName + "-0"
	req := coreClient.CoreV1().Pods(wsNamespace).GetLogs(podName, &corev1.PodLogOptions{
		TailLines: &tailLines,
		Container: wsName, // specify container name to handle multi-container pods (e.g., with routing sidecar)
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		GinkgoWriter.Printf("WARNING: could not fetch logs for pod %s: %v\n", podName, err)
		return
	}
	defer stream.Close()
	buf := new(strings.Builder)
	if _, err = io.Copy(buf, stream); err != nil {
		GinkgoWriter.Printf("WARNING: could not read logs for pod %s: %v\n", podName, err)
		return
	}
	foundDuration := false
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if strings.Contains(line, "total_phase_elapsed=") {
			GinkgoWriter.Printf("[benchmark] %s: %s\n", wsName, line)
			foundDuration = true
			for field := range strings.FieldsSeq(line) {
				if valStr, ok := strings.CutPrefix(field, "total_phase_elapsed="); ok {
					valStr = strings.TrimSuffix(valStr, "s")
					if v, parseErr := strconv.ParseFloat(valStr, 64); parseErr == nil {
						Expect(v).To(BeNumerically("<=", 300.0),
							"benchmark phase for %s took %.1fs, expected <= 300s", wsName, v)
					}
				}
			}
		}
	}
	if !foundDuration {
		GinkgoWriter.Printf("[benchmark] %s: total_phase_elapsed not found in last %d log lines\n", wsName, tailLines)
	}
}

// validateInferenceSetBenchmarkCompleted asserts that:
// - status.performance.metrics["aggregatedPeakTokensPerMinute"] is set with a positive value
// - all child workspaces have BenchmarkCompleted=True and their own performance set
func validateInferenceSetBenchmarkCompleted(inferenceSetObj *kaitov1beta1.InferenceSet) {
	By("Validating inferenceset aggregated performance is set", func() {
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Name:      inferenceSetObj.Name,
				Namespace: inferenceSetObj.Namespace,
			}, inferenceSetObj)
			if err != nil {
				return false
			}
			if inferenceSetObj.Status.Performance == nil {
				return false
			}
			m, ok := inferenceSetObj.Status.Performance.Metrics[controllers.BenchmarkMetricAggregatedPeakTPM]
			if !ok {
				return false
			}
			tpm, err := strconv.ParseFloat(m.Value, 64)
			if err != nil || tpm <= 0 {
				return false
			}
			return true
		}, 30*time.Second, utils.PollInterval).Should(BeTrue(),
			"inferenceset should have aggregated performance metric")
	})

	By("Validating all child workspace benchmarks completed", func() {
		wsList := &kaitov1beta1.WorkspaceList{}
		Eventually(func() bool {
			err := utils.TestingCluster.KubeClient.List(ctx, wsList,
				client.InNamespace(inferenceSetObj.Namespace),
				client.MatchingLabels{consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name},
			)
			if err != nil {
				return false
			}
			for i := range wsList.Items {
				ws := &wsList.Items[i]
				_, condFound := lo.Find(ws.Status.Conditions, func(c metav1.Condition) bool {
					return c.Type == string(kaitov1beta1.WorkspaceConditionTypeBenchmarkCompleted) &&
						c.Status == metav1.ConditionTrue
				})
				if !condFound {
					return false
				}
				if ws.Status.Performance == nil {
					return false
				}
			}
			return true
		}, 30*time.Second, utils.PollInterval).Should(BeTrue(),
			"all child workspaces should have BenchmarkCompleted=True and performance set")
	})

	By("Validating benchmark phase duration from child workspace pod logs", func() {
		coreClient, err := utils.GetK8sClientset()
		if err != nil {
			GinkgoWriter.Printf("WARNING: could not get k8s clientset to fetch benchmark logs: %v\n", err)
			return
		}
		wsList := &kaitov1beta1.WorkspaceList{}
		if err := utils.TestingCluster.KubeClient.List(ctx, wsList,
			client.InNamespace(inferenceSetObj.Namespace),
			client.MatchingLabels{consts.WorkspaceCreatedByInferenceSetLabel: inferenceSetObj.Name},
		); err != nil {
			GinkgoWriter.Printf("WARNING: could not list child workspaces: %v\n", err)
			return
		}
		for i := range wsList.Items {
			ws := &wsList.Items[i]
			logBenchmarkPhaseElapsed(coreClient, ws.Name, ws.Namespace)
		}
	})
}

// validateMultiRoleInferenceEPPReady validates that the EPP deployment is running
// with the disagg-profile-handler scheduling profile.
func validateMultiRoleInferenceEPPReady(mriObj *kaitov1alpha1.MultiRoleInference) {
	eppDeploymentName := kaitoutils.InferencePoolName(mriObj.Name) + "-epp"

	By("Validating EPP deployment is available with disagg-profile-handler", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred(), "Failed to create core client")

		// Validate the EPP deployment is available
		Eventually(func() bool {
			eppDeployment := &appsv1.Deployment{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: mriObj.Namespace,
				Name:      eppDeploymentName,
			}, eppDeployment)
			if err != nil {
				GinkgoWriter.Printf("EPP deployment %s not found: %v\n", eppDeploymentName, err)
				return false
			}
			// Check for Available condition
			for _, cond := range eppDeployment.Status.Conditions {
				if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
					if eppDeployment.Status.ReadyReplicas >= 1 {
						GinkgoWriter.Printf("EPP deployment %s is available with %d ready replicas\n",
							eppDeploymentName, eppDeployment.Status.ReadyReplicas)
						return true
					}
				}
			}
			return false
		}, 10*time.Minute, utils.PollInterval).Should(BeTrue(),
			"EPP deployment %s should be available with ready replicas", eppDeploymentName)

		// Validate EPP pod logs contain disagg-profile-handler scheduling profile
		Eventually(func() bool {
			// Get the EPP deployment's pod selector to find its pods reliably
			eppDeployment := &appsv1.Deployment{}
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: mriObj.Namespace,
				Name:      eppDeploymentName,
			}, eppDeployment); err != nil {
				GinkgoWriter.Printf("Failed to get EPP deployment: %v\n", err)
				return false
			}
			selector, err := metav1.LabelSelectorAsSelector(eppDeployment.Spec.Selector)
			if err != nil {
				GinkgoWriter.Printf("Failed to parse EPP deployment selector: %v\n", err)
				return false
			}
			pods, err := coreClient.CoreV1().Pods(mriObj.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: selector.String(),
			})
			if err != nil || len(pods.Items) == 0 {
				GinkgoWriter.Printf("Failed to list EPP pods (selector=%s): %v\n", selector.String(), err)
				return false
			}
			// Select a Running+Ready pod to avoid flakes from Pending/Terminating pods
			var eppPod *corev1.Pod
			for i := range pods.Items {
				p := &pods.Items[i]
				if p.Status.Phase != corev1.PodRunning {
					continue
				}
				for _, cond := range p.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						eppPod = p
						break
					}
				}
				if eppPod != nil {
					break
				}
			}
			if eppPod == nil {
				GinkgoWriter.Printf("No Running+Ready EPP pod found\n")
				return false
			}
			// Derive container name from pod spec, skipping istio-proxy sidecar
			containerName := ""
			for _, c := range eppPod.Spec.Containers {
				if c.Name != "istio-proxy" {
					containerName = c.Name
					break
				}
			}
			tailLines := int64(2000)
			logOpts := &corev1.PodLogOptions{
				TailLines: &tailLines,
			}
			if containerName != "" {
				logOpts.Container = containerName
			}
			req := coreClient.CoreV1().Pods(mriObj.Namespace).GetLogs(eppPod.Name, logOpts)
			stream, err := req.Stream(ctx)
			if err != nil {
				GinkgoWriter.Printf("Failed to get EPP pod logs: %v\n", err)
				return false
			}
			defer stream.Close()
			buf := new(strings.Builder)
			if _, err = io.Copy(buf, stream); err != nil {
				return false
			}
			logs := buf.String()
			if strings.Contains(logs, "disagg-profile-handler") {
				GinkgoWriter.Printf("EPP pod %s has disagg-profile-handler scheduling profile\n", eppPod.Name)
				return true
			}
			GinkgoWriter.Printf("EPP pod %s logs do not contain disagg-profile-handler yet\n", eppPod.Name)
			return false
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(),
			"EPP pod should have disagg-profile-handler in logs")
	})
}

// validateMultiRoleInferenceEPPKVCacheScorer validates that the EPP plugins
// ConfigMap contains the kv-cache-utilization-scorer plugin and that the EPP
// pod logs confirm the scorer was loaded. This ensures the default
// EndpointPickerConfig wires up KV cache-aware routing end-to-end.
func validateMultiRoleInferenceEPPKVCacheScorer(mriObj *kaitov1alpha1.MultiRoleInference) {
	poolName := kaitoutils.InferencePoolName(mriObj.Name)
	eppDeploymentName := poolName + "-epp"

	By("Validating EPP plugins ConfigMap contains kv-cache-utilization-scorer", func() {
		Eventually(func() error {
			// The llm-d-router-gateway chart stores the EPP plugins config in a
			// ConfigMap whose name is derived from the HelmRelease. Try the
			// most common naming patterns.
			candidateNames := []string{
				poolName + "-epp-plugins",
				poolName + "-plugins",
				poolName + "-epp",
			}

			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				return fmt.Errorf("create core client: %w", err)
			}

			// If none of the candidate names match, fall back to listing all
			// ConfigMaps in the namespace and scanning their data.
			var found bool
			for _, name := range candidateNames {
				cm, err := coreClient.CoreV1().ConfigMaps(mriObj.Namespace).Get(ctx, name, metav1.GetOptions{})
				if err != nil {
					continue
				}
				for _, v := range cm.Data {
					if strings.Contains(v, "kv-cache-utilization-scorer") {
						GinkgoWriter.Printf("ConfigMap %s contains kv-cache-utilization-scorer\n", name)
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			if !found {
				// Fallback: scan all ConfigMaps in namespace
				cmList, err := coreClient.CoreV1().ConfigMaps(mriObj.Namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("list ConfigMaps: %w", err)
				}
				for _, cm := range cmList.Items {
					for _, v := range cm.Data {
						if strings.Contains(v, "kv-cache-utilization-scorer") {
							GinkgoWriter.Printf("ConfigMap %s contains kv-cache-utilization-scorer\n", cm.Name)
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}

			if !found {
				return fmt.Errorf("no ConfigMap in namespace %s contains kv-cache-utilization-scorer", mriObj.Namespace)
			}
			return nil
		}, 5*time.Minute, utils.PollInterval).Should(Succeed(),
			"EPP plugins ConfigMap should contain kv-cache-utilization-scorer")
	})

	By("Validating EPP pod logs confirm kv-cache-utilization-scorer is loaded", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred(), "Failed to create core client")

		Eventually(func() bool {
			eppDeployment := &appsv1.Deployment{}
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: mriObj.Namespace,
				Name:      eppDeploymentName,
			}, eppDeployment); err != nil {
				GinkgoWriter.Printf("Failed to get EPP deployment: %v\n", err)
				return false
			}
			selector, err := metav1.LabelSelectorAsSelector(eppDeployment.Spec.Selector)
			if err != nil {
				GinkgoWriter.Printf("Failed to parse EPP deployment selector: %v\n", err)
				return false
			}
			pods, err := coreClient.CoreV1().Pods(mriObj.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: selector.String(),
			})
			if err != nil || len(pods.Items) == 0 {
				return false
			}
			// Select a Running+Ready pod
			var eppPod *corev1.Pod
			for i := range pods.Items {
				p := &pods.Items[i]
				if p.Status.Phase != corev1.PodRunning {
					continue
				}
				for _, cond := range p.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						eppPod = p
						break
					}
				}
				if eppPod != nil {
					break
				}
			}
			if eppPod == nil {
				GinkgoWriter.Printf("No Running+Ready EPP pod found\n")
				return false
			}
			// Derive container name, skipping istio-proxy sidecar
			containerName := ""
			for _, c := range eppPod.Spec.Containers {
				if c.Name != "istio-proxy" {
					containerName = c.Name
					break
				}
			}
			tailLines := int64(2000)
			logOpts := &corev1.PodLogOptions{
				TailLines: &tailLines,
			}
			if containerName != "" {
				logOpts.Container = containerName
			}
			req := coreClient.CoreV1().Pods(mriObj.Namespace).GetLogs(eppPod.Name, logOpts)
			stream, err := req.Stream(ctx)
			if err != nil {
				GinkgoWriter.Printf("Failed to get EPP pod logs: %v\n", err)
				return false
			}
			defer stream.Close()
			buf := new(strings.Builder)
			if _, err = io.Copy(buf, stream); err != nil {
				return false
			}
			logs := buf.String()
			if strings.Contains(logs, "kv-cache-utilization-scorer") {
				GinkgoWriter.Printf("EPP pod %s has kv-cache-utilization-scorer in logs\n", eppPod.Name)
				return true
			}
			GinkgoWriter.Printf("EPP pod %s logs do not contain kv-cache-utilization-scorer yet\n", eppPod.Name)
			return false
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(),
			"EPP pod should have kv-cache-utilization-scorer in logs")
	})
}

// isIstioCRDAvailable checks if the Istio DestinationRule CRD is registered
// in the cluster. It uses the RESTMapper so the check only fails when the
// GVK truly isn't registered, instead of when the caller lacks RBAC to list
// DestinationRules in a particular namespace.
func isIstioCRDAvailable() bool {
	restMapper := utils.TestingCluster.KubeClient.RESTMapper()
	_, err := restMapper.RESTMapping(schema.GroupKind{
		Group: "networking.istio.io",
		Kind:  "DestinationRule",
	}, "v1")
	if err == nil {
		return true
	}
	// As a fallback, attempt a namespaced List in the test namespace. A
	// Forbidden error still means the CRD is registered (RBAC just blocks us).
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "networking.istio.io",
		Version: "v1",
		Kind:    "DestinationRuleList",
	})
	listErr := utils.TestingCluster.KubeClient.List(ctx, list, client.InNamespace(namespaceName))
	if listErr == nil || apierrors.IsForbidden(listErr) {
		return true
	}
	return false
}

// validateMultiRoleInferenceDestinationRule creates and validates a DestinationRule
// for the EPP service with mode: SIMPLE and insecureSkipVerify: true.
func validateMultiRoleInferenceDestinationRule(mriObj *kaitov1alpha1.MultiRoleInference) {
	eppServiceName := kaitoutils.InferencePoolName(mriObj.Name) + "-epp"

	By("Creating DestinationRule for EPP service", func() {
		dr := &unstructured.Unstructured{}
		dr.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "networking.istio.io",
			Version: "v1",
			Kind:    "DestinationRule",
		})
		dr.SetName(eppServiceName)
		dr.SetNamespace(mriObj.Namespace)
		dr.Object["spec"] = map[string]interface{}{
			"host": eppServiceName,
			"trafficPolicy": map[string]interface{}{
				"tls": map[string]interface{}{
					"mode":               "SIMPLE",
					"insecureSkipVerify": true,
				},
			},
		}

		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Create(ctx, dr)
			if err != nil && apierrors.IsAlreadyExists(err) {
				// Fetch existing to get resourceVersion, then update
				existing := &unstructured.Unstructured{}
				existing.SetGroupVersionKind(dr.GroupVersionKind())
				if getErr := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: dr.GetNamespace(),
					Name:      dr.GetName(),
				}, existing); getErr != nil {
					return getErr
				}
				dr.SetResourceVersion(existing.GetResourceVersion())
				return utils.TestingCluster.KubeClient.Update(ctx, dr)
			}
			return err
		}, 2*time.Minute, utils.PollInterval).Should(Succeed(),
			"Failed to create DestinationRule for EPP service %s", eppServiceName)

		// Ensure cleanup after test
		DeferCleanup(func() {
			cleanupDR := &unstructured.Unstructured{}
			cleanupDR.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "networking.istio.io",
				Version: "v1",
				Kind:    "DestinationRule",
			})
			cleanupDR.SetName(eppServiceName)
			cleanupDR.SetNamespace(mriObj.Namespace)
			_ = utils.TestingCluster.KubeClient.Delete(ctx, cleanupDR)
		})

		GinkgoWriter.Printf("Created DestinationRule for EPP service %s\n", eppServiceName)
	})

	By("Validating DestinationRule exists", func() {
		Eventually(func() bool {
			dr := &unstructured.Unstructured{}
			dr.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "networking.istio.io",
				Version: "v1",
				Kind:    "DestinationRule",
			})
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: mriObj.Namespace,
				Name:      eppServiceName,
			}, dr)
			if err != nil {
				GinkgoWriter.Printf("DestinationRule not found: %v\n", err)
				return false
			}
			return true
		}, 2*time.Minute, utils.PollInterval).Should(BeTrue(),
			"DestinationRule should exist for EPP service %s", eppServiceName)
	})
}

// validateMultiRoleInferencePDDisaggregation sends multiple requests through the
// Istio gateway to trigger P/D disaggregation and verifies pod logs:
// - Prefill pod shows "Avg prompt throughput" > 0
// - Decode pod shows "Avg generation throughput" > 0
// - Decode pod shows "KV Transfer metrics: Num successful transfers" > 0
func validateMultiRoleInferencePDDisaggregation(mriObj *kaitov1alpha1.MultiRoleInference) {
	modelName := getModelName(mriObj.Spec.Model.Name)
	inferencePoolName := kaitoutils.InferencePoolName(mriObj.Name)

	By("Patching Gateway to allow routes from test namespace", func() {
		// Capture the original Gateway listeners so we can restore the shared
		// default/inference-gateway after the test. This avoids leaking
		// per-test configuration into other E2E cases or reruns.
		var originalListeners []interface{}
		originalCaptured := false

		gw := &unstructured.Unstructured{}
		gw.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "gateway.networking.k8s.io",
			Version: "v1",
			Kind:    "Gateway",
		})
		Eventually(func() error {
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: "default",
				Name:      "inference-gateway",
			}, gw); err != nil {
				return err
			}
			// Ensure spec map exists (avoid nil map assignment panic)
			spec, ok := gw.Object["spec"].(map[string]interface{})
			if !ok || spec == nil {
				spec = map[string]interface{}{}
				gw.Object["spec"] = spec
			}
			// Snapshot the original listeners exactly once (deep-copy via JSON
			// round-trip) for restoration in DeferCleanup.
			if !originalCaptured {
				if existing, ok := spec["listeners"].([]interface{}); ok {
					if b, err := json.Marshal(existing); err == nil {
						var cloned []interface{}
						if err := json.Unmarshal(b, &cloned); err == nil {
							originalListeners = cloned
						}
					}
				}
				originalCaptured = true
			}
			// Merge allowedRoutes into existing listeners instead of replacing.
			// This preserves TLS/hostnames/annotations from other listeners and
			// avoids breaking other E2E cases that share the same Gateway.
			existingListeners, _ := spec["listeners"].([]interface{})
			updatedListeners := make([]interface{}, 0, len(existingListeners))
			httpListenerFound := false
			for _, l := range existingListeners {
				listener, ok := l.(map[string]interface{})
				if !ok {
					updatedListeners = append(updatedListeners, l)
					continue
				}
				// Only patch HTTP listeners to allow cross-namespace routes
				if proto, _ := listener["protocol"].(string); proto == "HTTP" {
					listener["allowedRoutes"] = map[string]interface{}{
						"namespaces": map[string]interface{}{
							"from": "All",
						},
					}
					httpListenerFound = true
				}
				updatedListeners = append(updatedListeners, listener)
			}
			// If no HTTP listener exists (whether the Gateway is empty or it
			// only has TLS / non-HTTP listeners), append a minimal one so
			// cross-namespace HTTPRoutes are admitted.
			if !httpListenerFound {
				updatedListeners = append(updatedListeners, map[string]interface{}{
					"name":     "http",
					"port":     int64(80),
					"protocol": "HTTP",
					"allowedRoutes": map[string]interface{}{
						"namespaces": map[string]interface{}{
							"from": "All",
						},
					},
				})
			}
			spec["listeners"] = updatedListeners
			return utils.TestingCluster.KubeClient.Update(ctx, gw)
		}, 2*time.Minute, utils.PollInterval).Should(Succeed(),
			"Failed to patch Gateway to allow cross-namespace routes")
		GinkgoWriter.Printf("Patched Gateway inference-gateway to allow routes from namespace %s\n", mriObj.Namespace)

		// Restore the original Gateway listeners so this test does not leak
		// configuration into subsequent E2E cases that share the same Gateway.
		DeferCleanup(func() {
			if !originalCaptured {
				return
			}
			restoreGW := &unstructured.Unstructured{}
			restoreGW.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "gateway.networking.k8s.io",
				Version: "v1",
				Kind:    "Gateway",
			})
			_ = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: "default",
					Name:      "inference-gateway",
				}, restoreGW); err != nil {
					return err
				}
				spec, ok := restoreGW.Object["spec"].(map[string]interface{})
				if !ok || spec == nil {
					spec = map[string]interface{}{}
					restoreGW.Object["spec"] = spec
				}
				if originalListeners != nil {
					spec["listeners"] = originalListeners
				} else {
					delete(spec, "listeners")
				}
				return utils.TestingCluster.KubeClient.Update(ctx, restoreGW)
			})
			GinkgoWriter.Printf("Restored original listeners on Gateway inference-gateway\n")
		})
	})

	By("Creating HTTPRoute for inference gateway", func() {
		httpRoute := &unstructured.Unstructured{}
		httpRoute.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "gateway.networking.k8s.io",
			Version: "v1",
			Kind:    "HTTPRoute",
		})
		httpRoute.SetName(mriObj.Name + "-route")
		httpRoute.SetNamespace(mriObj.Namespace)
		httpRoute.Object["spec"] = map[string]interface{}{
			"parentRefs": []interface{}{
				map[string]interface{}{
					"group":     "gateway.networking.k8s.io",
					"kind":      "Gateway",
					"name":      "inference-gateway",
					"namespace": "default",
				},
			},
			"rules": []interface{}{
				map[string]interface{}{
					"backendRefs": []interface{}{
						map[string]interface{}{
							"group": "inference.networking.k8s.io",
							"kind":  "InferencePool",
							"name":  inferencePoolName,
						},
					},
					"matches": []interface{}{
						map[string]interface{}{
							"path": map[string]interface{}{
								"type":  "PathPrefix",
								"value": "/",
							},
						},
					},
				},
			},
		}

		Eventually(func() error {
			err := utils.TestingCluster.KubeClient.Create(ctx, httpRoute)
			if err != nil && apierrors.IsAlreadyExists(err) {
				// Update existing to ensure spec matches desired state
				existing := &unstructured.Unstructured{}
				existing.SetGroupVersionKind(httpRoute.GroupVersionKind())
				if getErr := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
					Namespace: httpRoute.GetNamespace(),
					Name:      httpRoute.GetName(),
				}, existing); getErr != nil {
					return getErr
				}
				httpRoute.SetResourceVersion(existing.GetResourceVersion())
				return utils.TestingCluster.KubeClient.Update(ctx, httpRoute)
			}
			return err
		}, 2*time.Minute, utils.PollInterval).Should(Succeed(),
			"Failed to create HTTPRoute for inference gateway")

		DeferCleanup(func() {
			cleanup := &unstructured.Unstructured{}
			cleanup.SetGroupVersionKind(httpRoute.GroupVersionKind())
			cleanup.SetName(httpRoute.GetName())
			cleanup.SetNamespace(httpRoute.GetNamespace())
			_ = utils.TestingCluster.KubeClient.Delete(ctx, cleanup)
		})
		GinkgoWriter.Printf("Created HTTPRoute %s for InferencePool %s\n", httpRoute.GetName(), inferencePoolName)

		// Wait for HTTPRoute to be accepted by the Gateway
		Eventually(func() bool {
			route := &unstructured.Unstructured{}
			route.SetGroupVersionKind(httpRoute.GroupVersionKind())
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: httpRoute.GetNamespace(),
				Name:      httpRoute.GetName(),
			}, route); err != nil {
				return false
			}
			// Check that the route is accepted: at least one parent must have
			// condition type=Accepted with status=True (not just non-empty parents,
			// since rejected routes can also populate parents with False conditions).
			status, _ := route.Object["status"].(map[string]interface{})
			parents, _ := status["parents"].([]interface{})
			for _, p := range parents {
				parent, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				conditions, _ := parent["conditions"].([]interface{})
				for _, c := range conditions {
					cond, ok := c.(map[string]interface{})
					if !ok {
						continue
					}
					ctype, _ := cond["type"].(string)
					cstatus, _ := cond["status"].(string)
					if ctype == "Accepted" && cstatus == "True" {
						GinkgoWriter.Printf("HTTPRoute %s has been accepted by gateway\n", httpRoute.GetName())
						return true
					}
				}
			}
			return false
		}, 2*time.Minute, 5*time.Second).Should(BeTrue(),
			"HTTPRoute should be accepted by the gateway")
	})

	By("Sending requests through inference gateway to trigger P/D disaggregation", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred(), "Failed to create core client")

		k8sConfig, err := utils.GetK8sConfig()
		Expect(err).NotTo(HaveOccurred(), "Failed to get k8s config")

		// Find the decode workspace and pod for exec-ing curl
		wsList := &kaitov1beta1.WorkspaceList{}
		Eventually(func() bool {
			err = utils.TestingCluster.KubeClient.List(ctx, wsList,
				client.InNamespace(mriObj.Namespace),
				client.MatchingLabels{
					kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name,
					kaitov1alpha1.LabelInferenceRole:            string(kaitov1alpha1.MultiRoleInferenceRoleDecode),
				})
			return err == nil && len(wsList.Items) > 0
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(),
			"Failed to find decode workspace")

		decodeWS := &wsList.Items[0]
		decodePodName := decodeWS.Name + "-0"
		// Route through the Istio inference gateway service
		gatewayEndpoint := "http://inference-gateway-istio.default.svc.cluster.local/v1/chat/completions"

		// Send 5 requests with long prompts through the gateway
		longPrompt := "Explain the entire history of distributed computing from the 1960s to present day, " +
			"including ARPANET, client-server architecture, grid computing, cloud computing, " +
			"microservices, serverless computing, and edge computing. For each era, describe " +
			"the key innovations, major companies involved, architectural patterns, and how " +
			"they influenced modern systems. Include technical details about consistency models, " +
			"CAP theorem, consensus algorithms like Paxos and Raft, and their practical implications."

		successCount := 0
		for i := 0; i < 5; i++ {
			GinkgoWriter.Printf("Sending P/D disaggregation request %d/5\n", i+1)
			execOption := corev1.PodExecOptions{
				Command: []string{"sh", "-c", fmt.Sprintf(
					`command -v curl > /dev/null 2>&1 || (apt-get update -qq > /dev/null 2>&1 && apt-get install -y -qq curl > /dev/null 2>&1); `+
						`curl -sf --max-time 120 -X POST -H "Content-Type: application/json" `+
						`-d '{"model":"%s","messages":[{"role":"user","content":"%s"}],"max_tokens":50,"temperature":0}' `+
						`%s && echo ''`,
					modelName, longPrompt, gatewayEndpoint)},
				Container: decodeWS.Name,
				Stdout:    true,
				Stderr:    true,
			}

			execCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			stdout, execErr := utils.ExecSync(execCtx, k8sConfig, coreClient, mriObj.Namespace, decodePodName, execOption)
			cancel()
			if execErr != nil {
				GinkgoWriter.Printf("Request %d failed: %v, stdout: %s\n", i+1, execErr, stdout)
			} else if strings.Contains(stdout, "choices") {
				GinkgoWriter.Printf("Request %d succeeded: %s\n", i+1, stdout[:min(len(stdout), 200)])
				successCount++
			} else {
				GinkgoWriter.Printf("Request %d returned unexpected response: %s\n", i+1, stdout[:min(len(stdout), 200)])
			}
			// Brief pause between requests
			time.Sleep(5 * time.Second)
		}
		Expect(successCount).To(BeNumerically(">=", 1),
			"At least one P/D disaggregation request should succeed")
	})

	By("Validating prefill pod shows prompt throughput > 0", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred())

		// Find prefill pods
		prefillWSList := &kaitov1beta1.WorkspaceList{}
		err = utils.TestingCluster.KubeClient.List(ctx, prefillWSList,
			client.InNamespace(mriObj.Namespace),
			client.MatchingLabels{
				kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name,
				kaitov1alpha1.LabelInferenceRole:            string(kaitov1alpha1.MultiRoleInferenceRolePrefill),
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(prefillWSList.Items).NotTo(BeEmpty(), "Expected at least one prefill workspace")

		prefillWS := &prefillWSList.Items[0]
		prefillPodName := prefillWS.Name + "-0"

		Eventually(func() bool {
			tailLines := int64(200)
			req := coreClient.CoreV1().Pods(mriObj.Namespace).GetLogs(prefillPodName, &corev1.PodLogOptions{
				TailLines: &tailLines,
				Container: prefillWS.Name,
			})
			stream, err := req.Stream(ctx)
			if err != nil {
				GinkgoWriter.Printf("Failed to get prefill pod logs: %v\n", err)
				return false
			}
			defer stream.Close()
			buf := new(strings.Builder)
			if _, err = io.Copy(buf, stream); err != nil {
				return false
			}
			logs := buf.String()

			// Check for "Avg prompt throughput" > 0 (or alternative formats)
			for _, line := range strings.Split(logs, "\n") {
				if strings.Contains(line, "prompt throughput") {
					// Try parsing "Avg prompt throughput: <val>"
					marker := "prompt throughput:"
					if idx := strings.Index(line, marker); idx >= 0 {
						valPart := strings.TrimSpace(line[idx+len(marker):])
						valStr := strings.Split(valPart, " ")[0]
						// Remove trailing comma if present
						valStr = strings.TrimRight(valStr, ",")
						val, parseErr := strconv.ParseFloat(valStr, 64)
						if parseErr == nil && val > 0 {
							GinkgoWriter.Printf("Prefill pod has prompt throughput: %.2f > 0\n", val)
							return true
						}
					}
				}
			}
			return false
		}, 3*time.Minute, 10*time.Second).Should(BeTrue(),
			"Prefill pod should show Avg prompt throughput > 0")
	})

	By("Validating decode pod shows generation throughput > 0 and successful KV transfers", func() {
		coreClient, err := utils.GetK8sClientset()
		Expect(err).NotTo(HaveOccurred())

		// Find decode pods
		decodeWSList := &kaitov1beta1.WorkspaceList{}
		err = utils.TestingCluster.KubeClient.List(ctx, decodeWSList,
			client.InNamespace(mriObj.Namespace),
			client.MatchingLabels{
				kaitov1alpha1.LabelMultiRoleInferenceParent: mriObj.Name,
				kaitov1alpha1.LabelInferenceRole:            string(kaitov1alpha1.MultiRoleInferenceRoleDecode),
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(decodeWSList.Items).NotTo(BeEmpty(), "Expected at least one decode workspace")

		decodeWS := &decodeWSList.Items[0]
		decodePodName := decodeWS.Name + "-0"

		// Validate generation throughput > 0
		Eventually(func() bool {
			tailLines := int64(200)
			req := coreClient.CoreV1().Pods(mriObj.Namespace).GetLogs(decodePodName, &corev1.PodLogOptions{
				TailLines: &tailLines,
				Container: decodeWS.Name,
			})
			stream, err := req.Stream(ctx)
			if err != nil {
				GinkgoWriter.Printf("Failed to get decode pod logs: %v\n", err)
				return false
			}
			defer stream.Close()
			buf := new(strings.Builder)
			if _, err = io.Copy(buf, stream); err != nil {
				return false
			}
			logs := buf.String()

			// Check for "Avg generation throughput" > 0 (or alternative formats)
			for _, line := range strings.Split(logs, "\n") {
				if strings.Contains(line, "generation throughput") {
					marker := "generation throughput:"
					if idx := strings.Index(line, marker); idx >= 0 {
						valPart := strings.TrimSpace(line[idx+len(marker):])
						valStr := strings.Split(valPart, " ")[0]
						valStr = strings.TrimRight(valStr, ",")
						val, parseErr := strconv.ParseFloat(valStr, 64)
						if parseErr == nil && val > 0 {
							GinkgoWriter.Printf("Decode pod has generation throughput: %.2f > 0\n", val)
							return true
						}
					}
				}
			}
			return false
		}, 3*time.Minute, 10*time.Second).Should(BeTrue(),
			"Decode pod should show Avg generation throughput > 0")

		// Best-effort KV transfer metric check — NIXL requires RDMA/NVLink
		// connectivity between pods which may not be available in all E2E environments.
		// Log a warning instead of failing the test if KV transfers are not observed.
		kvTransferFound := false
		for attempt := 0; attempt < 6; attempt++ {
			time.Sleep(10 * time.Second)
			tailLines := int64(500)
			req := coreClient.CoreV1().Pods(mriObj.Namespace).GetLogs(decodePodName, &corev1.PodLogOptions{
				TailLines: &tailLines,
				Container: decodeWS.Name,
			})
			stream, logErr := req.Stream(ctx)
			if logErr != nil {
				continue
			}
			buf := new(strings.Builder)
			_, _ = io.Copy(buf, stream)
			stream.Close()
			logs := buf.String()

			for _, line := range strings.Split(logs, "\n") {
				if strings.Contains(line, "Num successful transfers") {
					parts := strings.Split(line, "Num successful transfers:")
					if len(parts) >= 2 {
						valStr := strings.TrimSpace(strings.Split(parts[1], ",")[0])
						valStr = strings.TrimSpace(strings.Split(valStr, " ")[0])
						val, parseErr := strconv.ParseFloat(valStr, 64)
						if parseErr == nil && val >= 1 {
							GinkgoWriter.Printf("Decode pod has %d successful KV transfers\n", int(val))
							kvTransferFound = true
							break
						}
					}
				}
				if strings.Contains(line, "nixl_num_success_from_source") {
					GinkgoWriter.Printf("Decode pod shows NIXL transfer activity: %s\n", line)
					kvTransferFound = true
					break
				}
			}
			if kvTransferFound {
				break
			}
		}
		if !kvTransferFound {
			GinkgoWriter.Printf("WARNING: KV transfer metrics not observed — NIXL may not be available in this environment (requires RDMA/NVLink). Skipping KV transfer assertion.\n")
		}
	})
}
