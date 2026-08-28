// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	pkgmodel "github.com/kaito-project/kaito/pkg/model"
)

const modelMetadataPath = "/v1/models"

type modelMetadataHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// collectActualMaxModelLen reads the limit reported by a ready vLLM service.
// Metadata is best-effort: an unavailable endpoint must not make status sync fail.
func (c *WorkspaceReconciler) collectActualMaxModelLen(ctx context.Context, wObj *kaitov1beta1.Workspace, ready bool) *int32 {
	if !ready || c.modelMetadataClient == nil || kaitov1beta1.GetWorkspaceRuntimeName(wObj) != pkgmodel.RuntimeNameVLLM {
		return nil
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local%s", wObj.Name, wObj.Namespace, modelMetadataPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	response, err := c.modelMetadataClient.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}

	maxModelLen, err := parseModelMetadata(response.Body)
	if err != nil {
		return nil
	}
	return maxModelLen
}

type modelMetadataResponse struct {
	Data []struct {
		MaxModelLen *int64 `json:"max_model_len"`
	} `json:"data"`
}

func parseModelMetadata(reader io.Reader) (*int32, error) {
	var response modelMetadataResponse
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	if err := decoder.Decode(&response); err != nil {
		return nil, err
	}
	if len(response.Data) == 0 || response.Data[0].MaxModelLen == nil {
		return nil, nil
	}

	value := *response.Data[0].MaxModelLen
	if value <= 0 || value > int64(^uint32(0)>>1) {
		return nil, fmt.Errorf("max_model_len %d is outside the supported range", value)
	}
	result := int32(value)
	return &result, nil
}

// AggregateMaxModelLen returns a value only when every ready workspace reports
// the same limit. A missing or conflicting value is intentionally represented as unknown.
func AggregateMaxModelLen(workspaces []kaitov1beta1.Workspace) *int32 {
	var result *int32
	for i := range workspaces {
		workspace := &workspaces[i]
		if workspace.Status.State != kaitov1beta1.WorkspaceStateReady {
			continue
		}
		if workspace.Status.MaxModelLen == nil {
			return nil
		}
		if result == nil {
			value := *workspace.Status.MaxModelLen
			result = &value
			continue
		}
		if *result != *workspace.Status.MaxModelLen {
			return nil
		}
	}
	return result
}
