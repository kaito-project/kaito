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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/test/e2e/utils"
)

var (
	testModelImage            = utils.GetEnv("AI_MODELS_REGISTRY") + "/e2e-test:0.0.1"
	testDataSourceConfig      = &kaitov1beta1.DataSource{Name: PresetFalcon7BModel, Image: testModelImage}
	testDataDestinationConfig = &kaitov1beta1.DataDestination{Image: testModelImage, ImagePushSecret: utils.GetEnv("AI_MODELS_REGISTRY_SECRET")}

	initialPresetSpec = &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: PresetFalcon7BModel}}
	updatedPresetSpec = &kaitov1beta1.PresetSpec{PresetMeta: kaitov1beta1.PresetMeta{Name: PresetFalcon40BModel}}

	initialTuningMethod     = kaitov1beta1.TuningMethodLora
	alternativeTuningMethod = kaitov1beta1.TuningMethodQLora
)

var _ = Describe("Workspace Validation Webhook", utils.GinkgoLabelFastCheck, func() {
	It("should validate the workspace resource spec at creation ", func() {
		workspaceObj := utils.GenerateInferenceWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_Bad",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, PresetFalcon7BModel, nil, nil, nil, "")

		By("Creating a workspace with invalid instancetype", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})
	})

	It("should validate the workspace inference spec at creation ", func() {
		workspaceObj := utils.GenerateInferenceWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, "invalid-name", nil, nil, nil, "")

		By("Creating a workspace with invalid preset name", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})
	})

	It("should validate the workspace inference adapters at creation", func() {
		By("Creating a vllm workspace with adapter strength", func() {
			workspaceObj := utils.GenerateInferenceWorkspaceManifestWithVLLM(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
				}, nil, PresetFalcon7BModel, nil, nil, testAdapters1, "")

			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})

		By("Creating a vllm workspace without adapter strength", func() {
			workspaceObj := utils.GenerateInferenceWorkspaceManifestWithVLLM(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
				}, nil, PresetPhi4MiniModel, nil, nil, phi4Adapter, "")

			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)
			// delete workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")
		})

		By("Creating a transformers workspace with adapter strength", func() {
			workspaceObj := utils.GenerateInferenceWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
				}, nil, PresetFalcon7BModel, nil, nil, testAdapters1, "")
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)

			// delete workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")
		})
	})

	It("should validate the workspace inference configfile", func() {
		By("Creating a 2gpu vllm workspace without configfile", func() {
			workspaceObj := utils.GenerateInferenceWorkspaceManifestWithVLLM(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC12s_v3",
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
				}, nil, PresetFalcon7BModel, nil, nil, nil, "")

			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})

		By("Creating a 2gpu transformers workspace without configfile", func() {
			workspaceObj := utils.GenerateInferenceWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC12s_v3",
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
				}, nil, PresetFalcon7BModel, nil, nil, nil, "")

			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)

			// delete workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")
		})

		By("Creating a 2gpu workspace with correct configfile", func() {
			workspaceObj := utils.GenerateInferenceWorkspaceManifestWithVLLM(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC12s_v3",
				&metav1.LabelSelector{
					MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
				}, nil, PresetFalcon7BModel, nil, nil, nil, "")

			cm := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "inference-config-webhook",
					Namespace: namespaceName,
				},
				Data: map[string]string{
					"inference_config.yaml": `
vllm:
  max-model-len: 1024
`,
				},
			}
			workspaceObj.Inference.Config = cm.Name

			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, &cm, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to create configmap %s", cm.Name)

			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)
			// delete workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
			}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")
		})
	})

	It("should validate the workspace tuning spec at creation ", func() {
		workspaceObj := utils.GenerateTuningWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, nil, testDataDestinationConfig, initialPresetSpec, initialTuningMethod)

		By("Creating a workspace with nil input", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})
	})

	It("should validate the workspace tuning spec at creation ", func() {
		workspaceObj := utils.GenerateTuningWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, testDataSourceConfig, nil, initialPresetSpec, initialTuningMethod)

		By("Creating a workspace with nil output", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})
	})

	It("should validate the workspace tuning spec at creation ", func() {
		workspaceObj := utils.GenerateTuningWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, testDataSourceConfig, testDataDestinationConfig, nil, initialTuningMethod)

		By("Creating a workspace with nil preset", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(HaveOccurred(), "Failed to create workspace %s", workspaceObj.Name)
		})
	})

	//TODO preset private mode
	//TODO custom template

	It("should validate the workspace resource spec at update ", func() {
		workspaceObj := utils.GenerateInferenceWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, PresetFalcon7BModel, nil, nil, nil, "")

		By("Creating a valid workspace", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)
		})

		By("Updating the label selector", func() {
			updatedObj := workspaceObj
			updatedObj.Resource.LabelSelector = &metav1.LabelSelector{}
			// update workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Update(ctx, updatedObj, &client.UpdateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to update workspace %s", updatedObj.Name)
		})

		By("Updating the InstanceType", func() {
			updatedObj := workspaceObj
			updatedObj.Resource.InstanceType = "Standard_NV6"
			// update workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Update(ctx, updatedObj, &client.UpdateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to update workspace %s", updatedObj.Name)
		})

		//TODO custom template

		// delete	workspace
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")

	})

	It("should validate the workspace tuning spec at update ", func() {
		workspaceObj := utils.GenerateTuningWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, testDataSourceConfig, testDataDestinationConfig, initialPresetSpec, initialTuningMethod)

		By("Creating a valid tuning workspace", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)
		})

		By("Updating the tuning preset", func() {
			updatedObj := workspaceObj
			updatedObj.Tuning.Preset = updatedPresetSpec
			// update workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Update(ctx, updatedObj, &client.UpdateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to update workspace %s", updatedObj.Name)
		})

		By("Updating the Method", func() {
			updatedObj := workspaceObj
			updatedObj.Tuning.Method = alternativeTuningMethod
			// update workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Update(ctx, updatedObj, &client.UpdateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to update workspace %s", updatedObj.Name)
		})

		// delete	workspace
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")

	})

	It("should validate the workspace inference spec at update ", func() {
		workspaceObj := utils.GenerateInferenceWorkspaceManifest(fmt.Sprint("webhook-", rand.Intn(1000)), namespaceName, "", 1, "Standard_NC6s_v3",
			&metav1.LabelSelector{
				MatchLabels: map[string]string{"kaito-workspace": "webhook-e2e-test"},
			}, nil, PresetFalcon7BModel, nil, nil, nil, "")

		By("Creating a valid workspace", func() {
			// Create workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Create(ctx, workspaceObj, &client.CreateOptions{})
			}, 20*time.Minute, utils.PollInterval).
				Should(Succeed(), "Failed to create workspace %s", workspaceObj.Name)
		})

		By("Updating the preset spec", func() {
			updatedObj := workspaceObj
			updatedObj.Inference.Preset.Name = PresetFalcon40BModel
			// update workspace
			Eventually(func() error {
				return utils.TestingCluster.KubeClient.Update(ctx, updatedObj, &client.UpdateOptions{})
			}, utils.PollTimeout, utils.PollInterval).
				Should(HaveOccurred(), "Failed to update workspace %s", updatedObj.Name)
		})

		//TODO custom template

		// delete	workspace
		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Delete(ctx, workspaceObj, &client.DeleteOptions{})
		}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete workspace")

	})

})
