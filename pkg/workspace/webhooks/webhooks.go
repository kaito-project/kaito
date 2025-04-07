// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package webhooks

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/resourcesemantics"
	"knative.dev/pkg/webhook/resourcesemantics/validation"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func NewWorkspaceWebhooks() []knativeinjection.ControllerConstructor {
	return []knativeinjection.ControllerConstructor{
		certificates.NewController,
		NewWorkspaceCRDValidationWebhook,
	}
}

func NewWorkspaceCRDValidationWebhook(ctx context.Context, _ configmap.Watcher) *controller.Impl {
	return validation.NewAdmissionController(ctx,
		"validation.workspace.kaito.sh",
		"/validate/workspace.kaito.sh",
		WorkspaceResources,
		func(ctx context.Context) context.Context { return ctx },
		true,
	)
}

var WorkspaceResources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	kaitov1alpha1.GroupVersion.WithKind("Workspace"): &kaitov1alpha1.Workspace{},
	kaitov1beta1.GroupVersion.WithKind("Workspace"):  &kaitov1beta1.Workspace{},
}
