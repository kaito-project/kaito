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

package workspace

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/resourcesemantics"

	"github.com/kaito-project/kaito/api/env"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/internal/inferenceset"
	"github.com/kaito-project/kaito/internal/webhook/admission"
	"github.com/kaito-project/kaito/pkg/featuregates"
)

// Wire the InferenceSet admission webhook to validate the child Workspace it
// would create. Done here (not in api/v1beta1) to avoid an import cycle with the
// util package that builds the Workspace.
func init() {
	kaitov1beta1.ValidateInferenceSetWorkspace = inferenceset.ValidateWorkspaceForInferenceSet
}

// NewControllerWebhooks returns the webhook constructors; every admission
// request context carries e.
func NewControllerWebhooks(e *env.Env) []knativeinjection.ControllerConstructor {
	constructor := []knativeinjection.ControllerConstructor{
		certificates.NewController,
		NewWorkspaceCRDValidationWebhook(e),
	}

	if featuregates.FeatureGates[featuregates.FeatureFlagEnableInferenceSetController] {
		constructor = append(constructor, NewInferenceSetCRDValidationWebhook(e))
	}
	if featuregates.FeatureGates[featuregates.FeatureFlagEnableMultiRoleInferenceController] {
		constructor = append(constructor, NewMultiRoleInferenceCRDValidationWebhook(e))
	}
	if featuregates.FeatureGates[featuregates.FeatureFlagModelMirror] {
		constructor = append(constructor, NewModelMirrorCRDValidationWebhook(e))
	}

	return constructor
}

func NewWorkspaceCRDValidationWebhook(e *env.Env) knativeinjection.ControllerConstructor {
	return admission.NewValidationController(e, "validation.workspace.kaito.sh", "/validate/workspace.kaito.sh", WorkspaceResources)
}

func NewInferenceSetCRDValidationWebhook(e *env.Env) knativeinjection.ControllerConstructor {
	return admission.NewValidationController(e, "validation.inferenceset.kaito.sh", "/validate/inferenceset.kaito.sh", InferenceSetResources)
}

func NewMultiRoleInferenceCRDValidationWebhook(e *env.Env) knativeinjection.ControllerConstructor {
	return admission.NewValidationController(e, "validation.multiroleinference.kaito.sh", "/validate/multiroleinference.kaito.sh", MultiRoleInferenceResources)
}

var WorkspaceResources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	kaitov1alpha1.GroupVersion.WithKind("Workspace"): &kaitov1alpha1.Workspace{},
	kaitov1beta1.GroupVersion.WithKind("Workspace"):  &kaitov1beta1.Workspace{},
}

var InferenceSetResources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	kaitov1alpha1.GroupVersion.WithKind("InferenceSet"): &kaitov1alpha1.InferenceSet{},
	kaitov1beta1.GroupVersion.WithKind("InferenceSet"):  &kaitov1beta1.InferenceSet{},
}

var MultiRoleInferenceResources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	kaitov1alpha1.GroupVersion.WithKind("MultiRoleInference"): &kaitov1alpha1.MultiRoleInference{},
}

func NewModelMirrorCRDValidationWebhook(e *env.Env) knativeinjection.ControllerConstructor {
	return admission.NewValidationController(e, "validation.modelmirror.kaito.sh", "/validate/modelmirror.kaito.sh", ModelMirrorResources)
}

var ModelMirrorResources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	kaitov1alpha1.GroupVersion.WithKind("ModelMirror"): &kaitov1alpha1.ModelMirror{},
}
