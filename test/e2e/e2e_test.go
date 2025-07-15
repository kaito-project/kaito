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
	"flag"
	"fmt"
	"math/rand"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/test/e2e/utils"
)

var skipGPUProvisionerCheck = flag.Bool("skip-gpu-provisioner-check", false, "Skip checking for GPU provisioner pod in e2e tests")

var (
	ctx                 = context.Background()
	namespaceName       = fmt.Sprint(utils.E2eNamespace, rand.Intn(100))
	nodeProvisionerName = os.Getenv("TEST_SUITE")
)

var _ = BeforeSuite(func() {
	utils.GetClusterClient(utils.TestingCluster)

	namespaceName = fmt.Sprintf("%s-%d", namespaceName, GinkgoParallelProcess())
	GinkgoWriter.Printf("Namespace %q for e2e tests\n", namespaceName)

	kaitoNamespace := os.Getenv("KAITO_NAMESPACE")

	if nodeProvisionerName == "azkarpenter" {
		karpenterNamespace := os.Getenv("KARPENTER_NAMESPACE")
		//check karpenter deployment is up and running
		karpenterDeployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "karpenter",
				Namespace: karpenterNamespace,
			},
		}

		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: karpenterDeployment.Namespace,
				Name:      karpenterDeployment.Name,
			}, karpenterDeployment, &client.GetOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), "Failed to wait for	karpenter deployment")
	}

	// Only check GPU provisioner if the flag is not set
	if !*skipGPUProvisionerCheck &&
		nodeProvisionerName == "gpuprovisioner" {
		gpuName := os.Getenv("GPU_PROVISIONER_NAME")
		gpuNamespace := os.Getenv("GPU_PROVISIONER_NAMESPACE")
		//check gpu-provisioner deployment is up and running
		gpuProvisionerDeployment := &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gpuName,
				Namespace: gpuNamespace,
			},
		}

		Eventually(func() error {
			return utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Namespace: gpuProvisionerDeployment.Namespace,
				Name:      gpuProvisionerDeployment.Name,
			}, gpuProvisionerDeployment, &client.GetOptions{})
		}, utils.PollTimeout, utils.PollInterval).
			Should(Succeed(), fmt.Sprintf("Failed to wait for %s deployment", gpuName))
	}

	//check kaito-workspace deployment is up and running
	kaitoWorkspaceDeployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kaito-workspace",
			Namespace: kaitoNamespace,
		},
	}

	Eventually(func() error {
		return utils.TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: kaitoWorkspaceDeployment.Namespace,
			Name:      kaitoWorkspaceDeployment.Name,
		}, kaitoWorkspaceDeployment, &client.GetOptions{})
	}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to wait for	kaito-workspace deployment")

	// create testing namespace
	err := utils.TestingCluster.KubeClient.Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	})
	Expect(err).NotTo(HaveOccurred())

	loadTestEnvVars()

	err = copySecretToNamespace(aiModelsRegistrySecret, namespaceName)
	Expect(err).NotTo(HaveOccurred())

	err = copySecretToNamespace(e2eACRSecret, namespaceName)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	// delete testing namespace
	Eventually(func() error {
		return utils.TestingCluster.KubeClient.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}, &client.DeleteOptions{})
	}, utils.PollTimeout, utils.PollInterval).Should(Succeed(), "Failed to delete namespace for e2e")

})

func RunE2ETests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AI Toolchain Operator E2E Test Suite")
}

func TestE2E(t *testing.T) {
	RunE2ETests(t)
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
