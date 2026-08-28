// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package controllers

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

type modelMetadataHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f modelMetadataHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestParseModelMetadata(t *testing.T) {
	tests := map[string]struct {
		body    string
		want    *int32
		wantErr bool
	}{
		"reported value": {
			body: `{"data":[{"id":"model","max_model_len":4096,"extra":true}]}`,
			want: func() *int32 { value := int32(4096); return &value }(),
		},
		"missing value": {body: `{"data":[{"id":"model"}]}`},
		"null value":    {body: `{"data":[{"max_model_len":null}]}`},
		"empty data":    {body: `{"data":[]}`},
		"invalid value": {body: `{"data":[{"max_model_len":0}]}`, wantErr: true},
		"invalid json":  {body: `{`, wantErr: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseModelMetadata(strings.NewReader(test.body))
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestCollectActualMaxModelLen(t *testing.T) {
	originalVLLMFlag := featuregates.FeatureGates[consts.FeatureFlagVLLM]
	featuregates.FeatureGates[consts.FeatureFlagVLLM] = true
	t.Cleanup(func() { featuregates.FeatureGates[consts.FeatureFlagVLLM] = originalVLLMFlag })

	var requestedURL string
	reconciler := &WorkspaceReconciler{
		modelMetadataClient: modelMetadataHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			requestedURL = request.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"max_model_len":8192}]}`)),
			}, nil
		}),
	}
	workspace := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "models"}}

	got := reconciler.collectActualMaxModelLen(t.Context(), workspace, true)
	require.NotNil(t, got)
	assert.Equal(t, int32(8192), *got)
	assert.Equal(t, "http://demo.models.svc.cluster.local/v1/models", requestedURL)
}

func TestCollectActualMaxModelLenSkipsUnavailableMetadata(t *testing.T) {
	called := false
	reconciler := &WorkspaceReconciler{
		modelMetadataClient: modelMetadataHTTPClientFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		}),
	}
	workspace := &kaitov1beta1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "models"}}

	assert.Nil(t, reconciler.collectActualMaxModelLen(t.Context(), workspace, false))
	assert.False(t, called)
}

func TestAggregateMaxModelLen(t *testing.T) {
	value := func(v int32) *int32 { return &v }
	tests := map[string]struct {
		workspaces []kaitov1beta1.Workspace
		want       *int32
	}{
		"all ready and equal": {
			workspaces: []kaitov1beta1.Workspace{
				{Status: kaitov1beta1.WorkspaceStatus{State: kaitov1beta1.WorkspaceStateReady, MaxModelLen: value(4096)}},
				{Status: kaitov1beta1.WorkspaceStatus{State: kaitov1beta1.WorkspaceStateReady, MaxModelLen: value(4096)}},
			},
			want: value(4096),
		},
		"missing ready value": {
			workspaces: []kaitov1beta1.Workspace{
				{Status: kaitov1beta1.WorkspaceStatus{State: kaitov1beta1.WorkspaceStateReady}},
			},
		},
		"conflicting ready values": {
			workspaces: []kaitov1beta1.Workspace{
				{Status: kaitov1beta1.WorkspaceStatus{State: kaitov1beta1.WorkspaceStateReady, MaxModelLen: value(4096)}},
				{Status: kaitov1beta1.WorkspaceStatus{State: kaitov1beta1.WorkspaceStateReady, MaxModelLen: value(8192)}},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want, AggregateMaxModelLen(test.workspaces))
		})
	}
}
