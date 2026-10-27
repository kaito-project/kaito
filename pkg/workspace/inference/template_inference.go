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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/cache"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/nodeprovision"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/utils/resources"
	"github.com/kaito-project/kaito/pkg/workspace/manifests"
)

func CreateTemplateInference(ctx context.Context, workspaceObj *kaitov1beta1.Workspace, kubeClient client.Client, provisioner nodeprovision.NodeProvisioner) (client.Object, error) {
	ssObj := manifests.GenerateManifestWithPodTemplate(workspaceObj, defaultTolerations(workspaceObj))
	// Pin the pod to nodes provisioned for this workspace. Without this, a
	// custom-template pod could schedule onto a sibling workspace's node when
	// they share the same user label selector (e.g. InferenceSet replicas).
	if err := ApplyProvisionerNodeSelector(ctx, provisioner, workspaceObj, &ssObj.Spec.Template.Spec); err != nil {
		return nil, err
	}

	// Apply cache mutations for template-based workspaces.
	if featuregates.FeatureGates[consts.FeatureFlagDistributedCache] && cache.HasAttachment(workspaceObj) {
		if cacheErr := cache.ApplyTemplateCacheMutations(ctx, workspaceObj, kubeClient, ssObj); cacheErr != nil {
			return nil, cacheErr
		}
	}

	existing := &appsv1.StatefulSet{}
	err := kubeClient.Get(ctx, client.ObjectKeyFromObject(ssObj), existing)
	if apierrors.IsNotFound(err) {
		if err := resources.CreateResource(ctx, client.Object(ssObj), kubeClient); err != nil {
			return nil, err
		}
		return ssObj, nil
	}
	if err != nil {
		return nil, err
	}

	if !cache.MutationHashChanged(existing, ssObj) {
		return existing, nil
	}

	existing.Spec.Template = ssObj.Spec.Template
	if err := kubeClient.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}
