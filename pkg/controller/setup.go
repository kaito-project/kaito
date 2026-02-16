package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var InferenceServiceGVK = schema.GroupVersionKind{
	Group:   "serving.kserve.io",
	Version: "v1beta1",
	Kind:    "InferenceService",
}

var NodePoolGVK = schema.GroupVersionKind{
	Group:   "karpenter.sh",
	Version: "v1",
	Kind:    "NodePool",
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	isvc := &unstructured.Unstructured{}
	isvc.SetGroupVersionKind(InferenceServiceGVK)

	nodePool := &unstructured.Unstructured{}
	nodePool.SetGroupVersionKind(NodePoolGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(isvc, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		WatchesRawSource(
			source.Kind(mgr.GetCache(), nodePool,
				handler.TypedEnqueueRequestsFromMapFunc(r.nodePoolToInferenceService),
			),
		).
		Named("kaito-kserve-gpu-provisioner").
		Complete(r)
}

func (r *Reconciler) nodePoolToInferenceService(ctx context.Context, obj *unstructured.Unstructured) []reconcile.Request {
	labels := obj.GetLabels()
	if labels == nil {
		return nil
	}

	name, ok := labels[LabelISVCName]
	namespace, nsOk := labels[LabelISVCNamespace]
	if !ok || !nsOk || name == "" || namespace == "" {
		return nil
	}

	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}},
	}
}
