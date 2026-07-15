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

package azure

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/workspace/inference/modelstreaming"
)

// ValidateStreamingServiceAccount verifies the streaming ServiceAccount exists in the
// workspace namespace and carries the Azure Workload Identity client-id annotation.
// Shared by the Azure streaming providers (SAS and WI).
func ValidateStreamingServiceAccount(ctx context.Context, ws *v1beta1.Workspace, kubeClient client.Client, defaultSA string) error {
	saName, err := modelstreaming.ResolveStreamingServiceAccount(ws, defaultSA)
	if err != nil {
		return err
	}
	sa := &corev1.ServiceAccount{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: ws.Namespace}, sa); err != nil {
		return fmt.Errorf("ServiceAccount %q not found in namespace %q: %w", saName, ws.Namespace, err)
	}
	if sa.Annotations["azure.workload.identity/client-id"] == "" {
		return fmt.Errorf("ServiceAccount %q is missing annotation azure.workload.identity/client-id; "+
			"workload identity must be configured for model streaming", saName)
	}
	return nil
}
