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

package tolerations

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

var custom []corev1.Toleration

// SetFromJSON configures tolerations that should be added to generated
// workload pods. An empty value keeps the default behavior unchanged.
func SetFromJSON(value string) error {
	if strings.TrimSpace(value) == "" {
		custom = nil
		return nil
	}

	var parsed []corev1.Toleration
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return fmt.Errorf("parsing custom workload tolerations as JSON: %w", err)
	}
	custom = append([]corev1.Toleration(nil), parsed...)
	return nil
}

// Append adds configured tolerations after the built-in provider tolerations.
// It returns a new slice so neither input slice is modified.
func Append(base []corev1.Toleration) []corev1.Toleration {
	result := make([]corev1.Toleration, 0, len(base)+len(custom))
	result = append(result, base...)
	return append(result, custom...)
}
