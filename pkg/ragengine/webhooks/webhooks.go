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

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func NewRAGEngineWebhooks() []knativeinjection.ControllerConstructor {
	return []knativeinjection.ControllerConstructor{
		certificates.NewController,
		NewRAGEngineCRDValidationWebhook,
	}
}

func NewRAGEngineCRDValidationWebhook(ctx context.Context, _ configmap.Watcher) *controller.Impl {
	return validation.NewAdmissionController(ctx,
		"validation.ragengine.kaito.sh",
		"/validate/ragengine.kaito.sh",
		RAGEngineResources,
		func(ctx context.Context) context.Context { return ctx },
		true,
	)
}

var RAGEngineResources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	kaitov1beta1.GroupVersion.WithKind("RAGEngine"): &kaitov1beta1.RAGEngine{},
}
