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

// ValidateOptIn validates only the boolean annotation shape. It returns
// enabled=true when the annotation is explicitly set to "true". For absent
// or explicit "false", it returns enabled=false with no invalid value. When
// the annotation is present but not a valid boolean opt-in, invalidValue is
// set to the raw value so the caller can build the resource-specific field
// error and path.
func ValidateOptIn(annotations map[string]string, annotationKey string) (enabled bool, invalidValue string) {
	val, present := annotations[annotationKey]
	if !present || val == "false" {
		return false, ""
	}
	if val != "true" {
		return false, val
	}
	return true, ""
}
