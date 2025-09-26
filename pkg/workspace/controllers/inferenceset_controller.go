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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/nodeclaim"
)

type InferenceSetReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	klogger      klog.Logger
	expectations *utils.ControllerExpectations
}

func NewInferenceSetReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, Recorder record.EventRecorder) *InferenceSetReconciler {
	expectations := utils.NewControllerExpectations()
	return &InferenceSetReconciler{
		Client:       client,
		Scheme:       scheme,
		Log:          log,
		klogger:      klog.NewKlogr().WithName("InferenceSetController"),
		Recorder:     Recorder,
		expectations: expectations,
	}
}

func (c *InferenceSetReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	inferencesetObj := &kaitov1alpha1.InferenceSet{}
	if err := c.Client.Get(ctx, req.NamespacedName, inferencesetObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.klogger, req.String())
			return reconcile.Result{}, nil
		}
		klog.ErrorS(err, "failed to get inference set", "inference set", req.Name)
		return reconcile.Result{}, err
	}

	klog.InfoS("Reconciling", "inference set", req.NamespacedName)

	return c.addOrUpdateInferenceSet(ctx, inferencesetObj)
}

func (c *InferenceSetReconciler) addOrUpdateInferenceSet(ctx context.Context, inferencesetObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *InferenceSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("InferenceSet")

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1beta1.Workspace{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.ControllerRevision{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{}).
		Watches(&karpenterv1.NodeClaim{},
			&nodeClaimEventHandler{
				logger:         c.klogger,
				expectations:   c.expectations,
				enqueueHandler: enqueueWorkspaceForNodeClaim,
			},
			builder.WithPredicates(nodeclaim.NodeClaimPredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	//go monitorWorkspaces(context.Background(), c.Client)
	return builder.Complete(c)
}
