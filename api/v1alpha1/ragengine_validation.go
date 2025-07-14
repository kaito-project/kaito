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
	"net/url"
	"os"
	"regexp"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func (w *RAGEngine) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (w *RAGEngine) Validate(ctx context.Context) (errs *apis.FieldError) {
	base := apis.GetBaseline(ctx)
	if base == nil {
		klog.InfoS("Validate creation", "ragengine", fmt.Sprintf("%s/%s", w.Namespace, w.Name))
		errs = errs.Also(w.validateCreate().ViaField("spec"))
	} else {
		klog.InfoS("Validate update", "ragengine", fmt.Sprintf("%s/%s", w.Namespace, w.Name))
		old := base.(*RAGEngine)
		errs = errs.Also(
			w.validateCreate().ViaField("spec"),
			w.Spec.Compute.validateUpdate(old.Spec.Compute).ViaField("resource"),
		)
	}
	return errs
}

func (w *RAGEngine) validateCreate() (errs *apis.FieldError) {
	if w.Spec.InferenceService == nil {
		errs = errs.Also(apis.ErrGeneric("InferenceService must be specified", ""))
	}
	errs = errs.Also(w.Spec.InferenceService.validateCreate())
	if w.Spec.Embedding == nil {
		errs = errs.Also(apis.ErrGeneric("Embedding must be specified", ""))
		return errs
	}
	if w.Spec.Embedding.Local == nil && w.Spec.Embedding.Remote == nil {
		errs = errs.Also(apis.ErrGeneric("Either remote embedding or local embedding must be specified, not neither", ""))
	}
	if w.Spec.Embedding.Local != nil && w.Spec.Embedding.Remote != nil {
		errs = errs.Also(apis.ErrGeneric("Either remote embedding or local embedding must be specified, but not both", ""))
	}
	errs = errs.Also(w.Spec.Compute.validateRAGCreate())
	if w.Spec.Embedding.Local != nil {
		w.Spec.Embedding.Local.validateCreate().ViaField("embedding")
	}
	if w.Spec.Embedding.Remote != nil {
		w.Spec.Embedding.Remote.validateCreate().ViaField("embedding")
	}

	return errs
}

func (r *ResourceSpec) validateRAGCreate() (errs *apis.FieldError) {
	instanceType := string(r.InstanceType)

	skuHandler, err := utils.GetSKUHandler()
	if err != nil {
		errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("Failed to get SKU handler: %v", err), "instanceType"))
		return errs
	}

	if skuConfig := skuHandler.GetGPUConfigBySKU(instanceType); skuConfig == nil {
		provider := os.Getenv("CLOUD_PROVIDER")
		// Check for other instance types pattern matches if cloud provider is Azure
		if provider != consts.AzureCloudName || (!strings.HasPrefix(instanceType, N_SERIES_PREFIX) && !strings.HasPrefix(instanceType, D_SERIES_PREFIX)) {
			errs = errs.Also(apis.ErrInvalidValue(fmt.Sprintf("Unsupported instance type %s. Supported SKUs: %s", instanceType, skuHandler.GetSupportedSKUs()), "instanceType"))
		}
	}

	// Validate labelSelector
	if _, err := metav1.LabelSelectorAsMap(r.LabelSelector); err != nil {
		errs = errs.Also(apis.ErrInvalidValue(err.Error(), "labelSelector"))
	}

	return errs
}

func (e *LocalEmbeddingSpec) validateCreate() (errs *apis.FieldError) {
	if e.Image == "" && e.ModelID == "" {
		errs = errs.Also(apis.ErrGeneric("Either image or modelID must be specified, not neither", ""))
	}
	if e.Image != "" && e.ModelID != "" {
		errs = errs.Also(apis.ErrGeneric("Either image or modelID must be specified, but not both", ""))
	}
	if e.Image != "" {
		re := regexp.MustCompile(`^(.+/[^:/]+):([^:/]+)$`)
		if !re.MatchString(e.Image) {
			errs = errs.Also(apis.ErrInvalidValue("Invalid image format, require full input image URL", "Image"))
		} else {
			// Executes if image is of correct format
			err := utils.ExtractAndValidateRepoName(e.Image)
			if err != nil {
				errs = errs.Also(apis.ErrInvalidValue(err.Error(), "Image"))
			}
		}
	}
	return errs
}

func (e *RemoteEmbeddingSpec) validateCreate() (errs *apis.FieldError) {
	_, err := url.ParseRequestURI(e.URL)
	if err != nil {
		errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("URL input error: %v", err), "remote url"))
	}
	return errs
}

func (e *InferenceServiceSpec) validateCreate() (errs *apis.FieldError) {
	_, err := url.ParseRequestURI(e.URL)
	if err != nil {
		errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("URL input error: %v", err), "remote url"))
	}
	return errs
}
