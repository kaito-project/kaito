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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/nodeclass"
)

const (
	// nodeClassCheckInterval is how often the background goroutine checks
	// that global AKSNodeClasses still exist.
	nodeClassCheckInterval = 30 * time.Second
)

// requiredCRDs lists the CRD names that must be present for Azure Karpenter to work.
var requiredCRDs = []string{
	"nodepools.karpenter.sh",
	"nodeclaims.karpenter.sh",
	"aksnodeclasses.karpenter.azure.com",
}

// AzureKarpenterProvisioner implements NodeProvisioner using Azure Karpenter
// (https://github.com/Azure/karpenter-provider-azure).
// Init verifies that required Karpenter CRDs are installed, bootstraps global
// AKSNodeClass resources, and starts a background goroutine to recreate them
// if they are accidentally deleted.
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

// Start verifies that the NodePool, NodeClaim, and AKSNodeClass CRDs are
// installed in the cluster (i.e., Azure Karpenter is deployed), creates
// the global AKSNodeClass resources, and starts a background goroutine
// that periodically ensures the global AKSNodeClasses exist.
func (p *AzureKarpenterProvisioner) Start(ctx context.Context) error {
	if err := verifyRequiredCRDs(ctx, p.client); err != nil {
		return err
	}

	if err := nodeclass.EnsureGlobalAKSNodeClasses(ctx, p.client); err != nil {
		return fmt.Errorf("failed to bootstrap global AKSNodeClasses: %w", err)
	}
	klog.InfoS("AzureKarpenterProvisioner initialized successfully")

	go p.watchGlobalNodeClasses(ctx)

	return nil
}

// verifyRequiredCRDs checks that all required Karpenter CRDs are installed.
func verifyRequiredCRDs(ctx context.Context, c client.Client) error {
	for _, crdName := range requiredCRDs {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := c.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("required CRD %s not found: Azure Karpenter must be installed", crdName)
			}
			return fmt.Errorf("failed to check CRD %s: %w", crdName, err)
		}
		klog.InfoS("Required CRD verified", "crd", crdName)
	}
	return nil
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

// ProvisionNodes is not yet implemented for AzureKarpenterProvisioner.
// TODO: Implement per-Workspace NodePool creation.
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
