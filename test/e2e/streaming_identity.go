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
	"strings"
	"time"

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

// streamingFICName returns the federated identity credential name for a namespace.
func streamingFICName(namespace string) string {
	return fmt.Sprintf("streaming-e2e-%s", namespace)
}

// createStreamingFIC creates a federated identity credential whose subject targets the streaming
// ServiceAccount in the given namespace.
func createStreamingFIC(namespace string) {
	if !streamingEnabled() {
		return
	}
	identityName := os.Getenv("STREAMING_KUBELET_IDENTITY_NAME")
	identityRG := os.Getenv("STREAMING_KUBELET_IDENTITY_RG")
	oidcIssuer := os.Getenv("STREAMING_OIDC_ISSUER")
	Expect(identityName).NotTo(BeEmpty(), "STREAMING_KUBELET_IDENTITY_NAME must be set when STREAMING_ENABLED=true")
	Expect(identityRG).NotTo(BeEmpty(), "STREAMING_KUBELET_IDENTITY_RG must be set")
	Expect(oidcIssuer).NotTo(BeEmpty(), "STREAMING_OIDC_ISSUER must be set")

	ficName := streamingFICName(namespace)
	subject := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, streamingServiceAccountName)
	By(fmt.Sprintf("Creating streaming federated identity credential for namespace %s", namespace), func() {
		Eventually(func() error {
			cmd := exec.Command("az", "identity", "federated-credential", "create",
				"--name", ficName,
				"--identity-name", identityName,
				"--resource-group", identityRG,
				"--issuer", oidcIssuer,
				"--subject", subject,
				"--audiences", "api://AzureADTokenExchange",
				"-o", "none")
			out, err := cmd.CombinedOutput()
			if err == nil {
				return nil
			}
			// A FIC left over from a previous attempt (same name+subject) is fine — treat as success.
			if strings.Contains(string(out), "already exists") {
				return nil
			}
			return fmt.Errorf("az federated-credential create for %s failed: %s", namespace, string(out))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())
	})
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

// deleteStreamingFIC best-effort deletes the federated credential created for the given namespace.
// Deletes are also writes to the managed identity, so we retry on the concurrent-write error.
func deleteStreamingFIC(namespace string) {
	if !streamingEnabled() {
		return
	}
	identityName := os.Getenv("STREAMING_KUBELET_IDENTITY_NAME")
	identityRG := os.Getenv("STREAMING_KUBELET_IDENTITY_RG")
	ficName := streamingFICName(namespace)
	Eventually(func() error {
		// #nosec G204 -- inputs are CI-controlled env vars, not user input.
		cmd := exec.Command("az", "identity", "federated-credential", "delete",
			"--name", ficName,
			"--identity-name", identityName,
			"--resource-group", identityRG,
			"--yes", "-o", "none")
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if strings.Contains(string(out), "not found") || strings.Contains(string(out), "does not exist") {
			return nil
		}
		GinkgoWriter.Printf("warning: failed to delete FIC %s: %s\n", ficName, string(out))
		return fmt.Errorf("delete FIC %s: %s", ficName, string(out))
	}, 2*time.Minute, 10*time.Second).Should(Succeed())
}
