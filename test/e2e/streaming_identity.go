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
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kaito-project/kaito/test/e2e/utils"
)

const streamingServiceAccountName = "kaito-model-streamer"

// streamingEnabled reports whether the CI provisioned streaming infra for this run.
func streamingEnabled() bool {
	return os.Getenv("STREAMING_ENABLED") == "true"
}

// streamingNamespaceFor returns the per-process test namespace for the given 1-based parallel
// process index, derived from a shared random base.
func streamingNamespaceFor(base string, processIndex int) string {
	return fmt.Sprintf("%s-%d", base, processIndex)
}

// setupAllStreamingIdentities creates a federated identity credential for every parallel process's
// namespace sequentially.
func setupAllStreamingIdentities(base string, parallelTotal int) {
	if !streamingEnabled() {
		return
	}
	identityName := os.Getenv("STREAMING_KUBELET_IDENTITY_NAME")
	identityRG := os.Getenv("STREAMING_KUBELET_IDENTITY_RG")
	oidcIssuer := os.Getenv("STREAMING_OIDC_ISSUER")
	Expect(identityName).NotTo(BeEmpty(), "STREAMING_KUBELET_IDENTITY_NAME must be set when STREAMING_ENABLED=true")
	Expect(identityRG).NotTo(BeEmpty(), "STREAMING_KUBELET_IDENTITY_RG must be set")
	Expect(oidcIssuer).NotTo(BeEmpty(), "STREAMING_OIDC_ISSUER must be set")

	for i := 1; i <= parallelTotal; i++ {
		namespace := streamingNamespaceFor(base, i)
		By(fmt.Sprintf("Creating streaming federated identity credential for namespace %s", namespace), func() {
			ficName := fmt.Sprintf("streaming-e2e-%s", namespace)
			subject := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, streamingServiceAccountName)
			// #nosec G204 -- inputs are CI-controlled env vars, not user input.
			cmd := exec.Command("az", "identity", "federated-credential", "create",
				"--name", ficName,
				"--identity-name", identityName,
				"--resource-group", identityRG,
				"--issuer", oidcIssuer,
				"--subject", subject,
				"--audiences", "api://AzureADTokenExchange",
				"-o", "none")
			out, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "az federated-credential create for %s failed: %s", namespace, string(out))
		})
	}
}

// createStreamingServiceAccount creates the model-streamer SA (annotated for workload identity) in
// the given namespace.
func createStreamingServiceAccount(namespace string) {
	if !streamingEnabled() {
		return
	}
	clientID := os.Getenv("STREAMING_KUBELET_CLIENT_ID")
	Expect(clientID).NotTo(BeEmpty(), "STREAMING_KUBELET_CLIENT_ID must be set when STREAMING_ENABLED=true")
	By(fmt.Sprintf("Creating streaming ServiceAccount %s in %s", streamingServiceAccountName, namespace), func() {
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      streamingServiceAccountName,
				Namespace: namespace,
				Annotations: map[string]string{
					"azure.workload.identity/client-id": clientID,
				},
			},
		}
		err := utils.TestingCluster.KubeClient.Create(context.TODO(), sa)
		Expect(err).NotTo(HaveOccurred(), "create streaming SA")
	})
}

// teardownAllStreamingIdentities deletes the federated credentials created for every parallel
// process's namespace.
func teardownAllStreamingIdentities(base string, parallelTotal int) {
	if !streamingEnabled() {
		return
	}
	identityName := os.Getenv("STREAMING_KUBELET_IDENTITY_NAME")
	identityRG := os.Getenv("STREAMING_KUBELET_IDENTITY_RG")
	for i := 1; i <= parallelTotal; i++ {
		namespace := streamingNamespaceFor(base, i)
		ficName := fmt.Sprintf("streaming-e2e-%s", namespace)
		// #nosec G204 -- inputs are CI-controlled env vars, not user input.
		cmd := exec.Command("az", "identity", "federated-credential", "delete",
			"--name", ficName,
			"--identity-name", identityName,
			"--resource-group", identityRG,
			"--yes", "-o", "none")
		out, err := cmd.CombinedOutput()
		if err != nil {
			GinkgoWriter.Printf("warning: failed to delete FIC %s: %s\n", ficName, string(out))
		}
	}
}
