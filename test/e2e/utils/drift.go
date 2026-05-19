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

package utils

import (
	"context"
	"fmt"
	"time"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// TriggerDrift modifies the default AKSNodeClass to cause Karpenter to detect drift
// on all NodeClaims referencing it. It adds/updates a drift-trigger annotation.
func TriggerDrift(ctx context.Context) {
	ginkgo.By("Updating AKSNodeClass to trigger drift detection", func() {
		nc := &azurev1beta1.AKSNodeClass{}
		gomega.Eventually(func() error {
			if err := TestingCluster.KubeClient.Get(ctx, client.ObjectKey{
				Name: consts.AKSNodeClassUbuntuName,
			}, nc); err != nil {
				return err
			}
			if nc.Annotations == nil {
				nc.Annotations = make(map[string]string)
			}
			nc.Annotations["kaito.sh/drift-trigger"] = fmt.Sprintf("%d", time.Now().UnixNano())
			return TestingCluster.KubeClient.Update(ctx, nc)
		}, 1*time.Minute, PollInterval).Should(gomega.Succeed(),
			"Should update AKSNodeClass to trigger drift")
	})
}
