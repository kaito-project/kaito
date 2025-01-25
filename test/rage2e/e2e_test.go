// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/test/e2e/utils"
)

var (
	ctx           = context.Background()
	namespaceName = fmt.Sprint(utils.E2eNamespace, rand.Intn(100))
)

var _ = BeforeSuite(func() {
	utils.GetClusterClient(utils.TestingCluster)

	// Create testing namespace for pipeline
	namespaceName = fmt.Sprintf("%s-%d", namespaceName, GinkgoParallelProcess())
	GinkgoWriter.Printf("Namespace %q for e2e tests\n", namespaceName)

	// Create testing namespace
	err := utils.TestingCluster.KubeClient.Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	// Delete testing namespace
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
	RunSpecs(t, "AI Toolchain Operator Pipeline E2E Test Suite")
}

func TestE2E(t *testing.T) {
	RunE2ETests(t)
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
