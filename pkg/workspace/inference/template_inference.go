// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateTemplateInference(ctx context.Context, workspaceObj *kaitov1alpha1.Workspace, kubeClient client.Client) (client.Object, error) {
	depObj := manifests.GenerateDeploymentManifestWithPodTemplate(ctx, workspaceObj, tolerations)
	err := resources.CreateResource(ctx, client.Object(depObj), kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
