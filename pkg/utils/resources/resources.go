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
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
						errorMessage := fmt.Sprintf("deployment %s is not progressing: %s", k8sResource.Name, condition.Message)
						klog.ErrorS(fmt.Errorf(errorMessage), "deployment", k8sResource.Name, "reason", condition.Reason, "message", condition.Message)
						return fmt.Errorf(errorMessage)
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
			default:
				return fmt.Errorf("unsupported resource type")
			}
		}
	}
}
