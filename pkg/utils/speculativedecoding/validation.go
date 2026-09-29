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

import "github.com/kaito-project/kaito/pkg/model"

type OptInStatus int

const (
	OptInDisabled OptInStatus = iota
	OptInInvalidValue
	OptInMissingPreset
	OptInRuntimeMismatch
	OptInEnabled
)

// ValidateOptIn validates the common speculative-decoding opt-in flow while
// leaving resource-specific field paths and error messages to the caller.
func ValidateOptIn(annotations map[string]string, annotationKey, presetName string, runtime model.RuntimeName) (OptInStatus, string) {
	val, present := annotations[annotationKey]
	if !present || val == "false" {
		return OptInDisabled, ""
	}
	if val != "true" {
		return OptInInvalidValue, val
	}
	if presetName == "" {
		return OptInMissingPreset, ""
	}
	if runtime != model.RuntimeNameVLLM {
		return OptInRuntimeMismatch, ""
	}
	return OptInEnabled, ""
}
