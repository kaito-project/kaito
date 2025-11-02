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
	"slices"
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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/inferenceset"
	"github.com/kaito-project/kaito/pkg/utils/workspace"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
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
	iObj := &kaitov1alpha1.InferenceSet{}
	if err := c.Client.Get(ctx, req.NamespacedName, iObj); err != nil {
		if apierrors.IsNotFound(err) {
			c.expectations.DeleteExpectations(c.klogger, req.String())
			klog.InfoS("Inference set not found, might be deleted already", "inference set", req.Name)
			return reconcile.Result{}, nil
		}
		klog.ErrorS(err, "failed to get inference set", "inference set", req.Name)
		return reconcile.Result{}, err
	}

	klog.InfoS("Reconciling", "inference set", req.NamespacedName, "name", req.Name)
	if iObj.DeletionTimestamp.IsZero() {
		if err := c.ensureFinalizer(ctx, iObj); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Handle deleting inferenceset, garbage collect all the resources.
		return c.deleteInferenceSet(ctx, iObj)
	}

	if err := c.syncControllerRevision(ctx, iObj); err != nil {
		return reconcile.Result{}, err
	}

	return c.addOrUpdateInferenceSet(ctx, iObj)
}

func (c *InferenceSetReconciler) ensureFinalizer(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) error {
	if !controllerutil.ContainsFinalizer(iObj, consts.InferenceSetFinalizer) {
		patch := client.MergeFrom(iObj.DeepCopy())
		controllerutil.AddFinalizer(iObj, consts.InferenceSetFinalizer)
		if err := c.Client.Patch(ctx, iObj, patch); err != nil {
			klog.ErrorS(err, "failed to ensure the finalizer to the inference set", "inference set", klog.KObj(iObj))
			return err
		}
	}
	return nil
}

func (c *InferenceSetReconciler) deleteInferenceSet(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	klog.InfoS("deleteInferenceSet", "inferenceset", klog.KObj(iObj))
	err := inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeDeleting, metav1.ConditionTrue, "inferencesetDeleted", "inferenceset is being deleted")
	if err != nil {
		klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
		return reconcile.Result{}, err
	}

	return c.garbageCollectInferenceSet(ctx, iObj)
}

// garbageCollectInferenceSet remove finalizer associated with inferenceset object.
func (c *InferenceSetReconciler) garbageCollectInferenceSet(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) (ctrl.Result, error) {
	klog.InfoS("garbageCollectInferenceSet", "inferenceset", klog.KObj(iObj))
	// Check if there are any workspaces associated with this inferenceset.
	wsList, err := inferenceset.ListWorkspaces(ctx, iObj, c.Client)
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

	updateErr := inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, iObj, func(ws *kaitov1alpha1.InferenceSet) error {
		controllerutil.RemoveFinalizer(ws, consts.InferenceSetFinalizer)
		return nil
	})
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			return ctrl.Result{}, nil
		}
		klog.ErrorS(updateErr, "failed to update the inferenceset to remove finalizer", "inferenceset", klog.KObj(iObj))
		return ctrl.Result{}, updateErr
	}

	klog.InfoS("successfully removed the inferenceset finalizers", "inferenceset", klog.KObj(iObj))
	return ctrl.Result{}, nil
}

func (c *InferenceSetReconciler) addOrUpdateInferenceSet(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) (reconcile.Result, error) {
	if iObj == nil {
		return reconcile.Result{}, nil
	}

	isKey := client.ObjectKeyFromObject(iObj).String()
	if !c.expectations.SatisfiedExpectations(c.Log, isKey) {
		klog.V(4).InfoS("Waiting for expectations to be satisfied", "inferenceset", isKey)
		return reconcile.Result{}, nil
	}

	// Check if there are any existing workspaces associated with this inferenceset.
	wsList, err := inferenceset.ListWorkspaces(ctx, iObj, c.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	klog.InfoS("Found workspaces for inference set", "name", iObj.Name, "current", len(wsList.Items), "desired", iObj.Spec.Replicas)

	replicaNumToDelete := len(wsList.Items) - iObj.Spec.Replicas
	var deletingWorkspaces []string
	if replicaNumToDelete > 0 {
		klog.InfoS("Found extra workspaces, deleting...", "current", len(wsList.Items), "desired", iObj.Spec.Replicas)
		// first delete workspace that is not in ready state
		for _, ws := range wsList.Items {
			if !ws.DeletionTimestamp.IsZero() {
				deletingWorkspaces = append(deletingWorkspaces, ws.Name)
				replicaNumToDelete--
				klog.InfoS("Skipping workspace that is already being deleted...", "workspace", klog.KObj(&ws))
			} else if controllers.DetermineWorkspacePhase(&ws) != "succeeded" {
				klog.InfoS("Deleting non-ready workspace...", "workspace", klog.KObj(&ws))
				if err := c.Client.Delete(ctx, &ws, &client.DeleteOptions{}); err != nil {
					klog.ErrorS(err, "failed to delete non-ready workspace", "workspace", klog.KObj(&ws))
					return ctrl.Result{}, err
				}
				deletingWorkspaces = append(deletingWorkspaces, ws.Name)
				replicaNumToDelete--
			}
			if replicaNumToDelete <= 0 {
				break
			}
		}

		// delete rest of extra workspaces
		if replicaNumToDelete > 0 {
			for _, ws := range wsList.Items {
				// check whether ws.Name is already in deletingWorkspaces
				if slices.Contains(deletingWorkspaces, ws.Name) {
					continue
				}

				if !ws.DeletionTimestamp.IsZero() {
					replicaNumToDelete--
					klog.InfoS("Skipping workspace that is already being deleted...", "workspace", klog.KObj(&ws))
				} else {
					klog.InfoS("Deleting extra workspace...", "workspace", klog.KObj(&ws))
					if err := c.Client.Delete(ctx, &ws, &client.DeleteOptions{}); err != nil {
						klog.ErrorS(err, "failed to delete extra workspace", "workspace", klog.KObj(&ws))
						return ctrl.Result{}, err
					}
					replicaNumToDelete--
				}
				if replicaNumToDelete <= 0 {
					break
				}
			}
		}

		// After deleting the extra workspaces, we should requeue to wait for the deletion to complete
		if wsList, err = inferenceset.ListWorkspaces(ctx, iObj, c.Client); err != nil {
			return ctrl.Result{}, err
		}
	}

	replicaNumToCreate := iObj.Spec.Replicas - len(wsList.Items)
	if replicaNumToCreate > 0 {
		klog.InfoS("Need to create more workspaces...", "current", len(wsList.Items), "desired", iObj.Spec.Replicas)
		for i := range replicaNumToCreate {
			workspaceObj := &kaitov1beta1.Workspace{}
			workspaceObj.GenerateName = iObj.Name + "-"
			workspaceObj.Namespace = iObj.Namespace
			workspaceObj.Labels = map[string]string{
				consts.WorkspaceCreatedByInferenceSetLabel: iObj.Name,
			}
			workspaceObj.OwnerReferences = []metav1.OwnerReference{
				*metav1.NewControllerRef(iObj, kaitov1alpha1.GroupVersion.WithKind("InferenceSet")),
			}
			workspaceObj.Resource = kaitov1beta1.ResourceSpec{
				InstanceType:  iObj.Spec.Template.Resource.InstanceType,
				LabelSelector: iObj.Spec.Selector,
			}
			workspaceObj.Inference = &iObj.Spec.Template.Inference

			klog.InfoS("creating workspace", "workspace", workspaceObj.Name, "index", i)
			if err := c.Client.Create(ctx, workspaceObj); err != nil {
				klog.ErrorS(err, "failed to create workspace", "workspace", workspaceObj.Name)
				return reconcile.Result{}, err
			}
		}
	}

	// check whether all the workspaces are ready
	readyReplicas := 0
	for _, ws := range wsList.Items {
		if controllers.DetermineWorkspacePhase(&ws) == "succeeded" {
			readyReplicas++
		}
	}

	// update the replicas in the status
	if err = inferenceset.UpdateInferenceSetStatus(ctx, c.Client, &client.ObjectKey{Name: iObj.Name, Namespace: iObj.Namespace}, func(status *kaitov1alpha1.InferenceSetStatus) error {
		status.Replicas = iObj.Spec.Replicas
		status.ReadyReplicas = readyReplicas
		return nil
	}); err != nil {
		klog.ErrorS(err, "failed to update inferenceset replicas", "inferenceset", klog.KObj(iObj))
		return reconcile.Result{}, err
	}

	if readyReplicas == iObj.Spec.Replicas {
		if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeReady, metav1.ConditionTrue,
			"inferencesetReady", "inferenceset is ready"); err != nil {
			klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, err
		}
	} else {
		if err = inferenceset.UpdateStatusConditionIfNotMatch(ctx, c.Client, iObj, kaitov1alpha1.InferenceSetConditionTypeReady, metav1.ConditionFalse,
			"inferencesetNotReady", fmt.Sprintf("inferenceset is not ready, %d/%d replicas are ready", readyReplicas, iObj.Spec.Replicas)); err != nil {
			klog.ErrorS(err, "failed to update inferenceset status", "inferenceset", klog.KObj(iObj))
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (c *InferenceSetReconciler) syncControllerRevision(ctx context.Context, iObj *kaitov1alpha1.InferenceSet) error {
	currentHash := inferenceset.ComputeInferenceSetHash(iObj)
	annotations := iObj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	} // nil checking.

	revisionNum := int64(1)

	revisions := &appsv1.ControllerRevisionList{}
	if err := c.List(ctx, revisions, client.InNamespace(iObj.Namespace), client.MatchingLabels{InferenceSetNameLabel: iObj.Name}); err != nil {
		return fmt.Errorf("failed to list revisions: %w", err)
	}
	sort.Slice(revisions.Items, func(i, j int) bool {
		return revisions.Items[i].Revision < revisions.Items[j].Revision
	})

	var latestRevision *appsv1.ControllerRevision

	jsonData, err := inferenceset.MarshalInferenceSetFields(iObj)
	if err != nil {
		return fmt.Errorf("failed to marshal revision data: %w", err)
	}

	if len(revisions.Items) > 0 {
		latestRevision = &revisions.Items[len(revisions.Items)-1]
		revisionNum = latestRevision.Revision + 1
	}
	newRevision := &appsv1.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", iObj.Name, currentHash[:revisionHashSuffix]),
			Namespace: iObj.Namespace,
			Annotations: map[string]string{
				InferenceSetHashAnnotation: currentHash,
			},
			Labels: map[string]string{
				InferenceSetNameLabel: iObj.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(iObj, kaitov1alpha1.GroupVersion.WithKind("InferenceSet")),
			},
		},
		Revision: revisionNum,
		Data:     runtime.RawExtension{Raw: jsonData},
	}

	annotations[InferenceSetHashAnnotation] = currentHash
	iObj.SetAnnotations(annotations)
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

	err = inferenceset.UpdateInferenceSetWithRetry(ctx, c.Client, iObj, func(ws *kaitov1alpha1.InferenceSet) error {
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
		Watches(&kaitov1beta1.Workspace{},
			&workspaceEventHandler{
				logger:         c.klogger,
				expectations:   c.expectations,
				enqueueHandler: enqueueInferenceSetForWorkspace,
			},
			builder.WithPredicates(workspace.WorkspacePredicate),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: 5})

	go monitorInferenceSets(context.Background(), c.Client)
	return builder.Complete(c)
}
