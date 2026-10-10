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

package inference

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

// GenerateTemplateInference builds the StatefulSet for a workspace whose inference
// workload is defined by a custom pod template.
func GenerateTemplateInference(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, provisioner nodeprovision.NodeProvisioner) (*appsv1.StatefulSet, error) {
	ssObj := manifests.GenerateManifestWithPodTemplate(workspaceObj, defaultTolerations(workspaceObj))
	// Pin the pod to nodes provisioned for this workspace. Without this, a
	// custom-template pod could schedule onto a sibling workspace's node when
	// they share the same user label selector (e.g. InferenceSet replicas).
	if err := ApplyProvisionerNodeSelector(ctx, provisioner, workspaceObj, &ssObj.Spec.Template.Spec); err != nil {
		return nil, err
	}
	if revision, ok := workspaceObj.Annotations[kaitov1beta1.WorkspaceRevisionAnnotation]; ok {
		ssObj.Annotations = map[string]string{
			kaitov1beta1.WorkspaceRevisionAnnotation: revision,
		}
	}
	return ssObj, nil
}

func CreateTemplateInference(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, kubeClient client.Client, provisioner nodeprovision.NodeProvisioner) (client.Object, error) {
	ssObj, err := GenerateTemplateInference(ctx, workspaceObj, provisioner)
	if err != nil {
		return nil, err
	}
	err = resources.CreateResource(ctx, client.Object(ssObj), kubeClient)
	if client.IgnoreAlreadyExists(err) != nil {
		return nil, err
	}
	return ssObj, nil
}
