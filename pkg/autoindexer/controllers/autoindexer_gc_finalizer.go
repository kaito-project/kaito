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

package controllers

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// AutoIndexerGCFinalizer handles garbage collection for AutoIndexer resources
type AutoIndexerGCFinalizer struct {
	client client.Client
}

// NewAutoIndexerGCFinalizer creates a new AutoIndexer garbage collector finalizer
func NewAutoIndexerGCFinalizer(client client.Client) *AutoIndexerGCFinalizer {
	return &AutoIndexerGCFinalizer{
		client: client,
	}
}

// GarbageCollectAutoIndexer performs garbage collection for an AutoIndexer being deleted
func (f *AutoIndexerGCFinalizer) GarbageCollectAutoIndexer(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	klog.InfoS("Starting garbage collection for AutoIndexer", "autoindexer", klog.KObj(autoIndexerObj))

	// Clean up Jobs
	if err := f.cleanupJobs(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to cleanup jobs: %w", err)
	}

	// Clean up CronJobs
	if err := f.cleanupCronJobs(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to cleanup cronjobs: %w", err)
	}

	// Clean up ConfigMaps if any were created
	if err := f.cleanupConfigMaps(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to cleanup configmaps: %w", err)
	}

	// Clean up Secrets if any were created
	if err := f.cleanupSecrets(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("failed to cleanup secrets: %w", err)
	}

	// Wait for resources to be fully deleted
	if err := f.waitForResourceCleanup(ctx, autoIndexerObj); err != nil {
		return fmt.Errorf("timeout waiting for resource cleanup: %w", err)
	}

	klog.InfoS("Garbage collection completed for AutoIndexer", "autoindexer", klog.KObj(autoIndexerObj))
	return nil
}

// cleanupJobs removes all Jobs owned by the AutoIndexer
func (f *AutoIndexerGCFinalizer) cleanupJobs(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	jobs := &batchv1.JobList{}
	listOpts := []client.ListOption{
		client.InNamespace(autoIndexerObj.Namespace),
		client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name},
	}

	if err := f.client.List(ctx, jobs, listOpts...); err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	for _, job := range jobs.Items {
		klog.InfoS("Deleting job", "job", klog.KObj(&job), "autoindexer", klog.KObj(autoIndexerObj))

		// Set deletion policy to delete dependent pods immediately
		deletePolicy := client.PropagationPolicy("Background")
		if err := f.client.Delete(ctx, &job, deletePolicy); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete job %s: %w", job.Name, err)
			}
		}
	}

	return nil
}

// cleanupCronJobs removes all CronJobs owned by the AutoIndexer
func (f *AutoIndexerGCFinalizer) cleanupCronJobs(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	cronJobs := &batchv1.CronJobList{}
	listOpts := []client.ListOption{
		client.InNamespace(autoIndexerObj.Namespace),
		client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name},
	}

	if err := f.client.List(ctx, cronJobs, listOpts...); err != nil {
		return fmt.Errorf("failed to list cronjobs: %w", err)
	}

	for _, cronJob := range cronJobs.Items {
		klog.InfoS("Deleting cronjob", "cronjob", klog.KObj(&cronJob), "autoindexer", klog.KObj(autoIndexerObj))

		// Set deletion policy to delete dependent jobs immediately
		deletePolicy := client.PropagationPolicy("Background")
		if err := f.client.Delete(ctx, &cronJob, deletePolicy); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete cronjob %s: %w", cronJob.Name, err)
			}
		}
	}

	return nil
}

// cleanupConfigMaps removes ConfigMaps created for the AutoIndexer
func (f *AutoIndexerGCFinalizer) cleanupConfigMaps(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	configMaps := &corev1.ConfigMapList{}
	listOpts := []client.ListOption{
		client.InNamespace(autoIndexerObj.Namespace),
		client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name},
	}

	if err := f.client.List(ctx, configMaps, listOpts...); err != nil {
		return fmt.Errorf("failed to list configmaps: %w", err)
	}

	for _, configMap := range configMaps.Items {
		klog.InfoS("Deleting configmap", "configmap", klog.KObj(&configMap), "autoindexer", klog.KObj(autoIndexerObj))

		if err := f.client.Delete(ctx, &configMap); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete configmap %s: %w", configMap.Name, err)
			}
		}
	}

	return nil
}

// cleanupSecrets removes Secrets created for the AutoIndexer
func (f *AutoIndexerGCFinalizer) cleanupSecrets(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	secrets := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(autoIndexerObj.Namespace),
		client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name},
	}

	if err := f.client.List(ctx, secrets, listOpts...); err != nil {
		return fmt.Errorf("failed to list secrets: %w", err)
	}

	for _, secret := range secrets.Items {
		klog.InfoS("Deleting secret", "secret", klog.KObj(&secret), "autoindexer", klog.KObj(autoIndexerObj))

		if err := f.client.Delete(ctx, &secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete secret %s: %w", secret.Name, err)
			}
		}
	}

	return nil
}

// waitForResourceCleanup waits for all resources to be fully deleted
func (f *AutoIndexerGCFinalizer) waitForResourceCleanup(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	timeout := time.After(5 * time.Minute) // 5 minute timeout
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for resource cleanup")
		case <-ticker.C:
			if allResourcesDeleted, err := f.checkAllResourcesDeleted(ctx, autoIndexerObj); err != nil {
				klog.ErrorS(err, "failed to check resource deletion status", "autoindexer", klog.KObj(autoIndexerObj))
				continue
			} else if allResourcesDeleted {
				return nil
			}
			klog.InfoS("Still waiting for resources to be deleted", "autoindexer", klog.KObj(autoIndexerObj))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// checkAllResourcesDeleted verifies that all owned resources have been deleted
func (f *AutoIndexerGCFinalizer) checkAllResourcesDeleted(ctx context.Context, autoIndexerObj *kaitov1alpha1.AutoIndexer) (bool, error) {
	// Check Jobs
	jobs := &batchv1.JobList{}
	if err := f.client.List(ctx, jobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name}); err != nil {
		return false, err
	}
	if len(jobs.Items) > 0 {
		return false, nil
	}

	// Check CronJobs
	cronJobs := &batchv1.CronJobList{}
	if err := f.client.List(ctx, cronJobs, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name}); err != nil {
		return false, err
	}
	if len(cronJobs.Items) > 0 {
		return false, nil
	}

	// Check ConfigMaps
	configMaps := &corev1.ConfigMapList{}
	if err := f.client.List(ctx, configMaps, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name}); err != nil {
		return false, err
	}
	if len(configMaps.Items) > 0 {
		return false, nil
	}

	// Check Secrets
	secrets := &corev1.SecretList{}
	if err := f.client.List(ctx, secrets, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name}); err != nil {
		return false, err
	}
	if len(secrets.Items) > 0 {
		return false, nil
	}

	// Check for any orphaned pods that might be left behind
	pods := &corev1.PodList{}
	if err := f.client.List(ctx, pods, client.InNamespace(autoIndexerObj.Namespace), client.MatchingLabels{AutoIndexerNameLabel: autoIndexerObj.Name}); err != nil {
		return false, err
	}
	if len(pods.Items) > 0 {
		klog.InfoS("Found orphaned pods, cleaning up", "count", len(pods.Items), "autoindexer", klog.KObj(autoIndexerObj))
		for _, pod := range pods.Items {
			if err := f.client.Delete(ctx, &pod); err != nil && !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "failed to delete orphaned pod", "pod", klog.KObj(&pod))
			}
		}
		return false, nil
	}

	return true, nil
}

// isAutoIndexerBeingDeleted checks if the AutoIndexer is being deleted
func isAutoIndexerBeingDeleted(autoIndexerObj *kaitov1alpha1.AutoIndexer) bool {
	return !autoIndexerObj.DeletionTimestamp.IsZero()
}

// hasAutoIndexerFinalizer checks if the AutoIndexer has the finalizer
func hasAutoIndexerFinalizer(autoIndexerObj *kaitov1alpha1.AutoIndexer) bool {
	for _, finalizer := range autoIndexerObj.Finalizers {
		if finalizer == consts.AutoIndexerFinalizer {
			return true
		}
	}
	return false
}

// removeAutoIndexerFinalizer removes the finalizer from the AutoIndexer
func removeAutoIndexerFinalizer(ctx context.Context, clientObj client.Client, autoIndexerObj *kaitov1alpha1.AutoIndexer) error {
	patch := client.MergeFrom(autoIndexerObj.DeepCopy())

	// Remove the finalizer
	finalizers := []string{}
	for _, finalizer := range autoIndexerObj.Finalizers {
		if finalizer != consts.AutoIndexerFinalizer {
			finalizers = append(finalizers, finalizer)
		}
	}
	autoIndexerObj.Finalizers = finalizers

	if err := clientObj.Patch(ctx, autoIndexerObj, patch); err != nil {
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}

	klog.InfoS("Successfully removed finalizer from AutoIndexer", "autoindexer", klog.KObj(autoIndexerObj))
	return nil
}
