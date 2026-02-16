package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kaito-project/kaito/pkg/presets"
)

const (
	AnnotationModelPreset   = "kaito.sh/model-preset"
	AnnotationCloudProvider = "kaito.sh/cloud-provider"
	AnnotationNodePool      = "kaito.sh/nodepool"
	AnnotationGPUCount      = "kaito.sh/gpu-count"
	AnnotationInstanceType  = "kaito.sh/instance-type"
	LabelManagedBy          = "kaito.sh/managed-by"
	LabelISVCName           = "kaito.sh/isvc-name"
	LabelISVCNamespace      = "kaito.sh/isvc-namespace"
	FinalizerName           = "kaito.sh/gpu-provisioner"
	NodePoolPrefix          = "kaito-"
	ManagedByValue          = "kaito"
)

type Reconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DefaultProvider string
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	isvc := &unstructured.Unstructured{}
	isvc.SetGroupVersionKind(InferenceServiceGVK)
	if err := r.Get(ctx, req.NamespacedName, isvc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	annotations := isvc.GetAnnotations()
	modelName, ok := annotations[AnnotationModelPreset]
	if !ok || modelName == "" {
		return ctrl.Result{}, nil
	}

	if isvc.GetDeletionTimestamp() != nil {
		if containsFinalizer(isvc, FinalizerName) {
			if err := r.cleanupNodePool(ctx, req.Namespace, req.Name); err != nil {
				logger.Error(err, "failed to cleanup NodePool during deletion")
				return ctrl.Result{}, err
			}
			removeFinalizer(isvc, FinalizerName)
			if err := r.Update(ctx, isvc); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !containsFinalizer(isvc, FinalizerName) {
		addFinalizer(isvc, FinalizerName)
		if err := r.Update(ctx, isvc); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	preset, found := presets.Get(modelName)
	if !found {
		logger.Info("unknown model preset, skipping", "model", modelName)
		return ctrl.Result{}, nil
	}

	provider := r.DefaultProvider
	if p, ok := annotations[AnnotationCloudProvider]; ok && p != "" {
		provider = p
	}

	logger.Info("reconciling InferenceService", "model", modelName, "provider", provider, "gpus", preset.GPUCount)

	npName, err := r.ensureNodePool(ctx, isvc, preset, provider)
	if err != nil {
		logger.Error(err, "failed to ensure NodePool")
		return ctrl.Result{}, err
	}

	if err := r.patchInferenceService(ctx, isvc, preset, provider, npName); err != nil {
		logger.Error(err, "failed to patch InferenceService")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureNodePool(ctx context.Context, isvc *unstructured.Unstructured, preset presets.Preset, provider string) (string, error) {
	npName := nodePoolName(isvc.GetNamespace(), isvc.GetName())

	var instanceTypes []string

	// Allow user override via annotation.
	if override, ok := isvc.GetAnnotations()[AnnotationInstanceType]; ok && override != "" {
		instanceTypes = []string{override}
	} else {
		switch provider {
		case "aks", "azure":
			instanceTypes = preset.AzureInstanceTypes
		default:
			instanceTypes = preset.AWSInstanceTypes
		}
	}
	if len(instanceTypes) == 0 {
		return "", fmt.Errorf("no instance types for provider %q and model %q", provider, preset.ModelName)
	}

	instanceTypeValues := make([]interface{}, len(instanceTypes))
	for i, t := range instanceTypes {
		instanceTypeValues[i] = t
	}

	var nodeClassGroup, nodeClassKind string
	switch provider {
	case "aks", "azure":
		nodeClassGroup = "karpenter.azure.com"
		nodeClassKind = "AKSNodeClass"
	default:
		nodeClassGroup = "karpenter.k8s.aws"
		nodeClassKind = "EC2NodeClass"
	}

	np := &unstructured.Unstructured{}
	np.SetGroupVersionKind(NodePoolGVK)
	np.SetName(npName)
	np.SetLabels(map[string]string{
		LabelManagedBy:     ManagedByValue,
		LabelISVCName:      isvc.GetName(),
		LabelISVCNamespace: isvc.GetNamespace(),
	})

	np.Object["spec"] = map[string]interface{}{
		"template": map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]interface{}{
					LabelManagedBy:     ManagedByValue,
					LabelISVCName:      isvc.GetName(),
					LabelISVCNamespace: isvc.GetNamespace(),
				},
			},
			"spec": map[string]interface{}{
				"requirements": []interface{}{
					map[string]interface{}{
						"key":      "node.kubernetes.io/instance-type",
						"operator": "In",
						"values":   instanceTypeValues,
					},
					map[string]interface{}{
						"key":      "kubernetes.io/arch",
						"operator": "In",
						"values":   []interface{}{"amd64"},
					},
					map[string]interface{}{
						"key":      "karpenter.sh/capacity-type",
						"operator": "In",
						"values":   []interface{}{"on-demand"},
					},
				},
				"nodeClassRef": map[string]interface{}{
					"group": nodeClassGroup,
					"kind":  nodeClassKind,
					"name":  "default",
				},
			},
		},
		"limits": map[string]interface{}{
			"nvidia.com/gpu": strconv.Itoa(preset.GPUCount),
		},
		"disruption": map[string]interface{}{
			"consolidationPolicy": "WhenEmptyOrUnderutilized",
			"consolidateAfter":    "300s",
		},
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(NodePoolGVK)
	err := r.Get(ctx, types.NamespacedName{Name: npName}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return npName, r.Create(ctx, np)
		}
		return "", err
	}

	np.SetResourceVersion(existing.GetResourceVersion())
	return npName, r.Update(ctx, np)
}

func (r *Reconciler) patchInferenceService(ctx context.Context, isvc *unstructured.Unstructured, preset presets.Preset, provider string, npName string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &unstructured.Unstructured{}
		fresh.SetGroupVersionKind(InferenceServiceGVK)
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: isvc.GetNamespace(),
			Name:      isvc.GetName(),
		}, fresh); err != nil {
			return err
		}

		base := fresh.DeepCopy()

		nodeSelector := map[string]interface{}{
			LabelISVCName:      isvc.GetName(),
			LabelISVCNamespace: isvc.GetNamespace(),
		}
		if err := unstructured.SetNestedField(fresh.Object, nodeSelector, "spec", "predictor", "nodeSelector"); err != nil {
			return fmt.Errorf("setting nodeSelector: %w", err)
		}

		tolerations := []interface{}{
			map[string]interface{}{
				"key":      "nvidia.com/gpu",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
			map[string]interface{}{
				"key":      "kaito.sh/gpu-provisioner",
				"operator": "Exists",
				"effect":   "NoSchedule",
			},
		}
		if err := unstructured.SetNestedSlice(fresh.Object, tolerations, "spec", "predictor", "tolerations"); err != nil {
			return fmt.Errorf("setting tolerations: %w", err)
		}

		if preset.IsContainerImage() {
			if err := r.patchContainerImagePredictor(fresh, preset); err != nil {
				return err
			}
		} else {
			if err := r.patchHuggingFacePredictor(fresh, preset); err != nil {
				return err
			}
		}

		ann := fresh.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}
		ann[AnnotationNodePool] = npName
		ann[AnnotationGPUCount] = strconv.Itoa(preset.GPUCount)
		fresh.SetAnnotations(ann)

		return r.Patch(ctx, fresh, client.MergeFrom(base))
	})
}

func (r *Reconciler) patchContainerImagePredictor(fresh *unstructured.Unstructured, preset presets.Preset) error {
	unstructured.RemoveNestedField(fresh.Object, "spec", "predictor", "model")

	container := map[string]interface{}{
		"name":  "kserve-container",
		"image": preset.ContainerImage,
		"resources": map[string]interface{}{
			"limits": map[string]interface{}{
				"nvidia.com/gpu": strconv.Itoa(preset.GPUCount),
			},
			"requests": map[string]interface{}{
				"cpu":    "2",
				"memory": "8Gi",
			},
		},
		"ports": []interface{}{
			map[string]interface{}{
				"containerPort": int64(preset.ServingPort()),
				"name":          "http",
				"protocol":      "TCP",
			},
		},
	}

	if len(preset.ContainerCommand) > 0 {
		cmd := make([]interface{}, len(preset.ContainerCommand))
		for i, c := range preset.ContainerCommand {
			cmd[i] = c
		}
		container["command"] = cmd
	}

	if len(preset.ContainerArgs) > 0 {
		args := make([]interface{}, len(preset.ContainerArgs))
		for i, a := range preset.ContainerArgs {
			args[i] = a
		}
		container["args"] = args
	}

	if len(preset.ContainerEnv) > 0 {
		envVars := make([]interface{}, 0, len(preset.ContainerEnv))
		for k, v := range preset.ContainerEnv {
			envVars = append(envVars, map[string]interface{}{
				"name":  k,
				"value": v,
			})
		}
		container["env"] = envVars
	}

	container["volumeMounts"] = []interface{}{
		map[string]interface{}{
			"name":      "shm",
			"mountPath": "/dev/shm",
		},
	}

	if err := unstructured.SetNestedSlice(fresh.Object,
		[]interface{}{container},
		"spec", "predictor", "containers"); err != nil {
		return fmt.Errorf("setting container image predictor: %w", err)
	}

	volumes := []interface{}{
		map[string]interface{}{
			"name": "shm",
			"emptyDir": map[string]interface{}{
				"medium":    "Memory",
				"sizeLimit": "16Gi",
			},
		},
	}
	if err := unstructured.SetNestedSlice(fresh.Object, volumes, "spec", "predictor", "volumes"); err != nil {
		return fmt.Errorf("setting volumes: %w", err)
	}

	return nil
}

func (r *Reconciler) patchHuggingFacePredictor(fresh *unstructured.Unstructured, preset presets.Preset) error {
	gpuLimit := map[string]interface{}{
		"nvidia.com/gpu": strconv.Itoa(preset.GPUCount),
	}
	if err := unstructured.SetNestedField(fresh.Object, gpuLimit, "spec", "predictor", "model", "resources", "limits"); err != nil {
		containers, found, cErr := unstructured.NestedSlice(fresh.Object, "spec", "predictor", "containers")
		if cErr != nil || !found || len(containers) == 0 {
			return fmt.Errorf("setting GPU resource limits: model path failed (%w), no containers found", err)
		}
		container, ok := containers[0].(map[string]interface{})
		if !ok {
			return fmt.Errorf("setting GPU resource limits: containers[0] is not a map")
		}
		resources, _ := container["resources"].(map[string]interface{})
		if resources == nil {
			resources = map[string]interface{}{}
		}
		resources["limits"] = gpuLimit
		container["resources"] = resources
		containers[0] = container
		if err := unstructured.SetNestedSlice(fresh.Object, containers, "spec", "predictor", "containers"); err != nil {
			return fmt.Errorf("setting GPU resource limits on containers[0]: %w", err)
		}
	}
	return nil
}

func (r *Reconciler) cleanupNodePool(ctx context.Context, namespace, name string) error {
	npName := nodePoolName(namespace, name)

	np := &unstructured.Unstructured{}
	np.SetGroupVersionKind(NodePoolGVK)
	np.SetName(npName)

	err := r.Get(ctx, types.NamespacedName{Name: npName}, np)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	labels := np.GetLabels()
	if labels[LabelManagedBy] != ManagedByValue {
		return nil
	}

	return r.Delete(ctx, np)
}

func nodePoolName(namespace, name string) string {
	full := NodePoolPrefix + namespace + "-" + name
	if len(full) <= 63 {
		return full
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(namespace+"/"+name)))[:8]
	maxPrefix := 63 - len(hash) - 1
	return full[:maxPrefix] + "-" + hash
}

func containsFinalizer(obj *unstructured.Unstructured, finalizer string) bool {
	for _, f := range obj.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

func addFinalizer(obj *unstructured.Unstructured, finalizer string) {
	obj.SetFinalizers(append(obj.GetFinalizers(), finalizer))
}

func removeFinalizer(obj *unstructured.Unstructured, finalizer string) {
	var result []string
	for _, f := range obj.GetFinalizers() {
		if f != finalizer {
			result = append(result, f)
		}
	}
	obj.SetFinalizers(result)
}
