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

package speculativedecoding

import (
	"fmt"

	"knative.dev/pkg/apis"

	"github.com/kaito-project/kaito/pkg/model"
)

// ValidationOptions captures the resource-specific fields needed to validate
// kaito.sh/enable-speculative-decoding while sharing the annotation/preset/
// runtime policy across API versions and resource kinds.
type ValidationOptions struct {
	AnnotationKey          string
	AnnotationPath         string
	PresetName             string
	MissingPresetMessage   string
	Runtime                model.RuntimeName
	RuntimeMismatchMessage string
}

// ValidateOptIn validates the common admission policy for the speculative
// decoding annotation. Callers remain responsible for extracting the
// resource-specific preset name, effective runtime, and field-path messages.
func ValidateOptIn(annotations map[string]string, opts ValidationOptions) *apis.FieldError {
	val, present := annotations[opts.AnnotationKey]
	if !present || val == "false" {
		return nil
	}
	if val != "true" {
		return apis.ErrInvalidValue(
			fmt.Sprintf(
				"annotation %s has invalid value %q; expected \"true\" or \"false\"",
				opts.AnnotationKey,
				val,
			),
			opts.AnnotationPath,
		)
	}
	if opts.PresetName == "" {
		return apis.ErrGeneric(opts.MissingPresetMessage, opts.AnnotationPath)
	}
	if opts.Runtime != model.RuntimeNameVLLM {
		return apis.ErrGeneric(opts.RuntimeMismatchMessage, opts.AnnotationPath)
	}
	return nil
}
