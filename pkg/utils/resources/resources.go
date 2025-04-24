// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package resources

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"

	"github.com/kaito-project/kaito/pkg/utils"
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
	case *lwsv1.LeaderWorkerSet:
		klog.InfoS("CreateLeaderWorkerSet", "leaderworkerset", klog.KObj(r))
	}

	// Create the resource.
	return retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		err := kubeClient.Create(ctx, resource, &client.CreateOptions{})
		klog.ErrorS(err, "resource", resource)
		return err
	})
}

func GetResource(ctx context.Context, name, namespace string, kubeClient client.Client, resource client.Object) error {
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
		if apierrors.IsNotFound(err) {
			return false
		}
		return true
	}, func() error {
		err := kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, resource, &client.GetOptions{})
		klog.ErrorS(err, "resource", resource, "namespace", namespace, "name", name)
		return err
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
			case *lwsv1.LeaderWorkerSet:
				if k8sResource.Status.ReadyReplicas == *k8sResource.Spec.Replicas {
					klog.InfoS("leaderworker status is ready", "leaderworker", k8sResource.Name)
					return nil
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
