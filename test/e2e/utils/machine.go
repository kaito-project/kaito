// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/samber/lo"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateMachineCreation Logic to validate machine creation
func ValidateMachineCreation(ctx context.Context, workspaceObj *v1alpha1.Workspace, expectedCount int) {
	ginkgo.By("Checking machine created by the workspace CR", func() {
		gomega.Eventually(func() bool {
			machineList, err := getAllValidMachines(ctx, workspaceObj)
			if err != nil {
				fmt.Printf("Failed to get all valid machines: %v", err)
				return false
			}

			if len(machineList.Items) != expectedCount {
				return false
			}

			for _, machine := range machineList.Items {
				_, conditionFound := lo.Find(machine.GetConditions(), func(condition apis.Condition) bool {
					return condition.Type == apis.ConditionReady && condition.Status == v1.ConditionTrue
				})
				if !conditionFound {
					return false
				}
			}
			return true
		}, 20*time.Minute, PollInterval).Should(gomega.BeTrue(), "Failed to wait for machine to be ready")
	})
}

func getAllValidMachines(ctx context.Context, workspaceObj *v1alpha1.Workspace) (*v1alpha5.MachineList, error) {
	machineList := &v1alpha5.MachineList{}
	ls := labels.Set{
		v1alpha1.LabelWorkspaceName:      workspaceObj.Name,
		v1alpha1.LabelWorkspaceNamespace: workspaceObj.Namespace,
	}

	err := TestingCluster.KubeClient.List(ctx, machineList, &client.MatchingLabelsSelector{Selector: ls.AsSelector()})
	if err != nil {
		return nil, err
	}
	return machineList, nil
}
