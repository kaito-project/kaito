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

package v1alpha1

import (
	"context"
	"fmt"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/k8sclient"
)

func (is *InferenceSet) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (is *InferenceSet) Validate(ctx context.Context) (errs *apis.FieldError) {
	errmsgs := validation.IsDNS1123Label(is.Name)
	if len(errmsgs) > 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(errmsgs, ", "), "name"))
	}
	base := apis.GetBaseline(ctx)
	if base == nil {
		klog.InfoS("Validate creation", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		errs = errs.Also(is.validateCreate().ViaField("spec"))
	} else {
		klog.InfoS("Validate update", "inferenceset", fmt.Sprintf("%s/%s", is.Namespace, is.Name))
		old := base.(*InferenceSet)
		errs = errs.Also(
			is.validateUpdate(old).ViaField("spec"),
		)
	}
	return errs
}

func (is *InferenceSet) validateCreate() (errs *apis.FieldError) {
	// Validate replicas is at least 1
	if is.Spec.Replicas < 1 {
		errs = errs.Also(apis.ErrInvalidValue(is.Spec.Replicas, "replicas", "must be at least 1"))
	}
	errs = errs.Also(is.validateBYOPVCAccessMode())
	return errs
}

func (is *InferenceSet) validateUpdate(_ *InferenceSet) (errs *apis.FieldError) {
	errs = errs.Also(is.validateBYOPVCAccessMode())
	return errs
}

// validateBYOPVCAccessMode rejects a ReadWriteOnce BYO PVC when replicas > 1,
// because each InferenceSet replica becomes a separate Workspace/pod that needs
// to mount the same PVC — RWO only allows mounting on a single node.
func (is *InferenceSet) validateBYOPVCAccessMode() (errs *apis.FieldError) {
	if is.Spec.Template.Inference.Preset == nil {
		return errs
	}
	pvcName := is.Spec.Template.Inference.Preset.PresetOptions.ModelWeightsPVC
	if pvcName == "" || is.Spec.Replicas <= 1 {
		return errs
	}
	pvc := &corev1.PersistentVolumeClaim{}
	if err := k8sclient.Client.Get(context.TODO(), types.NamespacedName{
		Name:      pvcName,
		Namespace: is.Namespace,
	}, pvc); err == nil {
		for _, accessMode := range pvc.Spec.AccessModes {
			if accessMode == corev1.ReadWriteOnce {
				errs = errs.Also(apis.ErrInvalidValue(
					fmt.Sprintf(
						"PVC '%s' has ReadWriteOnce access mode but replicas is %d; use a ReadWriteMany PVC for multi-replica InferenceSet",
						pvcName, is.Spec.Replicas),
					"template.inference.presetOptions.modelWeightsPVC",
				).ViaField("spec"))
				break
			}
		}
	}
	return errs
}
