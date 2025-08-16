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

package resources

import (
	"context"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func CreateResource(ctx context.Context, resource client.Object, kubeClient client.Client) error {
	switch r := resource.(type) {
	case *appsv1.Deployment:
		klog.InfoS("CreateDeployment", "deployment", klog.KObj(r))
	case *appsv1.StatefulSet:
		klog.InfoS("CreateStatefulSet", "statefulset", klog.KObj(r))
	case *corev1.Service:
		klog.InfoS("CreateService", "service", klog.KObj(r))
	case *corev1.ConfigMap:
		klog.InfoS("CreateConfigMap", "configmap", klog.KObj(r))
	case *helmv2.HelmRelease:
		klog.InfoS("CreateHelmRelease", "helmrelease", klog.KObj(r))
	case *sourcev1.OCIRepository:
		klog.InfoS("CreateOCIRepository", "ocirepository", klog.KObj(r))
	}

	// Create the resource.
	return retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubeClient.Create(ctx, resource, &client.CreateOptions{})
	})
}

func GetResource(ctx context.Context, name, namespace string, kubeClient client.Client, resource client.Object) error {
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		return kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, resource, &client.GetOptions{})
	})

	return err
}

func CheckResourceStatus(obj client.Object, kubeClient client.Client, timeoutDuration time.Duration) error {
	// Use Context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			key := client.ObjectKey{
				Name:      obj.GetName(),
				Namespace: obj.GetNamespace(),
			}
			err := kubeClient.Get(ctx, key, obj)
			if err != nil {
				return err
			}

			switch k8sResource := obj.(type) {
			case *appsv1.Deployment:
				for _, condition := range k8sResource.Status.Conditions {
					if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse {
						err := fmt.Errorf("deployment %s is not progressing: %s", k8sResource.Name, condition.Message)
						klog.ErrorS(err, "deployment", k8sResource.Name, "reason", condition.Reason, "message", condition.Message)
						return err
					}
				}

				if k8sResource.Status.ReadyReplicas == *k8sResource.Spec.Replicas {
					klog.InfoS("deployment status is ready", "deployment", k8sResource.Name)
					return nil
				}
			case *appsv1.StatefulSet:
				if k8sResource.Status.ReadyReplicas == *k8sResource.Spec.Replicas {
					klog.InfoS("statefulset status is ready", "statefulset", k8sResource.Name)
					return nil
				}
			case *batchv1.Job:
				if k8sResource.Status.Failed > 0 {
					klog.ErrorS(fmt.Errorf("job failed"), "name", k8sResource.Name, "failed count", k8sResource.Status.Failed)
					return fmt.Errorf("job %s has failed %d pods", k8sResource.Name, k8sResource.Status.Failed)
				}
				if k8sResource.Status.Succeeded > 0 || (k8sResource.Status.Ready != nil && *k8sResource.Status.Ready > 0) {
					klog.InfoS("job status is active/succeeded", "name", k8sResource.Name)
					return nil
				}
			case *helmv2.HelmRelease:
				for _, condition := range k8sResource.Status.Conditions {
					if condition.Type == consts.ConditionReady && condition.Status == metav1.ConditionTrue {
						klog.InfoS("helmrelease status is ready", "helmrelease", k8sResource.Name)
						return nil
					}
				}
			case *sourcev1.OCIRepository:
				for _, condition := range k8sResource.Status.Conditions {
					if condition.Type == consts.ConditionReady && condition.Status == metav1.ConditionTrue {
						klog.InfoS("ocirepository status is ready", "ocirepository", k8sResource.Name)
						return nil
					}
				}
			default:
				return fmt.Errorf("unsupported resource type")
			}
		}
	}
}

// EnsureConfigOrCopyFromDefault handles two scenarios:
// 1. User provided config:
//   - Check if it exists in the target namespace
//   - If not found, return error as this is user-specified
//
// 2. No user config specified:
//   - Use the default config template
//   - Check if it exists in the target namespace
//   - If not, copy from release namespace to target namespace
func EnsureConfigOrCopyFromDefault(ctx context.Context, kubeClient client.Client,
	userProvided, systemDefault client.ObjectKey,
) (*corev1.ConfigMap, error) {

	// If user specified a config, use that
	if userProvided.Name != "" {
		userCM := &corev1.ConfigMap{}
		err := GetResource(ctx, userProvided.Name, userProvided.Namespace, kubeClient, userCM)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil, fmt.Errorf("user specified ConfigMap %s not found in namespace %s",
					userProvided.Name, userProvided.Namespace)
			}
			return nil, err
		}

		return userCM, nil
	}

	// Check if default configmap already exists in target namespace
	existingCM := &corev1.ConfigMap{}
	err := GetResource(ctx, systemDefault.Name, userProvided.Namespace, kubeClient, existingCM)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
	} else {
		klog.Infof("Default ConfigMap already exists in target namespace: %s, no action taken.", userProvided.Namespace)
		return existingCM, nil
	}

	// Copy default template from release namespace if not found
	if systemDefault.Namespace == "" {
		releaseNamespace, err := utils.GetReleaseNamespace()
		if err != nil {
			return nil, fmt.Errorf("failed to get release namespace: %v", err)
		}
		systemDefault.Namespace = releaseNamespace
	}

	templateCM := &corev1.ConfigMap{}
	err = GetResource(ctx, systemDefault.Name, systemDefault.Namespace, kubeClient, templateCM)
	if err != nil {
		return nil, fmt.Errorf("failed to get default ConfigMap from template namespace: %v", err)
	}

	templateCM.Namespace = userProvided.Namespace
	templateCM.ResourceVersion = "" // Clear metadata not needed for creation
	templateCM.UID = ""             // Clear UID

	err = CreateResource(ctx, templateCM, kubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create default ConfigMap in target namespace %s: %v",
			userProvided.Namespace, err)
	}

	return templateCM, nil
}
