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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
)

func TestReconcile_AlreadyReady(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	cr := &kaitov1alpha1.ModelMirror{
		ObjectMeta: metav1.ObjectMeta{Name: "abc123"},
		Status:     kaitov1alpha1.ModelMirrorStatus{Phase: kaitov1alpha1.ModelMirrorPhaseReady},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()
	r := NewModelMirrorReconciler(client, zap.New(zap.UseDevMode(true)), mmconsts.DefaultDownloadJobResources())

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "abc123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("expected no requeue for Ready CR, got %+v", result)
	}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	cr := &kaitov1alpha1.ModelMirror{
		ObjectMeta: metav1.ObjectMeta{Name: "abc123"},
		Spec: kaitov1alpha1.ModelMirrorSpec{
			Source:       kaitov1alpha1.ModelMirrorSource{Registry: "huggingface", ModelID: "test/model"},
			Storage:      kaitov1alpha1.ModelMirrorStorage{StorageClassName: "blob-nfs", Size: "10Gi"},
			JobNamespace: "default",
		},
		Status: kaitov1alpha1.ModelMirrorStatus{Phase: kaitov1alpha1.ModelMirrorPhasePending},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()
	r := NewModelMirrorReconciler(client, zap.New(zap.UseDevMode(true)), mmconsts.DefaultDownloadJobResources())

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "abc123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Error("expected requeue after adding finalizer")
	}

	// Verify finalizer was added
	updated := &kaitov1alpha1.ModelMirror{}
	_ = client.Get(context.Background(), types.NamespacedName{Name: "abc123"}, updated)
	found := false
	for _, f := range updated.Finalizers {
		if f == mmconsts.ModelMirrorFinalizer {
			found = true
		}
	}
	if !found {
		t.Error("finalizer not added to CR")
	}
}

func TestJobRetryInterval(t *testing.T) {
	if jobRetryInterval != 5*time.Minute {
		t.Errorf("expected 5m retry interval, got %v", jobRetryInterval)
	}
}
