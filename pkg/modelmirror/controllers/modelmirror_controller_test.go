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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	mmconsts "github.com/kaito-project/kaito/pkg/modelmirror/consts"
)

// testScheme builds the scheme used by every test in this package.
func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(s)
	_ = batchv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = storagev1.AddToScheme(s)
	return s
}

// newTestReconciler builds a reconciler over the given client, matching how the
// production code constructs it.
func newTestReconciler(c client.Client) *ModelMirrorReconciler {
	return NewModelMirrorReconciler(c, zap.New(zap.UseDevMode(true)), mmconsts.DefaultDownloadJobResources())
}

// newManagedTestCR returns a Managed-mode ModelMirror CR with the minimum spec
// the controller requires (source and storage are both mandatory for Managed).
func newManagedTestCR(name string) *kaitov1alpha1.ModelMirror {
	return &kaitov1alpha1.ModelMirror{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kaitov1alpha1.ModelMirrorSpec{
			Mode:         kaitov1alpha1.ModelMirrorModeManaged,
			JobNamespace: "default",
			Source: &kaitov1alpha1.ModelMirrorSource{
				Registry: "huggingface",
				ModelID:  "microsoft/Phi-3-mini-4k-instruct",
			},
			Storage: &kaitov1alpha1.ModelMirrorStorage{
				Size:             "20Gi",
				StorageClassName: ptr.To("kaito-model-mirror"),
			},
		},
	}
}

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
			Source:       &kaitov1alpha1.ModelMirrorSource{Registry: "huggingface", ModelID: "test/model"},
			Storage:      &kaitov1alpha1.ModelMirrorStorage{StorageClassName: ptr.To("blob-nfs"), Size: "10Gi"},
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

func TestReconcile_Static_SetsReadyNoProvision(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kaitov1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)

	cr := &kaitov1alpha1.ModelMirror{
		ObjectMeta: metav1.ObjectMeta{
			Name: "abc123",
		},
		Spec: kaitov1alpha1.ModelMirrorSpec{
			// A static mirror sets only Mode — no Source, no Storage (BYO storage; nothing to download).
			Mode: kaitov1alpha1.ModelMirrorModeStatic,
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).WithStatusSubresource(cr).Build()
	r := NewModelMirrorReconciler(client, zap.New(zap.UseDevMode(true)), mmconsts.DefaultDownloadJobResources())

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "abc123"}})
	assert.NoError(t, err)

	got := &kaitov1alpha1.ModelMirror{}
	assert.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "abc123"}, got))
	assert.Equal(t, kaitov1alpha1.ModelMirrorPhaseReady, got.Status.Phase)
	// A static mirror stored the weights nowhere locally, so ModelPath is empty.
	assert.Empty(t, got.Status.ModelPath)

	// Both conditions must be True for a static mirror.
	condStatus := func(condType string) metav1.ConditionStatus {
		for _, c := range got.Status.Conditions {
			if c.Type == condType {
				return c.Status
			}
		}
		return ""
	}
	assert.Equal(t, metav1.ConditionTrue, condStatus(mmconsts.ConditionTypeReady), "Ready condition must be True")
	assert.Equal(t, metav1.ConditionTrue, condStatus(mmconsts.ConditionTypeStorageReady), "StorageReady condition must be True")

	pvcs := &corev1.PersistentVolumeClaimList{}
	_ = client.List(context.Background(), pvcs)
	assert.Empty(t, pvcs.Items, "static mirror must not create a PVC")

	jobs := &batchv1.JobList{}
	_ = client.List(context.Background(), jobs)
	assert.Empty(t, jobs.Items, "static mirror must not create a Job")

	assert.Empty(t, got.Finalizers, "static mirror must not add a finalizer")
}

func TestHandleJobSuccess_NoMarkerStillReady(t *testing.T) {
	cr := newManagedTestCR("mirror-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-1-download-abc", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(cr, job).WithStatusSubresource(cr).Build()
	r := newTestReconciler(c)

	_, err := r.handleJobSuccess(context.Background(), cr, job, zap.New(zap.UseDevMode(true)))
	require.NoError(t, err)

	assert.Equal(t, kaitov1alpha1.ModelMirrorPhaseReady, cr.Status.Phase)
	assert.Empty(t, cr.Status.DownloadThroughputMBps)
}

func TestEnsurePVC_PendingSetsStorageReadyFalse(t *testing.T) {
	cr := newManagedTestCR("mirror-1")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-1", Namespace: "default"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(cr, pvc).WithStatusSubresource(cr).Build()
	r := newTestReconciler(c)

	require.NoError(t, r.ensurePVC(context.Background(), cr))

	cond := meta.FindStatusCondition(cr.Status.Conditions, mmconsts.ConditionTypeStorageReady)
	require.NotNil(t, cond, "a pending PVC must set StorageReady, not stay silent")
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, mmconsts.ReasonPVCPending, cond.Reason)
	assert.Contains(t, cond.Message, "Pending")
}

func TestEnsurePVC_CreateFailureSetsConditionAndReturnsError(t *testing.T) {
	cr := newManagedTestCR("mirror-1")
	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(cr).WithStatusSubresource(cr).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					return apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, "mirror-1", errors.New("quota exceeded"))
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).Build()
	r := newTestReconciler(c)

	err := r.ensurePVC(context.Background(), cr)

	assert.Error(t, err, "the error must still propagate so reconcile requeues")
	cond := meta.FindStatusCondition(cr.Status.Conditions, mmconsts.ConditionTypeStorageReady)
	require.NotNil(t, cond)
	assert.Equal(t, mmconsts.ReasonPVCCreateFailed, cond.Reason)
}

func TestClassifyDownloadFailure(t *testing.T) {
	cases := []struct {
		name       string
		podStatus  corev1.PodStatus
		wantReason string
	}{
		{
			name: "OOMKilled",
			podStatus: corev1.PodStatus{
				Phase: corev1.PodFailed,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name: "downloader",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
						Reason: "OOMKilled", ExitCode: 137,
					}},
				}},
			},
			wantReason: mmconsts.ReasonDownloadOOMKilled,
		},
		{
			name: "evicted, node low on ephemeral-storage",
			podStatus: corev1.PodStatus{
				Phase:   corev1.PodFailed,
				Reason:  "Evicted",
				Message: "The node was low on resource: ephemeral-storage. Threshold quantity: 2Gi, available: 1536Mi. ",
			},
			wantReason: mmconsts.ReasonDownloadEvicted,
		},
		{
			name: "evicted, container exceeded local ephemeral storage limit",
			podStatus: corev1.PodStatus{
				Phase:   corev1.PodFailed,
				Reason:  "Evicted",
				Message: `Container downloader exceeded its local ephemeral storage limit "8Gi". `,
			},
			wantReason: mmconsts.ReasonDownloadEvicted,
		},
		{
			name: "evicted, pod ephemeral storage usage exceeds total limit",
			podStatus: corev1.PodStatus{
				Phase:   corev1.PodFailed,
				Reason:  "Evicted",
				Message: "Pod ephemeral local storage usage exceeds the total limit of containers 8Gi. ",
			},
			wantReason: mmconsts.ReasonDownloadEvicted,
		},
		{
			name: "evicted for memory pressure, not disk",
			podStatus: corev1.PodStatus{
				Phase:   corev1.PodFailed,
				Reason:  "Evicted",
				Message: "The node was low on resource: memory. Container downloader was using 9Gi, request is 8Gi, has larger consumption of memory. ",
			},
			wantReason: mmconsts.ReasonDownloadEvicted,
		},
		{
			name: "generic exit 1 is not attributable",
			podStatus: corev1.PodStatus{
				Phase: corev1.PodFailed,
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:  "downloader",
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}},
				}},
			},
			wantReason: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cr := newManagedTestCR("mirror-1")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mirror-1-download-abc",
					Namespace: "default",
					Labels:    map[string]string{"job-name": "mirror-1-download"},
				},
				Status: tc.podStatus,
			}
			c := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(cr, pod).Build()
			r := newTestReconciler(c)

			reason, message := r.classifyDownloadFailure(context.Background(), cr, "mirror-1-download")
			assert.Equal(t, tc.wantReason, reason)
			if tc.podStatus.Reason == "Evicted" {
				assert.Contains(t, message, tc.podStatus.Message)
			}
		})
	}
}

func TestCheckJobStatus_FailedJobSetsReadyFalse(t *testing.T) {
	cr := newManagedTestCR("mm-failed")
	failedJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "mm-failed-download-abcde",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
			Labels:            map[string]string{mmconsts.LabelModelMirrorName: cr.Name},
		},
		Status: batchv1.JobStatus{
			Failed: 4,
			Conditions: []batchv1.JobCondition{{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Message: "BackoffLimitExceeded",
			}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithObjects(cr, failedJob).WithStatusSubresource(cr).Build()
	r := newTestReconciler(c)

	_, err := r.checkJobStatus(context.Background(), cr, zap.New(zap.UseDevMode(true)))
	require.NoError(t, err)

	got := &kaitov1alpha1.ModelMirror{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: cr.Name}, got))

	cond := meta.FindStatusCondition(got.Status.Conditions, mmconsts.ConditionTypeReady)
	require.NotNil(t, cond, "a failed download Job must surface a Ready condition")
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, mmconsts.ReasonDownloadFailed, cond.Reason)
	assert.Contains(t, cond.Message, "BackoffLimitExceeded")
	assert.Contains(t, got.Status.FailureMessage, "BackoffLimitExceeded")
}
