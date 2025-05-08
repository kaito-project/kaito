// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package inference

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

func CreateTemplateInference(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, kubeClient client.Client) (client.Object, error) {
	depObj := manifests.GenerateDeploymentManifestWithPodTemplate(workspaceObj, tolerations)
	err := resources.CreateResource(ctx, client.Object(depObj), kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return depObj, nil
}
