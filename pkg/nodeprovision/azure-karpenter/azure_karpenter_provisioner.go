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

package azurekarpenter

import (
	"context"
	"fmt"
	"time"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	"github.com/awslabs/operatorpkg/status"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/nodeclass"
)

const (
	// nodeClassCheckInterval is how often the background goroutine checks
	// that global AKSNodeClasses still exist.
	nodeClassCheckInterval = 30 * time.Second
)

// aksNodeClassCRDName is the CRD name for AKSNodeClass.
const aksNodeClassCRDName = "aksnodeclasses.karpenter.azure.com"

// AzureKarpenterProvisioner implements NodeProvisioner using Azure Karpenter
// (https://github.com/Azure/karpenter-provider-azure).
// Start verifies that the AKSNodeClass CRD is installed and owned by Azure
// Karpenter, bootstraps global AKSNodeClass resources, verifies at least one
// is ready, and starts a background goroutine to ensure they remain present.
type AzureKarpenterProvisioner struct {
	client client.Client
}

var _ nodeprovision.NodeProvisioner = (*AzureKarpenterProvisioner)(nil)

// NewAzureKarpenterProvisioner creates a new AzureKarpenterProvisioner.
func NewAzureKarpenterProvisioner(c client.Client) *AzureKarpenterProvisioner {
	return &AzureKarpenterProvisioner{client: c}
}

// Name returns the provisioner name.
func (p *AzureKarpenterProvisioner) Name() string { return "AzureKarpenterProvisioner" }

// Start verifies that the AKSNodeClass CRD is installed and owned by Azure
// Karpenter (via the "nap" category), creates the global AKSNodeClass
// resources, verifies that at least one is ready, and starts a background
// goroutine that periodically ensures the global AKSNodeClasses exist.
func (p *AzureKarpenterProvisioner) Start(ctx context.Context) error {
	if err := verifyAKSNodeClassCRD(ctx, p.client); err != nil {
		return err
	}

	if err := nodeclass.EnsureGlobalAKSNodeClasses(ctx, p.client); err != nil {
		return fmt.Errorf("failed to bootstrap global AKSNodeClasses: %w", err)
	}

	// Verify that at least one global AKSNodeClass is ready.
	var anyReady bool
	for _, name := range []string{consts.AKSNodeClassUbuntuName, consts.AKSNodeClassAzureLinuxName} {
		if err := checkAKSNodeClassReady(ctx, p.client, name); err != nil {
			klog.ErrorS(err, "Global AKSNodeClass is not ready", "name", name)
		} else {
			anyReady = true
		}
	}
	if !anyReady {
		return fmt.Errorf("no global AKSNodeClass is ready")
	}

	klog.InfoS("AzureKarpenterProvisioner initialized successfully")

	go p.watchGlobalNodeClasses(ctx)

	return nil
}

// verifyAKSNodeClassCRD checks that the AKSNodeClass CRD is installed and
// its categories contain "nap", which confirms the CRD is owned by Azure Karpenter.
func verifyAKSNodeClassCRD(ctx context.Context, c client.Client) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.Get(ctx, client.ObjectKey{Name: aksNodeClassCRDName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("required CRD %s not found: Azure Karpenter must be installed", aksNodeClassCRDName)
		}
		return fmt.Errorf("failed to check CRD %s: %w", aksNodeClassCRDName, err)
	}
	klog.InfoS("Required CRD verified", "crd", aksNodeClassCRDName)

	// Verify that the CRD categories contain "nap" to confirm ownership by Azure Karpenter.
	for _, v := range crd.Spec.Names.Categories {
		if v == "nap" {
			klog.InfoS("AKSNodeClass CRD ownership confirmed via 'nap' category")
			return nil
		}
	}
	return fmt.Errorf("CRD %s does not have 'nap' in categories: CRD is not owned by Azure Karpenter", aksNodeClassCRDName)
}

// watchGlobalNodeClasses periodically checks that the global AKSNodeClasses
// exist and recreates them if they have been accidentally deleted.
func (p *AzureKarpenterProvisioner) watchGlobalNodeClasses(ctx context.Context) {
	ticker := time.NewTicker(nodeClassCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			klog.InfoS("Stopping global AKSNodeClass watcher")
			return
		case <-ticker.C:
			if err := nodeclass.EnsureGlobalAKSNodeClasses(ctx, p.client); err != nil {
				klog.ErrorS(err, "failed to ensure global AKSNodeClasses exist")
			}
		}
	}
}

// checkAKSNodeClassReady verifies that the specified AKSNodeClass has a root
// "Ready" condition set to True. If the AKSNodeClass is not ready, returns
// an error with details about which sub-conditions are not satisfied.
func checkAKSNodeClassReady(ctx context.Context, c client.Client, name string) error {
	nc := &azurev1beta1.AKSNodeClass{}
	if err := c.Get(ctx, client.ObjectKey{Name: name}, nc); err != nil {
		return fmt.Errorf("failed to get AKSNodeClass %s: %w", name, err)
	}
	condSet := nc.StatusConditions()
	if root := condSet.Root(); root != nil && root.IsTrue() {
		return nil
	}
	var notReady []string
	for _, cond := range condSet.List() {
		if cond.Type != status.ConditionReady && !cond.IsTrue() {
			notReady = append(notReady, fmt.Sprintf("%s=%s (%s)", cond.Type, cond.Status, cond.Message))
		}
	}
	return fmt.Errorf("AKSNodeClass %s is not ready: %v", name, notReady)
}

// ProvisionNodes is not yet implemented for AzureKarpenterProvisioner.
// TODO: Implement per-Workspace NodePool creation. Before creating the NodePool,
// use checkAKSNodeClassReady to verify that the referenced AKSNodeClass is ready.
func (p *AzureKarpenterProvisioner) ProvisionNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	return fmt.Errorf("AzureKarpenterProvisioner.ProvisionNodes is not yet implemented")
}

// DeleteNodes is not yet implemented for AzureKarpenterProvisioner.
// TODO: Implement per-Workspace NodePool deletion.
func (p *AzureKarpenterProvisioner) DeleteNodes(ctx context.Context, ws *kaitov1beta1.Workspace) error {
	return fmt.Errorf("AzureKarpenterProvisioner.DeleteNodes is not yet implemented")
}

// EnsureNodesReady is not yet implemented for AzureKarpenterProvisioner.
// TODO: Implement node readiness checks for Karpenter-managed nodes.
func (p *AzureKarpenterProvisioner) EnsureNodesReady(ctx context.Context, ws *kaitov1beta1.Workspace) (bool, bool, error) {
	return false, false, fmt.Errorf("AzureKarpenterProvisioner.EnsureNodesReady is not yet implemented")
}

// EnableDrift is not yet implemented for AzureKarpenterProvisioner.
// TODO: Patch NodePool disruption budget nodes="0" -> "1".
func (p *AzureKarpenterProvisioner) EnableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return fmt.Errorf("AzureKarpenterProvisioner.EnableDriftRemediation is not yet implemented")
}

// DisableDriftRemediation is not yet implemented for AzureKarpenterProvisioner.
// TODO: Patch NodePool disruption budget nodes="1" -> "0".
func (p *AzureKarpenterProvisioner) DisableDriftRemediation(ctx context.Context, workspaceNamespace, workspaceName string) error {
	return fmt.Errorf("AzureKarpenterProvisioner.DisableDriftRemediation is not yet implemented")
}

// CollectNodeStatusInfo is not yet implemented for AzureKarpenterProvisioner.
// TODO: Implement node status collection for Karpenter-managed nodes.
func (p *AzureKarpenterProvisioner) CollectNodeStatusInfo(ctx context.Context, ws *kaitov1beta1.Workspace) ([]metav1.Condition, error) {
	return nil, fmt.Errorf("AzureKarpenterProvisioner.CollectNodeStatusInfo is not yet implemented")
}
