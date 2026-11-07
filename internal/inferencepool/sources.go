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

// Package inferencepool resolves where the InferencePool chart and EPP image
// are pulled from when gatewayAPIInferenceExtension is enabled.
package inferencepool

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/distribution/reference"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// ChartProviderGeneric is the Flux OCIRepository default provider (anonymous or static credentials).
const ChartProviderGeneric = sourcev1.GenericOCIProvider

var anchoredTag = regexp.MustCompile(`^` + reference.TagRegexp.String() + `$`)

var chartProviders = []string{
	sourcev1.GenericOCIProvider,
	sourcev1.AmazonOCIProvider,
	sourcev1.AzureOCIProvider,
	sourcev1.GoogleOCIProvider,
}

// Sources holds the chart source and EPP image used for the generated
// OCIRepository and HelmRelease of an InferencePool.
type Sources struct {
	ChartURL           string
	ChartVersion       string
	ChartProvider      string
	EPPImageRegistry   string
	EPPImageRepository string
	EPPImageTag        string
}

// DefaultSources returns the built-in upstream chart and EPP image.
func DefaultSources() Sources {
	return Sources{
		ChartURL:           consts.InferencePoolChartURL,
		ChartVersion:       consts.InferencePoolChartVersion,
		ChartProvider:      ChartProviderGeneric,
		EPPImageRegistry:   consts.EPPImageRegistry,
		EPPImageRepository: consts.EPPImageRepository,
		EPPImageTag:        consts.EPPImageTag,
	}
}

// OCIRepositoryProvider returns the value for OCIRepository spec.provider.
// generic is the CRD default, so it is left unset to keep generated specs unchanged.
func (s Sources) OCIRepositoryProvider() string {
	if s.ChartProvider == ChartProviderGeneric {
		return ""
	}
	return s.ChartProvider
}

// ResolveSources applies non-empty fields of overrides on top of the defaults
// and validates the result. Errors name the controller flag at fault.
func ResolveSources(overrides Sources) (Sources, error) {
	s := DefaultSources()

	if overrides.ChartURL != "" {
		u, err := parseChartURL(overrides.ChartURL)
		if err != nil {
			return Sources{}, fmt.Errorf("invalid --inference-pool-chart-url %q: %w", overrides.ChartURL, err)
		}
		s.ChartURL = u
	}
	if overrides.ChartVersion != "" {
		if err := validateTag(overrides.ChartVersion); err != nil {
			return Sources{}, fmt.Errorf("invalid --inference-pool-chart-version %q: %w", overrides.ChartVersion, err)
		}
		s.ChartVersion = overrides.ChartVersion
	}
	if overrides.ChartProvider != "" {
		if !slices.Contains(chartProviders, overrides.ChartProvider) {
			return Sources{}, fmt.Errorf("invalid --inference-pool-chart-provider %q: must be one of %s",
				overrides.ChartProvider, strings.Join(chartProviders, ", "))
		}
		s.ChartProvider = overrides.ChartProvider
	}
	if overrides.EPPImageRegistry != "" {
		r := strings.TrimRight(overrides.EPPImageRegistry, "/")
		if err := validateImage(r, s.EPPImageRepository, s.EPPImageTag); err != nil {
			return Sources{}, fmt.Errorf("invalid --epp-image-registry %q: %w", overrides.EPPImageRegistry, err)
		}
		s.EPPImageRegistry = r
	}
	if overrides.EPPImageRepository != "" {
		r := strings.Trim(overrides.EPPImageRepository, "/")
		// Checked against the resolved registry, so the composed name is validated as a whole.
		if err := validateImage(s.EPPImageRegistry, r, s.EPPImageTag); err != nil {
			return Sources{}, fmt.Errorf("invalid --epp-image-repository %q: %w", overrides.EPPImageRepository, err)
		}
		s.EPPImageRepository = r
	}
	if overrides.EPPImageTag != "" {
		if err := validateTag(overrides.EPPImageTag); err != nil {
			return Sources{}, fmt.Errorf("invalid --epp-image-tag %q: %w", overrides.EPPImageTag, err)
		}
		s.EPPImageTag = overrides.EPPImageTag
	}
	return s, nil
}

// parseChartURL returns the oci:// URL without trailing slashes. The chart
// is referenced by name only; its tag comes from --inference-pool-chart-version.
func parseChartURL(u string) (string, error) {
	rest, ok := strings.CutPrefix(u, "oci://")
	if !ok {
		return "", fmt.Errorf("must start with oci://")
	}
	rest = strings.TrimRight(rest, "/")
	if rest == "" {
		return "", fmt.Errorf("missing registry and chart name after oci://")
	}
	named, err := reference.ParseNormalizedNamed(rest)
	if err != nil {
		return "", err
	}
	if _, isTagged := named.(reference.Tagged); isTagged {
		return "", fmt.Errorf("must not include a tag; set it with --inference-pool-chart-version")
	}
	if _, isDigested := named.(reference.Digested); isDigested {
		return "", fmt.Errorf("must not include a digest")
	}
	return "oci://" + rest, nil
}

// validateImage checks the image the llm-d-router-gateway chart composes as {registry}/{repository}:{tag}.
func validateImage(registry, repository, tag string) error {
	_, err := reference.ParseNormalizedNamed(registry + "/" + repository + ":" + tag)
	return err
}

func validateTag(tag string) error {
	if !anchoredTag.MatchString(tag) {
		if strings.Contains(tag, "+") {
			return fmt.Errorf("must be an OCI tag; replace + with _, as Helm does for OCI tags")
		}
		return fmt.Errorf("must be an OCI tag (digests are not supported)")
	}
	return nil
}
