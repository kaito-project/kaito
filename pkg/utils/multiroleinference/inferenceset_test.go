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

package multiroleinference

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	inferencesetutil "github.com/kaito-project/kaito/pkg/utils/inferenceset"
)

func int32Ptr(v int32) *int32 { return &v }

func newValidMRI() *kaitov1alpha1.MultiRoleInference {
	return &kaitov1alpha1.MultiRoleInference{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mri",
			Namespace: "default",
			Annotations: map[string]string{
				kaitov1alpha1.AnnotationBypassResourceChecks: "true",
			},
		},
		Spec: kaitov1alpha1.MultiRoleInferenceSpec{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Model: kaitov1alpha1.MultiRoleInferenceModelSpec{Name: "deepseek-r1-0528"},
			Roles: []kaitov1alpha1.MultiRoleInferenceRoleSpec{
				{Type: kaitov1alpha1.MultiRoleInferenceRolePrefill, Replicas: int32Ptr(1), InstanceType: "Standard_NC6s_v3"},
				{Type: kaitov1alpha1.MultiRoleInferenceRoleDecode, Replicas: int32Ptr(1), InstanceType: "Standard_NC6s_v3"},
			},
		},
	}
}

func wireWorkspaceValidation(t *testing.T) {
	orig := kaitov1beta1.ValidateInferenceSetWorkspace
	kaitov1beta1.ValidateInferenceSetWorkspace = inferencesetutil.ValidateWorkspaceForInferenceSet
	t.Cleanup(func() { kaitov1beta1.ValidateInferenceSetWorkspace = orig })
}

func useFakeGlobalClient(t *testing.T, objects ...client.Object) {
	orig := k8sclient.GetGlobalClient()
	k8sclient.SetGlobalClient(fake.NewClientBuilder().WithObjects(objects...).Build())
	t.Cleanup(func() { k8sclient.SetGlobalClient(orig) })
}

func TestNewInferenceSetForMultiRoleInference(t *testing.T) {
	mri := newValidMRI()
	is := NewInferenceSetForMultiRoleInference(mri, mri.Spec.Roles[0])

	assert.Equal(t, "test-mri-prefill", is.Name)
	assert.Equal(t, "default", is.Namespace)
	assert.Equal(t, "test-mri", is.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent])
	assert.Equal(t, "prefill", is.Labels[kaitov1alpha1.LabelInferenceRole])
	assert.Equal(t, "test", is.Spec.Selector.MatchLabels["app"])
	assert.Equal(t, "test-mri", is.Spec.Selector.MatchLabels[kaitov1alpha1.LabelMultiRoleInferenceParent])
	assert.Equal(t, "prefill", is.Spec.Selector.MatchLabels[kaitov1alpha1.LabelInferenceRole])
	assert.Equal(t, "test", is.Spec.Template.Labels["app"])
	assert.Equal(t, "test-mri", is.Spec.Template.Labels[kaitov1alpha1.LabelMultiRoleInferenceParent])
	assert.Equal(t, "prefill", is.Spec.Template.Labels[kaitov1alpha1.LabelInferenceRole])
	assert.Equal(t, "true", is.Spec.Template.Annotations[kaitov1alpha1.AnnotationBypassResourceChecks])
	assert.Equal(t, "deepseek-r1-0528", string(is.Spec.Template.Inference.Preset.Name))
	assert.Equal(t, "Standard_NC6s_v3", is.Spec.Template.Resource.InstanceType)
}

func TestValidateInferenceSetsForMultiRoleInference_BYOAllowsInstanceType(t *testing.T) {
	wireWorkspaceValidation(t)
	useFakeGlobalClient(t)

	origNAPDisabled := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	origProvisioner := consts.ActiveNodeProvisioner
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	consts.ActiveNodeProvisioner = consts.NodeProvisionerBYO
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAPDisabled
		consts.ActiveNodeProvisioner = origProvisioner
	})

	assert.Nil(t, ValidateInferenceSetsForMultiRoleInference(context.Background(), newValidMRI()))
}

func TestValidateInferenceSetsForMultiRoleInference_UpdateRejectsSelectorChange(t *testing.T) {
	wireWorkspaceValidation(t)
	useFakeGlobalClient(t)

	origNAPDisabled := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	origProvisioner := consts.ActiveNodeProvisioner
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	consts.ActiveNodeProvisioner = consts.NodeProvisionerBYO
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = origNAPDisabled
		consts.ActiveNodeProvisioner = origProvisioner
	})

	old := newValidMRI()
	updated := newValidMRI()
	updated.Spec.LabelSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "changed"}}

	err := ValidateInferenceSetsForMultiRoleInference(apis.WithinUpdate(context.Background(), old), updated)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "spec.labelSelector")
	assert.Contains(t, err.Error(), "field is immutable")
	assert.NotContains(t, err.Error(), "generated child InferenceSet")
}

func TestValidateInferenceSetsForMultiRoleInference_RemapRoleLocalPaths(t *testing.T) {
	orig := kaitov1beta1.ValidateInferenceSetWorkspace
	kaitov1beta1.ValidateInferenceSetWorkspace = func(context.Context, *kaitov1beta1.InferenceSet) *apis.FieldError {
		return apis.ErrGeneric("field is immutable", "spec.template.resource.instanceType")
	}
	t.Cleanup(func() { kaitov1beta1.ValidateInferenceSetWorkspace = orig })

	err := ValidateInferenceSetsForMultiRoleInference(context.Background(), newValidMRI())
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "spec.roles[0].instanceType")
	assert.Contains(t, err.Error(), "spec.roles[1].instanceType")
	assert.NotContains(t, err.Error(), "spec.template.resource.instanceType")
}

func TestValidateInferenceSetsForMultiRoleInference_RemapTopLevelAnnotations(t *testing.T) {
	orig := kaitov1beta1.ValidateInferenceSetWorkspace
	kaitov1beta1.ValidateInferenceSetWorkspace = func(context.Context, *kaitov1beta1.InferenceSet) *apis.FieldError {
		return apis.ErrGeneric("annotation is immutable after creation", "spec.template.metadata.annotations[kaito.sh/capacity-type]")
	}
	t.Cleanup(func() { kaitov1beta1.ValidateInferenceSetWorkspace = orig })

	err := ValidateInferenceSetsForMultiRoleInference(context.Background(), newValidMRI())
	assert.NotNil(t, err)
	wrapped := err.WrappedErrors()
	assert.Len(t, wrapped, 1)
	assert.Equal(t, []string{"metadata.annotations[kaito.sh/capacity-type]"}, wrapped[0].Paths)
	assert.NotContains(t, err.Error(), "spec.template.metadata.annotations")
}

func TestRemapChildRuntimeConfigErrors(t *testing.T) {
	for _, path := range []string{"spec.template.config", "spec.template.inference.config", "spec.template.inference_config.yaml"} {
		t.Run(path, func(t *testing.T) {
			original := &apis.FieldError{Message: "ConfigMap not found", Paths: []string{path}, Details: "runtime configuration is unavailable"}
			mapped := remapChildInferenceSetErrors(original, 1)
			wrapped := mapped.WrappedErrors()
			if assert.Len(t, wrapped, 1) {
				assert.Equal(t, &apis.FieldError{
					Message: original.Message, Paths: []string{"spec.roles[1].runtimeConfig"}, Details: original.Details, Level: original.Level,
				}, wrapped[0])
			}
			assert.Equal(t, []string{path}, original.Paths)
		})
	}
}

func TestValidateInferenceSetsForMultiRoleInference_MissingRuntimeConfig(t *testing.T) {
	wireWorkspaceValidation(t)
	useFakeGlobalClient(t)
	oldVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldVLLM })
	mri := newValidMRI()
	mri.Spec.Roles[0].RuntimeConfig = "missing-runtime-config"
	err := ValidateInferenceSetsForMultiRoleInference(context.Background(), mri)
	if assert.NotNil(t, err) {
		assert.Contains(t, err.Error(), "spec.roles[0].runtimeConfig")
		assert.Contains(t, err.Error(), "missing-runtime-config")
		assert.NotContains(t, err.Error(), "spec.template.config")
	}
}

func TestValidateInferenceSetsForMultiRoleInference_WorkspaceCreateErrorPaths(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	wireWorkspaceValidation(t)
	useFakeGlobalClient(t, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "malformed-runtime-config", Namespace: "default"},
		Data:       map[string]string{"inference_config.yaml": "vllm: ["},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "missing-runtime-key", Namespace: "default"},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-context-length", Namespace: "default"},
		Data:       map[string]string{"inference_config.yaml": "vllm:\n  max-model-len: not-an-integer"},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "excessive-context-length", Namespace: "default"},
		Data:       map[string]string{"inference_config.yaml": "vllm:\n  max-model-len: 2147483647"},
	})
	oldVLLM := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	oldNAPDisabled := featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	t.Cleanup(func() {
		featuregates.FeatureGates[consts.FeatureFlagVLLM] = oldVLLM
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = oldNAPDisabled
	})

	// A valid baseline makes each failure attributable to the mutation below.
	require.Nil(t, ValidateInferenceSetsForMultiRoleInference(context.Background(), newValidMRI()))
	for _, tt := range []struct {
		name      string
		mutate    func(*kaitov1alpha1.MultiRoleInference)
		childPath string
		mriPath   string
		message   string
	}{
		{
			name: "missing instance type",
			mutate: func(mri *kaitov1alpha1.MultiRoleInference) {
				mri.Spec.Roles[1].InstanceType = ""
			},
			childPath: "spec.template.spec.resource.instanceType",
			mriPath:   "spec.roles[1].instanceType",
			message:   "missing field(s)",
		},
		{
			name: "invalid node image family",
			mutate: func(mri *kaitov1alpha1.MultiRoleInference) {
				mri.Annotations[kaitov1beta1.AnnotationNodeImageFamily] = "unsupported"
			},
			childPath: `spec.template.spec.metadata.annotations["kaito.sh/node-image-family"]`,
			mriPath:   `metadata.annotations["kaito.sh/node-image-family"]`,
			message:   "unsupported node image family",
		},
		{
			name: "missing runtime ConfigMap key",
			mutate: func(mri *kaitov1alpha1.MultiRoleInference) {
				mri.Spec.Roles[1].RuntimeConfig = "missing-runtime-key"
			},
			childPath: "spec.template.inference_config.yaml in ConfigMap",
			mriPath:   "spec.roles[1].runtimeConfig",
			message:   "missing field(s)",
		},
		{
			name: "invalid context length",
			mutate: func(mri *kaitov1alpha1.MultiRoleInference) {
				mri.Spec.Roles[1].RuntimeConfig = "invalid-context-length"
			},
			childPath: "spec.template.max-model-len",
			mriPath:   "spec.roles[1].runtimeConfig",
			message:   "must be an integer",
		},
		{
			name: "excessive context length",
			mutate: func(mri *kaitov1alpha1.MultiRoleInference) {
				mri.Spec.Roles[1].RuntimeConfig = "excessive-context-length"
			},
			childPath: "spec.template.max-model-len",
			mriPath:   "spec.roles[1].runtimeConfig",
			message:   "exceeds model's maximum supported context window",
		},
		{
			name: "malformed runtime YAML",
			mutate: func(mri *kaitov1alpha1.MultiRoleInference) {
				mri.Spec.Roles[1].RuntimeConfig = "malformed-runtime-config"
			},
			childPath: "spec.template.inference_config.yaml",
			mriPath:   "spec.roles[1].runtimeConfig",
			message:   "Failed to parse inference_config.yaml",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mri := newValidMRI()
			tt.mutate(mri)

			// Exercise the actual child validator, not a stub with an assumed path.
			childErr := NewInferenceSetForMultiRoleInference(mri, mri.Spec.Roles[1]).Validate(context.Background())
			require.NotNil(t, childErr)
			assert.Contains(t, fieldErrorPaths(childErr), tt.childPath)

			err := ValidateInferenceSetsForMultiRoleInference(context.Background(), mri)
			require.NotNil(t, err)
			assert.Contains(t, fieldErrorPaths(err), tt.mriPath)
			assert.Contains(t, err.Error(), tt.message)
			assert.NotContains(t, err.Error(), "spec.template.")
		})
	}
}

func TestValidateInferenceSetsForMultiRoleInference_CustomPresetStreamingDisabled(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	wireWorkspaceValidation(t)
	useFakeGlobalClient(t)
	oldStreaming := featuregates.FeatureGates[consts.FeatureFlagModelStreaming]
	featuregates.FeatureGates[consts.FeatureFlagModelStreaming] = false
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagModelStreaming] = oldStreaming })

	mri := newValidMRI()
	mri.Spec.Model.Name = "custom"
	childErr := NewInferenceSetForMultiRoleInference(mri, mri.Spec.Roles[1]).Validate(context.Background())
	require.NotNil(t, childErr)
	assert.Contains(t, fieldErrorPaths(childErr), "spec.template.inference.preset.name")

	err := ValidateInferenceSetsForMultiRoleInference(context.Background(), mri)
	require.NotNil(t, err)
	assert.Contains(t, fieldErrorPaths(err), "spec.model.name")
	assert.NotContains(t, err.Error(), "spec.model.name.name")
	assert.NotContains(t, err.Error(), "spec.template.")
	assert.Contains(t, err.Error(), "feature gate to be enabled")
}

func TestValidateInferenceSetsForMultiRoleInference_UpdateRejectsSecretChange(t *testing.T) {
	t.Setenv("CLOUD_PROVIDER", consts.AzureCloudName)
	wireWorkspaceValidation(t)
	useFakeGlobalClient(t)
	old := newValidMRI()
	old.Spec.Model.ModelAccessSecret = "old-model-secret"
	require.Nil(t, ValidateInferenceSetsForMultiRoleInference(context.Background(), old))

	updated := old.DeepCopy()
	updated.Spec.Model.ModelAccessSecret = "new-model-secret"
	oldChild := NewInferenceSetForMultiRoleInference(old, old.Spec.Roles[0])
	child := NewInferenceSetForMultiRoleInference(updated, updated.Spec.Roles[0])
	childErr := child.Validate(apis.WithinUpdate(context.Background(), oldChild))
	require.NotNil(t, childErr)
	assert.Equal(t, []string{"spec.template.inference.preset"}, fieldErrorPaths(childErr))

	err := ValidateInferenceSetsForMultiRoleInference(apis.WithinUpdate(context.Background(), old), updated)
	require.NotNil(t, err)
	assert.Equal(t, []string{"spec.model"}, fieldErrorPaths(err))
	assert.Contains(t, err.Error(), "field is immutable")
}

func TestRemapChildPresetPaths(t *testing.T) {
	for path, want := range map[string]string{
		"spec.template.inference.preset":                                 "spec.model",
		"spec.template.inference.presetName":                             "spec.model.name",
		"spec.template.inference.preset.presetOptions.modelAccessSecret": "spec.model.modelAccessSecret",
	} {
		t.Run(path, func(t *testing.T) {
			assert.Equal(t, want, remapChildInferenceSetPath(path, 1))
		})
	}
}

func fieldErrorPaths(err *apis.FieldError) []string {
	var paths []string
	for _, wrapped := range err.WrappedErrors() {
		paths = append(paths, wrapped.Paths...)
	}
	return paths
}
