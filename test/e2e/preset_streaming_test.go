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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
	models "github.com/kaito-project/kaito/presets/workspace/models"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

// Download-completion timeouts scale with model weight size.
const (
	defaultModelMirrorDownloadTimeout = 15 * time.Minute
	// largeModelMirrorDownloadTimeout covers large models added later.
	largeModelMirrorDownloadTimeout = 40 * time.Minute
	// largeModelMirrorSizeThreshold is the weight size above which the large timeout applies.
	largeModelMirrorSizeThreshold = "50Gi"
)

func modelMirrorModelFileSize(modelID string) string {
	m, err := models.GetModelByNameWithToken(ctx, modelID, "")
	if err != nil || m == nil {
		return ""
	}
	return m.GetInferenceParameters().TotalSafeTensorFileSize
}

// modelMirrorDownloadTimeout returns the Eventually ceiling for a model's download to complete,
// bucketed by weight size.
func modelMirrorDownloadTimeout(modelID string) time.Duration {
	sizeStr := modelMirrorModelFileSize(modelID)
	if sizeStr == "" {
		return defaultModelMirrorDownloadTimeout
	}
	size, err := resource.ParseQuantity(sizeStr)
	if err != nil {
		return defaultModelMirrorDownloadTimeout
	}
	if size.Cmp(resource.MustParse(largeModelMirrorSizeThreshold)) > 0 {
		return largeModelMirrorDownloadTimeout
	}
	return defaultModelMirrorDownloadTimeout
}

// validateModelMirrorCRReady asserts the cluster-scoped ModelMirror CR for modelID reaches
// Ready (with StorageReady) and exposes the expected modelPath + lastDownloadTime.
func validateModelMirrorCRReady(modelID string) {
	crName := modelstreaming.ModelMirrorCRName(modelID)
	By(fmt.Sprintf("Checking ModelMirror CR %s is Ready", crName), func() {
		Eventually(func() bool {
			mm := &kaitov1alpha1.ModelMirror{}
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: crName}, mm); err != nil {
				return false
			}
			if string(mm.Status.Phase) != "Ready" {
				return false
			}
			if mm.Status.ModelPath != "/models/"+modelID {
				return false
			}
			if mm.Status.LastDownloadTime == nil {
				return false
			}
			_, ready := lo.Find(mm.Status.Conditions, func(c metav1.Condition) bool {
				return c.Type == "Ready" && c.Status == metav1.ConditionTrue
			})
			_, storageReady := lo.Find(mm.Status.Conditions, func(c metav1.Condition) bool {
				return c.Type == "StorageReady" && c.Status == metav1.ConditionTrue
			})
			return ready && storageReady
		}, modelMirrorDownloadTimeout(modelID), utils.PollInterval).Should(BeTrue(), "ModelMirror CR did not reach Ready+StorageReady")
	})
}

// validateModelMirrorReady asserts the ModelMirror CR is Ready AND the workspace surfaces
// ModelMirrorReady=True. Use this for plain-Workspace streaming tests; for the InferenceSet
// case (no *Workspace handle) call validateModelMirrorCRReady directly.
func validateModelMirrorReady(workspaceObj *kaitov1beta1.Workspace, modelID string) {
	validateModelMirrorCRReady(modelID)
	By(fmt.Sprintf("Checking workspace %s has ModelMirrorReady=True", workspaceObj.Name), func() {
		Eventually(func() bool {
			ws := &kaitov1beta1.Workspace{}
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Name: workspaceObj.Name, Namespace: workspaceObj.Namespace}, ws); err != nil {
				return false
			}
			_, found := lo.Find(ws.Status.Conditions, func(c metav1.Condition) bool {
				return c.Type == string(kaitov1beta1.WorkspaceConditionTypeModelMirrorReady) &&
					c.Status == metav1.ConditionTrue
			})
			return found
		}, modelMirrorDownloadTimeout(modelID), utils.PollInterval).Should(BeTrue(), "workspace ModelMirrorReady condition not True")
	})
}

// validateModelMirrorResources asserts the PVC and download Job for the model exist in ns.
func validateModelMirrorResources(modelID, namespace string) {
	crName := modelstreaming.ModelMirrorCRName(modelID)
	By(fmt.Sprintf("Checking ModelMirror PVC %s in %s", crName, namespace), func() {
		Eventually(func() bool {
			pvc := &corev1.PersistentVolumeClaim{}
			if err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: crName, Namespace: namespace}, pvc); err != nil {
				return false
			}
			if pvc.Status.Phase != corev1.ClaimBound {
				return false
			}
			if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "blob-fuse" {
				return false
			}
			return lo.Contains(pvc.Finalizers, "kaito.sh/model-mirror-protection")
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "ModelMirror PVC not Bound with expected SC + finalizer")
	})

	By(fmt.Sprintf("Checking ModelMirror download Job for %s exists in %s", crName, namespace), func() {
		Eventually(func() bool {
			jobs := &batchv1.JobList{}
			if err := utils.TestingCluster.KubeClient.List(ctx, jobs,
				client.InNamespace(namespace),
				client.MatchingLabels{"kaito.sh/model-mirror-name": crName}); err != nil {
				return false
			}
			return len(jobs.Items) >= 1
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "no ModelMirror download Job found")
	})
}

// validateStreamingPodShape asserts the inference pod streams from az:// (workspace pod).
func validateStreamingPodShape(workspaceObj *kaitov1beta1.Workspace, modelID string, distributed bool) {
	By(fmt.Sprintf("Checking streaming pod shape for %s", workspaceObj.Name), func() {
		Eventually(func() error {
			return assertStreamingPod(workspaceObj.Namespace, workspaceObj.Name, modelID, distributed)
		}, 5*time.Minute, utils.PollInterval).Should(Succeed())
	})
}

// streamingPodName returns the pod to inspect for a workspace. For distributed (multi-node Ray)
// workspaces, only the leader pod (apps.kubernetes.io/pod-index=0) carries the `vllm serve`
// command, so we target it explicitly; otherwise the single workspace pod is used.
func streamingPodName(coreClient *kubernetes.Clientset, namespace, workspaceName string, distributed bool) (string, error) {
	if !distributed {
		return utils.GetPodNameForWorkspace(coreClient, namespace, workspaceName)
	}
	pods, err := coreClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kaito.sh/workspace=%s,%s=0", workspaceName, appsv1.PodIndexLabel),
	})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no leader pod (pod-index=0) found for workspace %s", workspaceName)
	}
	return pods.Items[0].Name, nil
}

// assertStreamingPod returns nil when the pod for workspaceName in namespace has streaming shape.
func assertStreamingPod(namespace, workspaceName, modelID string, distributed bool) error {
	coreClient, err := utils.GetK8sClientset()
	if err != nil {
		return err
	}
	podName, err := streamingPodName(coreClient, namespace, workspaceName, distributed)
	if err != nil {
		return err
	}
	pod, err := coreClient.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// Workload-identity label.
	if pod.Labels["azure.workload.identity/use"] != "true" {
		return fmt.Errorf("pod %s missing azure.workload.identity/use=true", podName)
	}
	if pod.Spec.ServiceAccountName != "kaito-model-streamer" {
		return fmt.Errorf("pod %s serviceAccountName=%q, want kaito-model-streamer", podName, pod.Spec.ServiceAccountName)
	}
	// Container command + env + volume checks.
	var c *corev1.Container
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == workspaceName {
			c = &pod.Spec.Containers[i]
			break
		}
	}
	if c == nil {
		return fmt.Errorf("inference container %q not found in pod %s", workspaceName, podName)
	}
	cmd := strings.Join(c.Command, " ") + " " + strings.Join(c.Args, " ")
	if !strings.Contains(cmd, "az://") {
		return fmt.Errorf("pod %s command missing az:// model path: %s", podName, cmd)
	}
	if !strings.Contains(cmd, "--load-format=runai_streamer") {
		return fmt.Errorf("pod %s command missing '--load-format=runai_streamer': %s", podName, cmd)
	}
	if !hasEnv(c.Env, "AZURE_STORAGE_ACCOUNT_NAME") {
		return fmt.Errorf("pod %s missing AZURE_STORAGE_ACCOUNT_NAME env", podName)
	}
	if envVal(c.Env, "KAITO_PROCESSOR") != modelID {
		return fmt.Errorf("pod %s KAITO_PROCESSOR=%q, want %q", podName, envVal(c.Env, "KAITO_PROCESSOR"), modelID)
	}
	for _, vm := range c.VolumeMounts {
		if vm.MountPath == "/workspace/weights" {
			return fmt.Errorf("pod %s unexpectedly mounts /workspace/weights (streaming should not)", podName)
		}
	}
	if distributed {
		if !strings.Contains(cmd, "tensor-parallel-size") {
			return fmt.Errorf("pod %s missing tensor-parallel-size (distributed)", podName)
		}
		if !strings.Contains(cmd, "distributed-executor-backend=ray") {
			return fmt.Errorf("pod %s missing distributed-executor-backend=ray (multi-node)", podName)
		}
		if !strings.Contains(cmd, `{"distributed": true}`) {
			return fmt.Errorf("pod %s missing distributed loader config", podName)
		}
	}
	return nil
}

func hasEnv(env []corev1.EnvVar, name string) bool {
	for _, e := range env {
		if e.Name == name && (e.Value != "" || e.ValueFrom != nil) {
			return true
		}
	}
	return false
}

func envVal(env []corev1.EnvVar, name string) string {
	for _, e := range env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}

// deleteStreamingModelMirrorCR deletes the cluster-scoped ModelMirror CR for modelID and waits
// for it to be gone. The CR is not owned by any workspace/InferenceSet (it is shared and
// cluster-scoped), so it must be cleaned up explicitly to avoid a leftover Ready CR false-passing
// a later run on a reused cluster.
func deleteStreamingModelMirrorCR(modelID string) {
	By("Deleting the cluster-scoped ModelMirror CR", func() {
		crName := modelstreaming.ModelMirrorCRName(modelID)
		mm := &kaitov1alpha1.ModelMirror{ObjectMeta: metav1.ObjectMeta{Name: crName}}
		delErr := utils.TestingCluster.KubeClient.Delete(ctx, mm)
		Expect(delErr == nil || apierrors.IsNotFound(delErr)).To(BeTrue())
		Eventually(func() bool {
			getErr := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{Name: crName}, &kaitov1alpha1.ModelMirror{})
			return apierrors.IsNotFound(getErr)
		}, 3*time.Minute, utils.PollInterval).Should(BeTrue())
	})
}

// cleanupStreamingResources deletes the workspace, then (on success) the cluster-scoped MM CR.
func cleanupStreamingResources(workspaceObj *kaitov1beta1.Workspace, modelID string) {
	cleanupResources(workspaceObj)
	if CurrentSpecReport().Failed() {
		return
	}
	deleteStreamingModelMirrorCR(modelID)
}
