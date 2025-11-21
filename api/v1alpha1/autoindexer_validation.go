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

package v1alpha1

import (
	"context"
	"fmt"
	"regexp"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/klog/v2"
	"knative.dev/pkg/apis"
)

func (a *AutoIndexer) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (a *AutoIndexer) Validate(ctx context.Context) (errs *apis.FieldError) {
	base := apis.GetBaseline(ctx)
	if base == nil {
		klog.InfoS("Validate creation", "autoindexer", fmt.Sprintf("%s/%s", a.Namespace, a.Name))
		errs = errs.Also(a.validateCreate().ViaField("spec"))
	} else {
		klog.InfoS("Validate update", "autoindexer", fmt.Sprintf("%s/%s", a.Namespace, a.Name))
		old := base.(*AutoIndexer)
		errs = errs.Also(
			a.validateCreate().ViaField("spec"),
			a.Spec.validateUpdate(old.Spec).ViaField("resource"),
		)
	}
	return errs
}
func (a *AutoIndexer) validateCreate() (errs *apis.FieldError) {

	if a.Spec.RAGEngine == "" {
		errs = errs.Also(apis.ErrMissingField("ragEngine"))
	}
	if a.Spec.IndexName == "" {
		errs = errs.Also(apis.ErrMissingField("indexName"))
	} else {
		// Use regex for indexName validation
		var patIndexName = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)
		if !patIndexName.MatchString(a.Spec.IndexName) {
			errs = errs.Also(apis.ErrInvalidValue(a.Spec.IndexName, "indexName"))
		}
	}

	// Validate DataSource
	if err := a.Spec.DataSource.validate(); err != nil {
		errs = errs.Also(err.ViaField("dataSource"))
	}
	// Validate Credentials if present
	if a.Spec.Credentials != nil {
		if a.Spec.Credentials.Type == "" {
			errs = errs.Also(apis.ErrMissingField("credentials.type"))
		}
		if a.Spec.Credentials.Type == "SecretRef" && (a.Spec.Credentials.SecretRef == nil || a.Spec.Credentials.SecretRef.Name == "" || a.Spec.Credentials.SecretRef.Key == "") {
			errs = errs.Also(apis.ErrMissingField("credentials.secretRef"))
		}
	}
	// Validate Schedule pattern if present
	if a.Spec.Schedule != nil && *a.Spec.Schedule != "" {
		// Basic check for cron or @every format
		s := *a.Spec.Schedule
		if !(s[0] == '@' || len(s) >= 5) {
			errs = errs.Also(apis.ErrInvalidValue(s, "schedule"))
		}
	}
	return errs
}

func (ds *DataSourceSpec) validate() *apis.FieldError {
	if ds == nil {
		return apis.ErrMissingField("")
	}
	if ds.Type == "" {
		return apis.ErrMissingField("type")
	}
	switch ds.Type {
	case "Git":
		if ds.Git == nil {
			return apis.ErrMissingField("git")
		}
		return ds.Git.validate()
	case "Static":
		if ds.Static == nil {
			return apis.ErrMissingField("static")
		}
		return ds.Static.validate()
	default:
		return apis.ErrInvalidValue(ds.Type, "type")
	}
}

func (g *GitDataSourceSpec) validate() *apis.FieldError {
	if g.Repository == "" {
		return apis.ErrMissingField("repository")
	}
	if g.Branch == "" {
		return apis.ErrMissingField("branch")
	}

	// Validate Paths and ExcludePaths using .gitignore-like rules with regex
	// Patterns:
	// 1. **/foo/bar, foo/bar, foo, **/foo, a/**/b
	// 2. *.go, foo/*.go, **/*.go
	// 3. foo/**, foo/bar/**
	// 4. a/**/b
	var (
		patSimple           = regexp.MustCompile(`^([a-zA-Z0-9._-]+)(/[a-zA-Z0-9._-]+)*$`)
		patStar             = regexp.MustCompile(`^(\*|\*\*/[a-zA-Z0-9._-]+|[a-zA-Z0-9._-]+/\*|[a-zA-Z0-9._-]+/\*\.[a-zA-Z0-9]+|\*\.[a-zA-Z0-9]+)$`)
		patGlobstar         = regexp.MustCompile(`^(\*\*/[a-zA-Z0-9._-]+(/[a-zA-Z0-9._-]+)*)$`)
		patTrailingGlobstar = regexp.MustCompile(`^([a-zA-Z0-9._-]+(/[a-zA-Z0-9._-]+)*)/\*\*$`)
		patMiddleGlobstar   = regexp.MustCompile(`^([a-zA-Z0-9._-]+/)?\*\*/[a-zA-Z0-9._-]+(/[a-zA-Z0-9._-]+)*$`)
	)
	isValidGitignorePattern := func(p string) bool {
		if p == "" {
			return false
		}
		if patSimple.MatchString(p) {
			return true
		}
		if patStar.MatchString(p) {
			return true
		}
		if patGlobstar.MatchString(p) {
			return true
		}
		if patTrailingGlobstar.MatchString(p) {
			return true
		}
		if patMiddleGlobstar.MatchString(p) {
			return true
		}
		return false
	}
	for i, p := range g.Paths {
		if !isValidGitignorePattern(p) {
			return apis.ErrInvalidValue(p, "paths").ViaFieldIndex("paths", i)
		}
	}
	for i, p := range g.ExcludePaths {
		if !isValidGitignorePattern(p) {
			return apis.ErrInvalidValue(p, "excludePaths").ViaFieldIndex("excludePaths", i)
		}
	}
	return nil
}

func (s *StaticDataSourceSpec) validate() *apis.FieldError {
	if len(s.URLs) == 0 {
		return apis.ErrMissingField("urls")
	}
	for i, url := range s.URLs {
		// Basic URL validation
		u, err := apis.ParseURL(url)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return apis.ErrInvalidValue(url, "urls").ViaFieldIndex("urls", i)
		}
	}
	return nil

}

func (a *AutoIndexerSpec) validateUpdate(old AutoIndexerSpec) (errs *apis.FieldError) {
	return nil
}
