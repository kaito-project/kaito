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

package nodeclass

import (
	"context"
	"errors"
	"fmt"
	"os"

	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	awsv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/samber/lo"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// GenerateAKSNodeClassManifest generates a default AKSNodeClass object.
func GenerateAKSNodeClassManifest(ctx context.Context) *azurev1beta1.AKSNodeClass {
	return &azurev1beta1.AKSNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: consts.NodeClassName,
			Annotations: map[string]string{
				"kubernetes.io/description": "General purpose AKSNodeClass for running Ubuntu 22.04 nodes",
			},
		},
		Spec: azurev1beta1.AKSNodeClassSpec{
			ImageFamily: lo.ToPtr("Ubuntu2204"),
		},
	}
}

// GenerateEC2NodeClassManifest generates a default EC2NodeClass object.
func GenerateEC2NodeClassManifest(ctx context.Context) *awsv1.EC2NodeClass {
	clusterName := os.Getenv("CLUSTER_NAME")
	return &awsv1.EC2NodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kubernetes.io/description": "General purpose EC2NodeClass for running Amazon Linux 2 nodes",
			},
			Name: consts.NodeClassName,
		},
		Spec: awsv1.EC2NodeClassSpec{
			AMIFamily:           lo.ToPtr(awsv1.AMIFamilyAL2),
			Role:                fmt.Sprintf("KarpenterNodeRole-%s", clusterName),
			InstanceStorePolicy: lo.ToPtr(awsv1.InstanceStorePolicyRAID0),
			SubnetSelectorTerms: []awsv1.SubnetSelectorTerm{
				{
					Tags: map[string]string{
						"karpenter.sh/discovery": clusterName,
					},
				},
			},
			SecurityGroupSelectorTerms: []awsv1.SecurityGroupSelectorTerm{
				{
					Tags: map[string]string{
						"karpenter.sh/discovery": clusterName,
					},
				},
			},
		},
	}
}

// CreateKarpenterNodeClass creates a nodeClass object for Karpenter.
// Deprecated: Use CheckNodeClass instead, which handles create-if-not-exists.
func CreateKarpenterNodeClass(ctx context.Context, kubeClient client.Client) error {
	cloudName := os.Getenv("CLOUD_PROVIDER")
	klog.InfoS("CreateKarpenterNodeClass", "cloudName", cloudName)

	switch cloudName {
	case consts.AzureCloudName:
		return kubeClient.Create(ctx, GenerateAKSNodeClassManifest(ctx), &client.CreateOptions{})
	case consts.AWSCloudName:
		return kubeClient.Create(ctx, GenerateEC2NodeClassManifest(ctx), &client.CreateOptions{})
	default:
		return fmt.Errorf("unsupported cloud provider %s", cloudName)
	}
}

// IsNodeClassAvailable checks if a NodeClass resource exists for the given cloud provider.
// Deprecated: Use CheckNodeClass instead, which handles create-if-not-exists.
func IsNodeClassAvailable(ctx context.Context, cloudName string, kubeClient client.Client) bool {
	switch cloudName {
	case consts.AzureCloudName:
		return kubeClient.Get(ctx, client.ObjectKey{Name: consts.NodeClassName},
			&azurev1beta1.AKSNodeClass{}, &client.GetOptions{}) == nil
	case consts.AWSCloudName:
		return kubeClient.Get(ctx, client.ObjectKey{Name: consts.NodeClassName},
			&awsv1.EC2NodeClass{}, &client.GetOptions{}) == nil
	default:
		klog.Error("unsupported cloud provider ", cloudName)
		return false
	}
}

// CheckNodeClass ensures the default Karpenter NodeClass exists for the current cloud provider.
// If the NodeClass does not exist, it creates one automatically.
func CheckNodeClass(ctx context.Context, kClient client.Client) error {
	cloudProvider := os.Getenv("CLOUD_PROVIDER")
	if cloudProvider == "" {
		return errors.New("CLOUD_PROVIDER environment variable cannot be empty")
	}
	if !IsNodeClassAvailable(ctx, cloudProvider, kClient) {
		klog.Infof("NodeClass is not available, creating NodeClass")
		if err := CreateKarpenterNodeClass(ctx, kClient); err != nil && client.IgnoreAlreadyExists(err) != nil {
			klog.ErrorS(err, "unable to create NodeClass")
			return errors.New("error while creating NodeClass")
		}
	}
	return nil
}

// VerifyAKSNodeClassCRD checks if the AKSNodeClass CRD is installed in the cluster.
// Returns an error if the CRD does not exist (i.e., Azure Karpenter is not installed).
func VerifyAKSNodeClassCRD(ctx context.Context, c client.Client) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	crdName := "aksnodeclasses.karpenter.azure.com"
	if err := c.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("AKSNodeClass CRD (%s) not found: Azure Karpenter must be installed before enabling ensureNodeClass feature gate", crdName)
		}
		return fmt.Errorf("failed to check AKSNodeClass CRD existence: %w", err)
	}
	klog.InfoS("AKSNodeClass CRD verified", "crd", crdName)
	return nil
}

// ensureAKSNodeClassExists checks if an AKSNodeClass exists and creates it if not.
func ensureAKSNodeClassExists(ctx context.Context, c client.Client, desired *azurev1beta1.AKSNodeClass) error {
	if err := c.Get(ctx, client.ObjectKey{Name: desired.Name}, &azurev1beta1.AKSNodeClass{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to check AKSNodeClass %s: %w", desired.Name, err)
		}
		klog.InfoS("Creating AKSNodeClass", "name", desired.Name)
		if err := c.Create(ctx, desired); err != nil {
			if apierrors.IsAlreadyExists(err) {
				klog.InfoS("AKSNodeClass already exists (concurrent create)", "name", desired.Name)
				return nil
			}
			return fmt.Errorf("failed to create AKSNodeClass %s: %w", desired.Name, err)
		}
		klog.InfoS("Created AKSNodeClass", "name", desired.Name)
	}
	return nil
}

// EnsureGlobalAKSNodeClasses creates the global AKSNodeClass resources if they do not already exist.
// Two AKSNodeClasses are created: image-family-ubuntu and image-family-azure-linux.
func EnsureGlobalAKSNodeClasses(ctx context.Context, c client.Client) error {
	nodeClasses := []*azurev1beta1.AKSNodeClass{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: consts.AKSNodeClassUbuntuName,
				Labels: map[string]string{
					consts.KarpenterLabelManagedBy: consts.KarpenterManagedByValue,
				},
			},
			Spec: azurev1beta1.AKSNodeClassSpec{
				ImageFamily:  lo.ToPtr("Ubuntu2204"),
				OSDiskSizeGB: lo.ToPtr(int32(consts.AKSNodeClassOSDiskSizeGB)),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: consts.AKSNodeClassAzureLinuxName,
				Labels: map[string]string{
					consts.KarpenterLabelManagedBy: consts.KarpenterManagedByValue,
				},
			},
			Spec: azurev1beta1.AKSNodeClassSpec{
				ImageFamily:  lo.ToPtr("AzureLinux"),
				OSDiskSizeGB: lo.ToPtr(int32(consts.AKSNodeClassOSDiskSizeGB)),
			},
		},
	}

	for _, nc := range nodeClasses {
		if err := ensureAKSNodeClassExists(ctx, c, nc); err != nil {
			return err
		}
	}
	return nil
}
