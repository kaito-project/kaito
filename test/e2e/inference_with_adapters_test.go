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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/workspace/image"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var DefaultStrength = "1.0"

var imageName1 = "e2e-adapter"
var fullImageName1 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + imageName1 + ":0.0.1"
var imageName2 = "e2e-adapter2"
var fullImageName2 = utils.GetEnv("E2E_ACR_REGISTRY") + "/" + imageName2 + ":0.0.1"

var testAdapters1 = []kaitov1beta1.AdapterSpec{
	{
		Source: &kaitov1beta1.DataSource{
			Name:  imageName1,
			Image: fullImageName1,
			ImagePullSecrets: []string{
				utils.GetEnv("E2E_ACR_REGISTRY_SECRET"),
			},
		},
		Strength: &DefaultStrength,
	},
}

var testAdapters2 = []kaitov1beta1.AdapterSpec{
	{
		Source: &kaitov1beta1.DataSource{
			Name:  imageName2,
			Image: fullImageName2,
			ImagePullSecrets: []string{
				utils.GetEnv("E2E_ACR_REGISTRY_SECRET"),
			},
		},
		Strength: &DefaultStrength,
	},
}

var phi4AdapterName = "adapter-phi-3-mini-pycoder"
var phi4Adapter = []kaitov1beta1.AdapterSpec{
	{
		Source: &kaitov1beta1.DataSource{
			Name:  phi4AdapterName,
			Image: utils.GetEnv("E2E_ACR_REGISTRY") + "/" + phi4AdapterName + ":0.0.1",
			ImagePullSecrets: []string{
				utils.GetEnv("E2E_ACR_REGISTRY_SECRET"),
			},
		},
	},
}

var baseInitContainer = image.NewPullerContainer("", "")

func validateInitContainers(workspaceObj *kaitov1beta1.Workspace, expectedInitContainers []corev1.Container) {
	By("Checking the InitContainers", func() {
		Eventually(func() bool {
			var err error
			var initContainers []corev1.Container

			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workspaceObj.Name,
					Namespace: workspaceObj.Namespace,
				},
			}
			err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, sts)
			initContainers = sts.Spec.Template.Spec.InitContainers

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}

			for _, initContainer := range expectedInitContainers {
				found := false
				for _, gotContainer := range initContainers {
					if initContainer.Name == gotContainer.Name && initContainer.Image == gotContainer.Image {
						// Found a matching init container
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}

			return true
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for initContainers to be ready")
	})
}

func validateImagePullSecrets(workspaceObj *kaitov1beta1.Workspace, expectedImagePullSecrets []string) {
	By("Checking the ImagePullSecrets", func() {
		Eventually(func() bool {
			var err error

			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workspaceObj.Name,
					Namespace: workspaceObj.Namespace,
				},
			}
			err = utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, sts)

			if err != nil {
				GinkgoWriter.Printf("Error fetching resource: %v\n", err)
				return false
			}
			if sts.Spec.Template.Spec.ImagePullSecrets == nil {
				return false
			}

			return utils.CompareSecrets(sts.Spec.Template.Spec.ImagePullSecrets, expectedImagePullSecrets)
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for ImagePullSecrets to be ready")
	})
}

func validateAdapterAdded(workspaceObj *kaitov1beta1.Workspace, deploymentName string, adapterName string) {
	By("Checking the Adapters", func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				GinkgoWriter.Printf("Failed to create core client: %v\n", err)
				return false
			}

			namespace := workspaceObj.Namespace
			podName, err := utils.GetPodNameForWorkspace(coreClient, namespace, deploymentName)
			if err != nil {
				GinkgoWriter.Printf("Failed to get pod name for deployment %s: %v\n", deploymentName, err)
				return false
			}

			logs, err := utils.GetPodLogs(coreClient, namespace, podName, "")
			if err != nil {
				GinkgoWriter.Printf("Failed to get logs from pod %s: %v\n", podName, err)
				return false
			}

			searchStringAdapter := fmt.Sprintf("Adapter added: %s", adapterName)
			searchStringModelSuccess := "Model loaded successfully"

			return strings.Contains(logs, searchStringAdapter) && strings.Contains(logs, searchStringModelSuccess)
		}, 20*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for adapter resource to be ready")
	})
}

// createAdapterPVCWithData creates a PVC and populates it with adapter weights
// by running a pod that copies data from the given adapter image into the PVC.
// This follows the same pattern as createInputDatasetVolume() in preset_test.go.
func createAdapterPVCWithData(storageClassName string, adapterImage string, imagePullSecret string) string {
	coreClient, err := utils.GetK8sClientset()
	if err != nil {
		Fail(fmt.Sprintf("Failed to create core client: %v", err))
	}

	pvcName := fmt.Sprintf("adapter-pvc-%s", string(uuid.NewUUID())[:8])
	pvc, err := coreClient.CoreV1().PersistentVolumeClaims(namespaceName).Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespaceName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("500Mi"),
				},
			},
			StorageClassName: &storageClassName,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		Fail(fmt.Sprintf("Failed to create PVC: %v", err))
	}

	volumeName := "adapter-data"
	volumeMount := corev1.VolumeMount{
		Name:      volumeName,
		MountPath: "/mnt/adapter",
		ReadOnly:  false,
	}
	volume := corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvc.Name,
			},
		},
	}

	// Create a pod that copies adapter weights from the image to the PVC
	podName := fmt.Sprintf("populate-adapter-%s", string(uuid.NewUUID())[:8])
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespaceName,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			ImagePullSecrets: []corev1.LocalObjectReference{
				{Name: imagePullSecret},
			},
			Containers: []corev1.Container{
				{
					Name:    "copy-adapter",
					Image:   adapterImage,
					Command: []string{"/bin/sh", "-c", "cp -r /data/. /mnt/adapter/ && ls -la /mnt/adapter/"},
					VolumeMounts: []corev1.VolumeMount{
						volumeMount,
					},
				},
			},
			Volumes: []corev1.Volume{
				volume,
			},
		},
	}
	_, err = coreClient.CoreV1().Pods(namespaceName).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		Fail(fmt.Sprintf("Failed to create adapter population Pod: %v", err))
	}

	// Wait for the PVC to be bound
	Eventually(func() bool {
		pvc, err := coreClient.CoreV1().PersistentVolumeClaims(namespaceName).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			Fail(fmt.Sprintf("Failed to get PVC: %v", err))
		}
		return pvc.Status.Phase == corev1.ClaimBound
	}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "PVC is not bound")

	// Wait for the Pod to succeed (completed copying)
	Eventually(func() bool {
		pod, err := coreClient.CoreV1().Pods(namespaceName).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			Fail(fmt.Sprintf("Failed to get Pod: %v", err))
		}
		return pod.Status.Phase == corev1.PodSucceeded
	}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "Adapter population pod did not succeed")

	// Delete the pod to release the PVC
	err = coreClient.CoreV1().Pods(namespaceName).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		Fail(fmt.Sprintf("Failed to delete adapter population Pod: %v", err))
	}

	// Wait for the Pod to be deleted
	Eventually(func() bool {
		_, err := coreClient.CoreV1().Pods(namespaceName).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return true
			}
			Fail(fmt.Sprintf("Failed to get Pod: %v", err))
		}
		return false
	}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "Adapter population pod not deleted")

	return pvcName
}

func validateNoAdapterInitContainer(workspaceObj *kaitov1beta1.Workspace) {
	By("Checking that no adapter puller init container exists (volume adapter path)", func() {
		Eventually(func() bool {
			sts := &appsv1.StatefulSet{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, sts)
			if err != nil {
				GinkgoWriter.Printf("Error fetching StatefulSet: %v\n", err)
				return false
			}

			for _, ic := range sts.Spec.Template.Spec.InitContainers {
				// The puller init containers follow the pattern "puller-<adapterName>"
				if strings.HasPrefix(ic.Name, "puller-") {
					GinkgoWriter.Printf("Found unexpected adapter puller init container: %s\n", ic.Name)
					return false
				}
			}
			return true
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Volume-based adapter should NOT create puller init containers")
	})
}

func validatePVCMounted(workspaceObj *kaitov1beta1.Workspace, pvcName string) {
	By("Checking that the PVC is mounted in the StatefulSet", func() {
		Eventually(func() bool {
			sts := &appsv1.StatefulSet{}
			err := utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: workspaceObj.Namespace,
				Name:      workspaceObj.Name,
			}, sts)
			if err != nil {
				GinkgoWriter.Printf("Error fetching StatefulSet: %v\n", err)
				return false
			}

			// Check that a volume referencing the PVC exists
			pvcVolumeFound := false
			var pvcVolumeName string
			for _, vol := range sts.Spec.Template.Spec.Volumes {
				if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == pvcName {
					pvcVolumeFound = true
					pvcVolumeName = vol.Name
					break
				}
			}
			if !pvcVolumeFound {
				GinkgoWriter.Printf("PVC %s not found in StatefulSet volumes\n", pvcName)
				return false
			}

			// Check that the volume is mounted in the main container at /mnt/adapter/<name>
			for _, container := range sts.Spec.Template.Spec.Containers {
				for _, vm := range container.VolumeMounts {
					if vm.Name == pvcVolumeName && strings.HasPrefix(vm.MountPath, "/mnt/adapter/") {
						return true
					}
				}
			}
			GinkgoWriter.Printf("Volume %s not mounted in any container\n", pvcVolumeName)
			return false
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "PVC should be mounted in the StatefulSet")
	})
}

func validateAdapterLoadedInVLLM(workspaceObj *kaitov1beta1.Workspace, adapterName string) {
	deploymentName := workspaceObj.Name
	execOption := corev1.PodExecOptions{
		Command: []string{
			"bash",
			"-c",
			"apt-get update && apt-get install curl -y; curl -s 127.0.0.1:5000/v1/models | grep " + adapterName,
		},
		Container: deploymentName,
		Stdout:    true,
		Stderr:    true,
	}

	By("Checking the loaded Adapters", func() {
		Eventually(func() bool {
			coreClient, err := utils.GetK8sClientset()
			if err != nil {
				GinkgoWriter.Printf("Failed to create core client: %v\n", err)
				return false
			}

			namespace := workspaceObj.Namespace
			podName, err := utils.GetPodNameForWorkspace(coreClient, namespace, deploymentName)
			if err != nil {
				GinkgoWriter.Printf("Failed to get pod name for deployment %s: %v\n", deploymentName, err)
				return false
			}

			k8sConfig, err := utils.GetK8sConfig()
			if err != nil {
				GinkgoWriter.Printf("Failed to get k8s config: %v\n", err)
				return false
			}

			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			_, err = utils.ExecSync(ctx, k8sConfig, coreClient, namespace, podName, execOption)
			if err != nil {
				GinkgoWriter.Printf("validate command fails: %v\n", err)
				return false
			}
			return true
		}, 5*time.Minute, utils.PollInterval).Should(BeTrue(), "Failed to wait for adapter to be loaded")
	})
}

var _ = Describe("Workspace Preset", func() {
	BeforeEach(func() {
		loadTestEnvVars()
		loadModelVersions()
	})

	It(
		"should create a falcon workspace with adapter, and update the workspace with another adapter",
		utils.GinkgoLabelFastCheck,
		func() {
			numOfNode := 1
			workspaceObj := createCustomWorkspaceWithAdapter(numOfNode, testAdapters1)

			defer cleanupResources(workspaceObj)
			time.Sleep(30 * time.Second)

			utils.ValidateNodeClaimCreation(ctx, workspaceObj, numOfNode)
			validateResourceStatus(workspaceObj)

			time.Sleep(30 * time.Second)

			validateAssociatedService(workspaceObj)

			validateInferenceResource(workspaceObj, int32(numOfNode))

			validateWorkspaceReadiness(workspaceObj)

			validateRevision(workspaceObj, "1")

			var expectedInitContainers1 = []corev1.Container{
				{
					Name:  baseInitContainer.Name + "-" + imageName1,
					Image: baseInitContainer.Image,
				},
			}
			var expectedInitContainers2 = []corev1.Container{
				{
					Name:  baseInitContainer.Name + "-" + imageName2,
					Image: baseInitContainer.Image,
				},
			}

			validateInitContainers(workspaceObj, expectedInitContainers1)
			validateImagePullSecrets(workspaceObj, testAdapters1[0].Source.ImagePullSecrets)
			validateAdapterAdded(workspaceObj, workspaceObj.Name, imageName1)

			workspaceObj = updateCustomWorkspaceWithAdapter(workspaceObj, testAdapters2)
			validateResourceStatus(workspaceObj)

			time.Sleep(30 * time.Second)

			validateAssociatedService(workspaceObj)

			validateInferenceResource(workspaceObj, int32(numOfNode))

			validateWorkspaceReadiness(workspaceObj)

			validateRevision(workspaceObj, "2")
			validateInitContainers(workspaceObj, expectedInitContainers2)
			validateImagePullSecrets(workspaceObj, testAdapters2[0].Source.ImagePullSecrets)
			validateAdapterAdded(workspaceObj, workspaceObj.Name, imageName2)
		},
	)

})
