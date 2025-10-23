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

	"github.com/awslabs/operatorpkg/status"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kaito-project/kaito/api/v1beta1"
)

// ValidateNodeClaimCreation Logic to validate the nodeClaim creation.
func ValidateNodeClaimCreation(ctx context.Context, workspaceObj *v1beta1.Workspace, expectedCount int) {
	ginkgo.By("Checking nodeClaim created by the workspace CR", func() {
		gomega.Eventually(func() bool {
			nodeClaimList, err := GetAllValidNodeClaims(ctx, workspaceObj)
			if err != nil {
				fmt.Printf("Failed to get all valid nodeClaim: %v", err)
				return false
			}

			if len(nodeClaimList.Items) != expectedCount {
				for _, nodeClaim := range nodeClaimList.Items {
					fmt.Printf("Found nodeClaim: %s\n", nodeClaim.Name)
					fmt.Printf("NodeClaim is: %+v\n", nodeClaim)
				}
				return false
			}

			for _, nodeClaim := range nodeClaimList.Items {
				_, conditionFound := lo.Find(nodeClaim.GetConditions(), func(condition status.Condition) bool {
					fmt.Printf("Found nodeClaim with condition: %s\n", nodeClaim.Name)
					fmt.Printf("NodeClaim is: %+v\n", nodeClaim)
					fmt.Printf("ConditionReady is : %+v\n", condition)
					return condition.Type == string(apis.ConditionReady) && condition.Status == metav1.ConditionTrue
				})
				if !conditionFound {
					fmt.Printf("Found nodeClaim without condition: %s\n", nodeClaim.Name)
					fmt.Printf("NodeClaim is: %+v\n", nodeClaim)
					return false
				}
			}
			return true
		}, 20*time.Minute, PollInterval).Should(gomega.BeTrue(), "Failed to wait for nodeClaim to be ready")
	})
}

// GetAllValidNodeClaims get all valid nodeClaims.
func GetAllValidNodeClaims(ctx context.Context, workspaceObj *v1beta1.Workspace) (*karpenterv1.NodeClaimList, error) {
	nodeClaimList := &karpenterv1.NodeClaimList{}
	ls := labels.Set{
		v1beta1.LabelWorkspaceName:      workspaceObj.Name,
		v1beta1.LabelWorkspaceNamespace: workspaceObj.Namespace,
	}

	err := TestingCluster.KubeClient.List(ctx, nodeClaimList, &client.MatchingLabelsSelector{Selector: ls.AsSelector()})
	if err != nil {
		return nil, err
	}
	return nodeClaimList, nil
}
