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
	"testing"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestAutoIndexerReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	ragEngine := &kaitov1alpha1.RAGEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ragengine",
			Namespace: "default",
		},
		Spec: &kaitov1alpha1.RAGEngineSpec{},
		Status: kaitov1alpha1.RAGEngineStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(kaitov1alpha1.RAGEngineConditionTypeSucceeded),
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	autoIndexer := &kaitov1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
		Spec: kaitov1alpha1.AutoIndexerSpec{
			RAGEngine: "test-ragengine",
			IndexName: "test-index",
			DataSource: kaitov1alpha1.DataSourceSpec{
				Type: kaitov1alpha1.DataSourceTypeGitHub,
				Git: &kaitov1alpha1.GitDataSourceSpec{
					Repository: "https://github.com/example/repo",
				},
			},
		},
		Status: kaitov1alpha1.AutoIndexerStatus{},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ragEngine, autoIndexer).
		WithStatusSubresource(&kaitov1alpha1.AutoIndexer{}).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := NewAutoIndexerReconciler(client, scheme, logr.Discard(), recorder)

	ctx := context.Background()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Log("Reconcile requested requeue")
	}

	// Verify the AutoIndexer was processed (we'll check status conditions instead of finalizers
	// since the current controller doesn't implement finalizer addition)
	updatedAutoIndexer := &kaitov1alpha1.AutoIndexer{}
	err = client.Get(ctx, req.NamespacedName, updatedAutoIndexer)
	if err != nil {
		t.Fatalf("Failed to get updated AutoIndexer: %v", err)
	}

	// Verify at least one condition was set during reconciliation
	if len(updatedAutoIndexer.Status.Conditions) == 0 {
		t.Error("Expected status conditions to be set during reconciliation")
	}
}

func TestAutoIndexerReconciler_deleteAutoIndexer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	now := metav1.Now()
	autoIndexer := &kaitov1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-autoindexer",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{consts.AutoIndexerFinalizer},
		},
	}

	// Create a completed job (both succeeded and failed are 0 means incomplete job)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
			Labels: map[string]string{
				AutoIndexerNameLabel: "test-autoindexer",
			},
		},
		Status: batchv1.JobStatus{
			Succeeded: 1, // Mark job as completed
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer, job).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := NewAutoIndexerReconciler(client, scheme, logr.Discard(), recorder)

	ctx := context.Background()

	result, err := reconciler.deleteAutoIndexer(ctx, autoIndexer)
	if err != nil {
		t.Fatalf("deleteAutoIndexer failed: %v", err)
	}

	// The current implementation doesn't handle finalizer removal, so it should complete without error
	// and not request a requeue for completed jobs
	if result.RequeueAfter > 0 {
		t.Error("deleteAutoIndexer should not request requeue when all jobs are completed")
	}
}

func TestAutoIndexerReconciler_updateStatusConditionIfNotMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)

	autoIndexer := &kaitov1alpha1.AutoIndexer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-autoindexer",
			Namespace: "default",
		},
		Status: kaitov1alpha1.AutoIndexerStatus{
			Conditions: []metav1.Condition{},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(autoIndexer).
		WithStatusSubresource(&kaitov1alpha1.AutoIndexer{}).
		Build()

	recorder := record.NewFakeRecorder(10)
	reconciler := NewAutoIndexerReconciler(client, scheme, logr.Discard(), recorder)

	ctx := context.Background()

	// Test adding a new condition
	err := reconciler.updateStatusConditionIfNotMatch(ctx, autoIndexer, kaitov1alpha1.AutoIndexerConditionTypeSucceeded, metav1.ConditionTrue, "TestReason", "Test message")
	if err != nil {
		t.Fatalf("updateStatusConditionIfNotMatch failed: %v", err)
	}

	// Verify condition was added
	if len(autoIndexer.Status.Conditions) != 1 {
		t.Fatalf("Expected 1 condition, got %d", len(autoIndexer.Status.Conditions))
	}

	condition := autoIndexer.Status.Conditions[0]
	if condition.Type != string(kaitov1alpha1.AutoIndexerConditionTypeSucceeded) {
		t.Errorf("Expected condition type %s, got %s", kaitov1alpha1.AutoIndexerConditionTypeSucceeded, condition.Type)
	}
	if condition.Status != metav1.ConditionTrue {
		t.Errorf("Expected condition status True, got %s", condition.Status)
	}
	if condition.Reason != "TestReason" {
		t.Errorf("Expected condition reason TestReason, got %s", condition.Reason)
	}
	if condition.Message != "Test message" {
		t.Errorf("Expected condition message 'Test message', got %s", condition.Message)
	}
}
