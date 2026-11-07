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

package inferencepool

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

func TestDefaultSources(t *testing.T) {
	assert.Equal(t, Sources{
		ChartURL:           consts.InferencePoolChartURL,
		ChartVersion:       consts.InferencePoolChartVersion,
		ChartProvider:      ChartProviderGeneric,
		EPPImageRegistry:   consts.EPPImageRegistry,
		EPPImageRepository: consts.EPPImageRepository,
		EPPImageTag:        consts.EPPImageTag,
	}, DefaultSources())
}

func TestOCIRepositoryProvider(t *testing.T) {
	// generic is the CRD default, so it is left unset to keep generated specs unchanged.
	assert.Equal(t, "", DefaultSources().OCIRepositoryProvider())
	assert.Equal(t, "", Sources{}.OCIRepositoryProvider())
	for _, p := range []string{"aws", "azure", "gcp"} {
		assert.Equal(t, p, Sources{ChartProvider: p}.OCIRepositoryProvider())
	}
}

func TestResolveSources(t *testing.T) {
	defaults := DefaultSources()
	with := func(mutate func(*Sources)) Sources {
		s := defaults
		mutate(&s)
		return s
	}
	tests := []struct {
		name      string
		overrides Sources
		want      Sources
		wantErr   []string
	}{
		{name: "empty overrides keep defaults", want: defaults},
		{
			name: "all overrides",
			overrides: Sources{
				ChartURL:           "oci://mirror.example.com/charts/llm-d-router-gateway",
				ChartVersion:       "v0.9.1",
				ChartProvider:      "azure",
				EPPImageRegistry:   "mirror.example.com/llm-d",
				EPPImageRepository: "epp",
				EPPImageTag:        "v0.9.1",
			},
			want: Sources{
				ChartURL:           "oci://mirror.example.com/charts/llm-d-router-gateway",
				ChartVersion:       "v0.9.1",
				ChartProvider:      "azure",
				EPPImageRegistry:   "mirror.example.com/llm-d",
				EPPImageRepository: "epp",
				EPPImageTag:        "v0.9.1",
			},
		},
		{
			name:      "registry only keeps other defaults",
			overrides: Sources{EPPImageRegistry: "myacr.azurecr.io/llm-d"},
			want:      with(func(s *Sources) { s.EPPImageRegistry = "myacr.azurecr.io/llm-d" }),
		},
		{
			name:      "chart url only keeps other defaults",
			overrides: Sources{ChartURL: "oci://artifactory.example.com/ghcr/llm-d/charts/llm-d-router-gateway"},
			want:      with(func(s *Sources) { s.ChartURL = "oci://artifactory.example.com/ghcr/llm-d/charts/llm-d-router-gateway" }),
		},
		{
			name: "trailing slashes on chart url and registry and a leading slash on repository are trimmed",
			overrides: Sources{
				ChartURL:           "oci://mirror.example.com/charts/llm-d-router-gateway/",
				EPPImageRegistry:   "mirror.example.com/llm-d/",
				EPPImageRepository: "/llm-d-router-endpoint-picker",
			},
			want: with(func(s *Sources) {
				s.ChartURL = "oci://mirror.example.com/charts/llm-d-router-gateway"
				s.EPPImageRegistry = "mirror.example.com/llm-d"
				s.EPPImageRepository = "llm-d-router-endpoint-picker"
			}),
		},
		{
			name:      "localhost registry with port",
			overrides: Sources{ChartURL: "oci://localhost:5000/charts/llm-d-router-gateway", EPPImageRegistry: "localhost:5000"},
			want: with(func(s *Sources) {
				s.ChartURL = "oci://localhost:5000/charts/llm-d-router-gateway"
				s.EPPImageRegistry = "localhost:5000"
			}),
		},
		{
			name: "ECR host",
			overrides: Sources{
				ChartURL:         "oci://123456789012.dkr.ecr.us-east-1.amazonaws.com/llm-d-router-gateway",
				ChartProvider:    "aws",
				EPPImageRegistry: "123456789012.dkr.ecr.us-east-1.amazonaws.com/llm-d",
			},
			want: with(func(s *Sources) {
				s.ChartURL = "oci://123456789012.dkr.ecr.us-east-1.amazonaws.com/llm-d-router-gateway"
				s.ChartProvider = "aws"
				s.EPPImageRegistry = "123456789012.dkr.ecr.us-east-1.amazonaws.com/llm-d"
			}),
		},
		{
			name:      "Artifactory remote repository path",
			overrides: Sources{EPPImageRegistry: "artifactory.example.com/docker-remote/oss/v2/llm-d"},
			want:      with(func(s *Sources) { s.EPPImageRegistry = "artifactory.example.com/docker-remote/oss/v2/llm-d" }),
		},
		{
			name:      "Harbor proxy cache on a custom port",
			overrides: Sources{EPPImageRegistry: "harbor.example.com:8443/mcr-proxy/oss/v2/llm-d"},
			want:      with(func(s *Sources) { s.EPPImageRegistry = "harbor.example.com:8443/mcr-proxy/oss/v2/llm-d" }),
		},
		{
			name:      "explicit generic provider",
			overrides: Sources{ChartProvider: "generic"},
			want:      defaults,
		},
		// Chart URL errors.
		{name: "chart url without oci scheme", overrides: Sources{ChartURL: "https://mirror.example.com/charts/x"}, wantErr: []string{"--inference-pool-chart-url", "oci://"}},
		{name: "chart url with only the scheme", overrides: Sources{ChartURL: "oci://"}, wantErr: []string{"--inference-pool-chart-url", "missing registry and chart name"}},
		{name: "chart url with empty host", overrides: Sources{ChartURL: "oci:///x"}, wantErr: []string{"--inference-pool-chart-url"}},
		{name: "chart url with empty path segment", overrides: Sources{ChartURL: "oci://host.example.com//x"}, wantErr: []string{"--inference-pool-chart-url"}},
		{name: "chart url with whitespace", overrides: Sources{ChartURL: "oci://mirror.example.com/charts/llm d"}, wantErr: []string{"--inference-pool-chart-url"}},
		{name: "chart url with a tag", overrides: Sources{ChartURL: "oci://h.example.com/chart:v1"}, wantErr: []string{"--inference-pool-chart-url", "--inference-pool-chart-version"}},
		{name: "chart url with a digest", overrides: Sources{ChartURL: "oci://h.example.com/chart@sha256:" + sha}, wantErr: []string{"--inference-pool-chart-url"}},
		// Chart version errors.
		{name: "chart version with a leading colon", overrides: Sources{ChartVersion: ":v0.9.0"}, wantErr: []string{"--inference-pool-chart-version"}},
		{name: "chart version with build metadata", overrides: Sources{ChartVersion: "v0.9.0+build"}, wantErr: []string{"--inference-pool-chart-version", "replace + with _, as Helm does for OCI tags"}},
		// Provider errors.
		{name: "unknown provider", overrides: Sources{ChartProvider: "oci"}, wantErr: []string{"--inference-pool-chart-provider", "generic, aws, azure, gcp"}},
		{name: "provider is case sensitive", overrides: Sources{ChartProvider: "Azure"}, wantErr: []string{"--inference-pool-chart-provider"}},
		// Registry errors.
		{name: "whitespace-only registry", overrides: Sources{EPPImageRegistry: "   "}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry with a scheme", overrides: Sources{EPPImageRegistry: "https://mirror.example.com/llm-d"}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry with uppercase path", overrides: Sources{EPPImageRegistry: "mirror.example.com/LLM-D"}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry with empty path segment", overrides: Sources{EPPImageRegistry: "a.example.com//b"}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry with two ports", overrides: Sources{EPPImageRegistry: "host:5000:6000"}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry host starting with a dash", overrides: Sources{EPPImageRegistry: "-bad.example"}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry with a query", overrides: Sources{EPPImageRegistry: "mirror.example.com/llm-d?x=1"}, wantErr: []string{"--epp-image-registry"}},
		{name: "registry with a fragment", overrides: Sources{EPPImageRegistry: "mirror.example.com/llm-d#x"}, wantErr: []string{"--epp-image-registry"}},
		// Repository errors.
		{name: "repository with a tag", overrides: Sources{EPPImageRepository: "llm-d-router-endpoint-picker:v0.9.0"}, wantErr: []string{"--epp-image-repository"}},
		{name: "repository with a digest", overrides: Sources{EPPImageRepository: "epp@sha256:" + sha}, wantErr: []string{"--epp-image-repository"}},
		{name: "repository with uppercase", overrides: Sources{EPPImageRepository: "EPP"}, wantErr: []string{"--epp-image-repository"}},
		// Each piece is valid alone, but registry/repository together exceed 255 characters.
		{
			name: "composed image name too long",
			overrides: Sources{
				EPPImageRegistry:   "mirror.example.com/" + strings.Repeat("a", 180),
				EPPImageRepository: strings.Repeat("b", 100),
			},
			wantErr: []string{"--epp-image-repository", "255"},
		},
		// Tag errors.
		{name: "tag with a digest", overrides: Sources{EPPImageTag: "sha256:" + sha}, wantErr: []string{"--epp-image-tag"}},
		{name: "tag with whitespace", overrides: Sources{EPPImageTag: "v0.9.0 "}, wantErr: []string{"--epp-image-tag"}},
		{name: "tag with build metadata", overrides: Sources{EPPImageTag: "v0.9.0+build"}, wantErr: []string{"--epp-image-tag", "replace + with _, as Helm does for OCI tags"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSources(tt.overrides)
			if len(tt.wantErr) > 0 {
				require.Error(t, err)
				for _, want := range tt.wantErr {
					assert.Contains(t, err.Error(), want)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
