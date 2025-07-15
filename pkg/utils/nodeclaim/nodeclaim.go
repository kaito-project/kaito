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

package nodeclaim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	awsv1beta1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1beta1"
	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
)

var (
	// nodeClaimStatusTimeoutInterval is the interval to check the nodeClaim status.
	nodeClaimStatusTimeoutInterval = 240 * time.Second

	NodeClaimPredicate = predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNodeClaim, ok := e.ObjectOld.(*karpenterv1.NodeClaim)
			if !ok {
				return false
			}

			newNodeClaim, ok := e.ObjectNew.(*karpenterv1.NodeClaim)
			if !ok {
				return false
			}

			oldNodeClaimCopy := oldNodeClaim.DeepCopy()
			newNodeClaimCopy := newNodeClaim.DeepCopy()

			// if only nodeclaim.Status.LastPodEventTime is changed, skip update event
			oldNodeClaimCopy.ResourceVersion = ""
			oldNodeClaimCopy.Status.LastPodEventTime = metav1.Time{}
			newNodeClaimCopy.ResourceVersion = ""
			newNodeClaimCopy.Status.LastPodEventTime = metav1.Time{}
			return !reflect.DeepEqual(oldNodeClaimCopy, newNodeClaimCopy)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}
)

// GenerateNodeClaimManifest generates a nodeClaim object from the given workspace or RAGEngine.
func GenerateNodeClaimManifest(storageRequirement string, obj client.Object) *karpenterv1.NodeClaim {
	klog.InfoS("GenerateNodeClaimManifest", "object", obj)

	// Determine the type of the input object and extract relevant fields
	instanceType, namespace, name, labelSelector, nameLabel, namespaceLabel, err := resources.ExtractObjFields(obj)
	if err != nil {
		klog.Error(err)
		return nil
	}

	nodeClaimName := GenerateNodeClaimName(obj)

	nodeClaimLabels := map[string]string{
		consts.LabelNodePool: consts.KaitoNodePoolName, // Fake nodepool name to prevent Karpenter from scaling up.
		nameLabel:            name,
		namespaceLabel:       namespace,
	}
	if labelSelector != nil && len(labelSelector.MatchLabels) != 0 {
		nodeClaimLabels = lo.Assign(nodeClaimLabels, labelSelector.MatchLabels)
	}

	nodeClaimAnnotations := map[string]string{
		karpenterv1.DoNotDisruptAnnotationKey: "true", // To prevent Karpenter from scaling down.
	}

	cloudName := os.Getenv("CLOUD_PROVIDER")

	var nodeClassRefKind string

	if cloudName == consts.AzureCloudName {
		nodeClassRefKind = "AKSNodeClass"
	} else if cloudName == consts.AWSCloudName { //aws
		nodeClassRefKind = "EC2NodeClass"
	}
	nodeClaimObj := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nodeClaimName,
			Namespace:   namespace,
			Labels:      nodeClaimLabels,
			Annotations: nodeClaimAnnotations,
		},
		Spec: karpenterv1.NodeClaimSpec{
			NodeClassRef: &karpenterv1.NodeClassReference{
				Name: consts.NodeClassName,
				Kind: nodeClassRefKind,
			},
			Taints: []v1.Taint{
				{
					Key:    consts.SKUString,
					Value:  consts.GPUString,
					Effect: v1.TaintEffectNoSchedule,
				},
			},
			Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: v1.NodeSelectorRequirement{
						Key:      consts.LabelNodePool,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{consts.KaitoNodePoolName},
					},
				},
				{
					NodeSelectorRequirement: v1.NodeSelectorRequirement{
						Key:      v1.LabelInstanceTypeStable,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{instanceType},
					},
				},
				{
					NodeSelectorRequirement: v1.NodeSelectorRequirement{
						Key:      v1.LabelOSStable,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{"linux"},
					},
				},
			},
			Resources: karpenterv1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse(storageRequirement),
				},
			},
		},
	}

	if cloudName == consts.AzureCloudName {
		nodeSelector := karpenterv1.NodeSelectorRequirementWithMinValues{
			NodeSelectorRequirement: v1.NodeSelectorRequirement{
				Key:      azurev1alpha2.LabelSKUName,
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{instanceType},
			},
		}
		nodeClaimObj.Spec.Requirements = append(nodeClaimObj.Spec.Requirements, nodeSelector)
	}

	if cloudName == consts.AWSCloudName {
		nodeSelector := karpenterv1.NodeSelectorRequirementWithMinValues{
			NodeSelectorRequirement: v1.NodeSelectorRequirement{
				Key:      "karpenter.k8s.aws/instance-gpu-count",
				Operator: v1.NodeSelectorOpGt,
				Values:   []string{"0"},
			},
		}
		nodeClaimObj.Spec.Requirements = append(nodeClaimObj.Spec.Requirements, nodeSelector)
	}

	return nodeClaimObj
}

// GenerateNodeClaimName generates a nodeClaim name from the given workspace or RAGEngine.
func GenerateNodeClaimName(obj client.Object) string {
	// Determine the type of the input object and extract relevant fields
	_, namespace, name, _, _, _, err := resources.ExtractObjFields(obj)
	if err != nil {
		return ""
	}

	digest := sha256.Sum256([]byte(namespace + name + time.Now().Format("2006-01-02 15:04:05.000000000"))) // We make sure the nodeClaim name is not fixed to the object
	nodeClaimName := "ws" + hex.EncodeToString(digest[0:])[0:9]
	return nodeClaimName
}

func GenerateAKSNodeClassManifest(ctx context.Context) *azurev1alpha2.AKSNodeClass {
	return &azurev1alpha2.AKSNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: consts.NodeClassName,
			Annotations: map[string]string{
				"kubernetes.io/description": "General purpose AKSNodeClass for running Ubuntu 22.04 nodes",
			},
		},
		Spec: azurev1alpha2.AKSNodeClassSpec{
			ImageFamily: lo.ToPtr("Ubuntu2204"),
		},
	}
}

func GenerateEC2NodeClassManifest(ctx context.Context) *awsv1beta1.EC2NodeClass {
	clusterName := os.Getenv("CLUSTER_NAME")
	return &awsv1beta1.EC2NodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kubernetes.io/description": "General purpose EC2NodeClass for running Amazon Linux 2 nodes",
			},
			Name: consts.NodeClassName,
		},
		Spec: awsv1beta1.EC2NodeClassSpec{
			AMIFamily:           lo.ToPtr(awsv1beta1.AMIFamilyAL2), // Amazon Linux 2
			Role:                fmt.Sprintf("KarpenterNodeRole-%s", clusterName),
			InstanceStorePolicy: lo.ToPtr(awsv1beta1.InstanceStorePolicyRAID0), //required to share node's ephermeral storage among pods that request it
			SubnetSelectorTerms: []awsv1beta1.SubnetSelectorTerm{
				{
					Tags: map[string]string{
						"karpenter.sh/discovery": clusterName, // replace with your cluster name
					},
				},
			},
			SecurityGroupSelectorTerms: []awsv1beta1.SecurityGroupSelectorTerm{
				{
					Tags: map[string]string{
						"karpenter.sh/discovery": clusterName, // replace with your cluster name
					},
				},
			},
		},
	}
}

// CreateNodeClaim creates a nodeClaim object.
func CreateNodeClaim(ctx context.Context, nodeClaimObj *karpenterv1.NodeClaim, kubeClient client.Client) error {
	klog.InfoS("CreateNodeClaim", "nodeClaim", klog.KObj(nodeClaimObj))
	if featuregates.FeatureGates[consts.FeatureFlagEnsureNodeClass] {
		err := CheckNodeClass(ctx, kubeClient)
		if err != nil {
			return err
		}
	}

	return kubeClient.Create(ctx, nodeClaimObj, &client.CreateOptions{})
}

// CreateKarpenterNodeClass creates a nodeClass object for Karpenter.
func CreateKarpenterNodeClass(ctx context.Context, kubeClient client.Client) error {
	cloudName := os.Getenv("CLOUD_PROVIDER")
	klog.InfoS("CreateKarpenterNodeClass", "cloudName", cloudName)

	if cloudName == consts.AzureCloudName {
		nodeClassObj := GenerateAKSNodeClassManifest(ctx)
		return kubeClient.Create(ctx, nodeClassObj, &client.CreateOptions{})
	} else if cloudName == consts.AWSCloudName {
		nodeClassObj := GenerateEC2NodeClassManifest(ctx)
		return kubeClient.Create(ctx, nodeClassObj, &client.CreateOptions{})
	} else {
		return errors.New("unsupported cloud provider " + cloudName)
	}
}

// WaitForPendingNodeClaims checks if there are any nodeClaims in provisioning condition. If so, wait until they are ready.
func WaitForPendingNodeClaims(ctx context.Context, obj client.Object, kubeClient client.Client) error {

	// Determine the type of the input object and retrieve the InstanceType
	instanceType, _, _, _, _, _, err := resources.ExtractObjFields(obj)
	if err != nil {
		return err
	}

	nodeClaims, err := ListNodeClaim(ctx, obj, kubeClient)
	if err != nil {
		return err
	}

	for i := range nodeClaims.Items {
		// check if the nodeClaim being created has the requested instance type
		_, nodeClaimInstanceType := lo.Find(nodeClaims.Items[i].Spec.Requirements, func(requirement karpenterv1.NodeSelectorRequirementWithMinValues) bool {
			return requirement.Key == v1.LabelInstanceTypeStable &&
				requirement.Operator == v1.NodeSelectorOpIn &&
				lo.Contains(requirement.Values, instanceType)
		})
		if nodeClaimInstanceType {
			_, nodeClaimIsInitalized := lo.Find(nodeClaims.Items[i].GetConditions(), func(condition status.Condition) bool {
				return condition.Type == karpenterv1.ConditionTypeInitialized && condition.Status == metav1.ConditionTrue
			})

			if nodeClaimIsInitalized {
				continue
			}

			// wait until the nodeClaim is initialized
			if err := CheckNodeClaimStatus(ctx, &nodeClaims.Items[i], kubeClient); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListNodeClaim lists all nodeClaim objects in the cluster that are created by the given workspace or RAGEngine.
func ListNodeClaim(ctx context.Context, obj client.Object, kubeClient client.Client) (*karpenterv1.NodeClaimList, error) {
	nodeClaimList := &karpenterv1.NodeClaimList{}

	var ls labels.Set

	// Build label selector based on the type of the input object
	switch o := obj.(type) {
	case *kaitov1beta1.Workspace:
		ls = labels.Set{
			kaitov1beta1.LabelWorkspaceName:      o.Name,
			kaitov1beta1.LabelWorkspaceNamespace: o.Namespace,
		}
	case *kaitov1alpha1.RAGEngine:
		ls = labels.Set{
			kaitov1alpha1.LabelRAGEngineName:      o.Name,
			kaitov1alpha1.LabelRAGEngineNamespace: o.Namespace,
		}
	default:
		return nil, fmt.Errorf("unsupported object type: %T", obj)
	}

	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubeClient.List(ctx, nodeClaimList, &client.MatchingLabelsSelector{Selector: ls.AsSelector()})
	})
	if err != nil {
		return nil, err
	}

	return nodeClaimList, nil
}

// CheckNodeClaimStatus checks the status of the nodeClaim. If the nodeClaim is not ready, then it will wait for the nodeClaim to be ready.
// If the nodeClaim is not ready after the timeout, then it will return an error.
// if the nodeClaim is ready, then it will return nil.
func CheckNodeClaimStatus(ctx context.Context, nodeClaimObj *karpenterv1.NodeClaim, kubeClient client.Client) error {
	klog.InfoS("CheckNodeClaimStatus", "nodeClaim", klog.KObj(nodeClaimObj))
	timeClock := clock.RealClock{}
	tick := timeClock.NewTicker(nodeClaimStatusTimeoutInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-tick.C():
			return fmt.Errorf("check nodeClaim status timed out. nodeClaim %s is not ready", nodeClaimObj.Name)

		default:
			time.Sleep(1 * time.Second)
			err := kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaimObj.Name, Namespace: nodeClaimObj.Namespace}, nodeClaimObj, &client.GetOptions{})
			if err != nil {
				return err
			}

			// if SKU is not available, then no need to retry.
			_, conditionFound := lo.Find(nodeClaimObj.GetConditions(), func(condition status.Condition) bool {
				return condition.Type == karpenterv1.ConditionTypeLaunched &&
					condition.Status == metav1.ConditionFalse && strings.Contains(condition.Message, consts.ErrorInstanceTypesUnavailable)
			})
			if conditionFound {
				klog.Error(consts.ErrorInstanceTypesUnavailable, "reconcile will not continue")
				return reconcile.TerminalError(fmt.Errorf(consts.ErrorInstanceTypesUnavailable))
			}

			// if nodeClaim is not ready, then continue.
			_, conditionFound = lo.Find(nodeClaimObj.GetConditions(), func(condition status.Condition) bool {
				return condition.Type == string(apis.ConditionReady) &&
					condition.Status == metav1.ConditionTrue
			})
			if !conditionFound {
				continue
			}

			klog.InfoS("nodeClaim status is ready", "nodeClaim", nodeClaimObj.Name)
			return nil
		}
	}
}

func IsNodeClassAvailable(ctx context.Context, cloudName string, kubeClient client.Client) bool {
	if cloudName == consts.AzureCloudName {
		err := kubeClient.Get(ctx, client.ObjectKey{Name: consts.NodeClassName},
			&azurev1alpha2.AKSNodeClass{}, &client.GetOptions{})
		return err == nil
	} else if cloudName == consts.AWSCloudName {
		err := kubeClient.Get(ctx, client.ObjectKey{Name: consts.NodeClassName},
			&awsv1beta1.EC2NodeClass{}, &client.GetOptions{})
		return err == nil
	}
	klog.Error("unsupported cloud provider ", cloudName)
	return false
}

// CheckNodeClass checks if Karpenter NodeClass is available. If not, the controller will create it automatically.
// This is only applicable when Karpenter feature flag is enabled.
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
