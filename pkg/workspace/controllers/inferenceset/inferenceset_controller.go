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

package inferenceset

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/inferenceset"
)

const (
	InferenceSetHashAnnotation = "inferenceset.kaito.io/hash"
	InferenceSetNameLabel      = "inferenceset.kaito.io/name"
	revisionHashSuffix         = 5
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
	isObj := &kaitov1alpha1.InferenceSet{}
	if err := c.Client.Get(ctx, req.NamespacedName, isObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.klogger, req.String())
			return reconcile.Result{}, nil
		}
		klog.ErrorS(err, "failed to get inference set", "inference set", req.Name)
		return reconcile.Result{}, err
	}

	klog.InfoS("Reconciling", "inference set", req.NamespacedName)
	if isObj.DeletionTimestamp.IsZero() {
		if err := c.ensureFinalizer(ctx, isObj); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Handle deleting inferenceset, garbage collect all the resources.
		return c.deleteInferenceSet(ctx, isObj)
	}

	if err := c.syncControllerRevision(ctx, isObj); err != nil {
		return reconcile.Result{}, err
	}

	return c.addOrUpdateInferenceSet(ctx, isObj)
}

func (c *InferenceSetReconciler) ensureFinalizer(ctx context.Context, isObj *kaitov1alpha1.InferenceSet) error {
	if !controllerutil.ContainsFinalizer(isObj, consts.InferenceSetFinalizer) {
		patch := client.MergeFrom(isObj.DeepCopy())
		controllerutil.AddFinalizer(isObj, consts.InferenceSetFinalizer)
		if err := c.Client.Patch(ctx, isObj, patch); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer to the inference set", "inference set", klog.KObj(isObj))
			return err
		}
	}
	return nil
}

func (c *InferenceSetReconciler) deleteInferenceSet(ctx context.Context, isObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	klog.InfoS("deleteInferenceSet", "inferenceset", klog.KObj(isObj))
	err := inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, isObj, kaitov1alpha1.InferenceSetConditionTypeDeleting, metav1.ConditionTrue, "inferencesetDeleted", "inferenceset is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(isObj))
		return reconcile.Result{}, err
	}

	return c.garbageCollectInferenceSet(ctx, isObj)
}

// garbageCollectInferenceSet remove finalizer associated with inferenceset object.
func (c *InferenceSetReconciler) garbageCollectInferenceSet(ctx context.Context, isObj *kaitov1alpha1.InferenceSet) (ctrl.Result, error) {
	klog.InfoS("garbageCollectInferenceSet", "inferenceset", klog.KObj(isObj))
	// Check if there are any workspaces associated with this inferenceset.
	wsList, err := inferenceset.ListWorkspaces(ctx, isObj, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// We should delete all the workspaces that are created by this inferenceset
	for i := range wsList.Items {
		if wsList.Items[i].DeletionTimestamp.IsZero() {
			klog.InfoS("Deleting associated Workspace...", "workspace", wsList.Items[i].Name)
			if deleteErr := c.Delete(ctx, &wsList.Items[i], &client.DeleteOptions{}); deleteErr != nil {
				klog.ErrorS(deleteErr, "failed to delete the workspace", "workspace", klog.KObj(&wsList.Items[i]))
				return ctrl.Result{}, deleteErr
			}
		}
	}

	updateErr := inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, isObj, func(ws *kaitov1alpha1.InferenceSet) error {
		controllerutil.RemoveFinalizer(ws, consts.InferenceSetFinalizer)
		return nil
	})
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			return ctrl.Result{}, nil
		}
		klog.ErrorS(updateErr, "failed to update the inferenceset to remove finalizer", "inferenceset", klog.KObj(isObj))
		return ctrl.Result{}, updateErr
	}

	klog.InfoS("successfully removed the inferenceset finalizers", "inferenceset", klog.KObj(isObj))
	return ctrl.Result{}, nil
}

func (c *InferenceSetReconciler) addOrUpdateInferenceSet(ctx context.Context, isObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	if isObj == nil {
		return reconcile.Result{}, nil
	}

	// Check if there are any existing workspaces associated with this inferenceset.
	wsList, err := inferenceset.ListWorkspaces(ctx, isObj, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	klog.InfoS("Found workspaces for inference set", "name", isObj.Name, "current", len(wsList.Items), "desired", isObj.Spec.Replicas)

	if len(wsList.Items) > isObj.Spec.Replicas {
		// We should delete the extra workspaces that are created by this inferenceset
		klog.InfoS("Found extra workspaces, deleting...", "current", len(wsList.Items), "desired", isObj.Spec.Replicas)
		for i := isObj.Spec.Replicas; i < len(wsList.Items); i++ {
			deleteWorkspaceName := isObj.Name + "-" + strconv.Itoa(i)
			wsObj := inferenceset.GetWorkspace(deleteWorkspaceName, wsList)
			if wsObj == nil {
				klog.InfoS("Workspace not found, skipping...", "workspace", deleteWorkspaceName)
				continue
			}
			if wsObj.DeletionTimestamp.IsZero() {
				klog.InfoS("Deleting extra workspace...", "workspace", wsObj.Name)
				if err := c.Client.Delete(ctx, wsObj, &client.DeleteOptions{}); err != nil {
					klog.ErrorS(err, "failed to delete extra workspace", "workspace", klog.KObj(wsObj))
					return ctrl.Result{}, err
				}
			}
		}

		// After deleting the extra workspaces, we should requeue to wait for the deletion to complete
		if wsList, err = inferenceset.ListWorkspaces(ctx, isObj, c.Client); err != nil {
			return ctrl.Result{}, err
		}
	}

	klog.InfoS("begin to create/update workspaces for inference set", "name", isObj.Name, "replicas", isObj.Spec.Replicas)
	for i := 0; i < isObj.Spec.Replicas; i++ {
		createWorkspace := false
		workspaceName := isObj.Name + "-" + strconv.Itoa(i)
		workspaceObj := inferenceset.GetWorkspace(workspaceName, wsList)
		if workspaceObj == nil {
			workspaceObj = &kaitov1beta1.Workspace{}
			createWorkspace = true
		}

		workspaceObj.Namespace = isObj.Namespace
		workspaceObj.Name = workspaceName

		workspaceObj.Labels = map[string]string{
			consts.InferenceSetMemberLabel:             workspaceObj.Name,
			consts.WorkspaceCreatedByInferenceSetLabel: isObj.Name,
		}
		workspaceObj.Resource = kaitov1beta1.ResourceSpec{
			InstanceType:  isObj.Spec.Template.Resource.InstanceType,
			LabelSelector: isObj.Spec.Selector,
		}
		workspaceObj.Inference = &isObj.Spec.Template.Inference

		if createWorkspace {
			klog.InfoS("creating workspace", "workspace", workspaceObj.Name, "index", i)
			if err := c.Client.Create(ctx, workspaceObj); err != nil {
				klog.ErrorS(err, "failed to create workspace", "workspace", workspaceObj.Name)
				return reconcile.Result{}, err
			}
		} else {
			klog.InfoS("updating workspace", "workspace", workspaceObj.Name, "index", i)
			if err := c.Client.Update(ctx, workspaceObj); err != nil {
				klog.ErrorS(err, "failed to update workspace", "workspace", workspaceObj.Name)
				return reconcile.Result{}, err
			}
		}
	}

	// update the replicas in the status
	if err = inferenceset.UpdateInferenceSetStatus(ctx, c.Client, &client.ObjectKey{Name: isObj.Name, Namespace: isObj.Namespace}, func(status *kaitov1alpha1.InferenceSetStatus) error {
		status.Replicas = isObj.Spec.Replicas
		status.ReadyReplicas = isObj.Spec.Replicas
		return nil
	}); err != nil {
		klog.ErrorS(err, "failed to update inferenceset replicas", "inferenceset", klog.KObj(isObj))
		return reconcile.Result{}, err
	}

	if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, isObj, kaitov1alpha1.InferenceSetConditionTypeSucceeded, metav1.ConditionTrue,
		"inferencesetSucceeded", "inferenceset succeeds"); err != nil {
		klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(isObj))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (c *InferenceSetReconciler) syncControllerRevision(ctx context.Context, isObj *kaitov1alpha1.InferenceSet) error {
	currentHash := inferenceset.ComputeInferenceSetHash(isObj)
	annotations := isObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	} // nil checking.

	revisionNum := int64(1)

	revisions := &appsv1.ControllerRevisionList{}
	if err := c.List(ctx, revisions, client.InNamespace(isObj.Namespace), client.MatchingLabels{InferenceSetNameLabel: isObj.Name}); err != nil {
		return fmt.Errorf("failed to list revisions: %w", err)
	}
	sort.Slice(revisions.Items, func(i, j int) bool {
		return revisions.Items[i].Revision < revisions.Items[j].Revision
	})

	var latestRevision *appsv1.ControllerRevision

	jsonData, err := inferenceset.MarshalInferenceSetFields(isObj)
	if err != nil {
		return fmt.Errorf("failed to marshal revision data: %w", err)
	}

	if len(revisions.Items) > 0 {
		latestRevision = &revisions.Items[len(revisions.Items)-1]

		revisionNum = latestRevision.Revision + 1
	}
	newRevision := &appsv1.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", isObj.Name, currentHash[:revisionHashSuffix]),
			Namespace: isObj.Namespace,
			Annotations: map[string]string{
				InferenceSetHashAnnotation: currentHash,
			},
			Labels: map[string]string{
				InferenceSetNameLabel: isObj.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(isObj, kaitov1alpha1.GroupVersion.WithKind("InferenceSet")),
			},
		},
		Revision: revisionNum,
		Data:     runtime.RawExtension{Raw: jsonData},
	}

	annotations[InferenceSetHashAnnotation] = currentHash
	isObj.SetAnnotations(annotations)
	controllerRevision := &appsv1.ControllerRevision{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      newRevision.Name,
		Namespace: newRevision.Namespace,
	}, controllerRevision); err != nil {
		if apierrors.IsNotFound(err) {
			if err := c.Create(ctx, newRevision); err != nil {
				return fmt.Errorf("failed to create new ControllerRevision: %w", err)
			} else {
				annotations[kaitov1alpha1.InferenceSetRevisionAnnotation] = strconv.FormatInt(revisionNum, 10)
			}

			if len(revisions.Items) > consts.MaxRevisionHistoryLimit {
				if err := c.Delete(ctx, &revisions.Items[0]); err != nil {
					return fmt.Errorf("failed to delete old revision: %w", err)
				}
			}
		} else {
			return fmt.Errorf("failed to get controller revision: %w", err)
		}
	} else {
		if controllerRevision.Annotations[InferenceSetHashAnnotation] != newRevision.Annotations[InferenceSetHashAnnotation] {
			return fmt.Errorf("revision name conflicts, the hash values are different, old hash: %s, new hash: %s", controllerRevision.Annotations[InferenceSetHashAnnotation], newRevision.Annotations[InferenceSetHashAnnotation])
		}
		annotations[kaitov1alpha1.InferenceSetRevisionAnnotation] = strconv.FormatInt(controllerRevision.Revision, 10)
	}
	annotations[InferenceSetHashAnnotation] = currentHash

	err = inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, isObj, func(ws *kaitov1alpha1.InferenceSet) error {
		ws.SetAnnotations(annotations)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update InferenceSet annotations: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *InferenceSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("InferenceSet")

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1alpha1.InferenceSet{}).
		Owns(&appsv1.ControllerRevision{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	go monitorInferenceSets(context.Background(), c.Client)
	return builder.Complete(c)
}
