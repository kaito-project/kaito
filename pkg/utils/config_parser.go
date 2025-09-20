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

package utils

import (
	"strconv"
	"strings"
)

// ParseExplicitMaxModelLen scans YAML content for a top-level (under vllm:) 'max-model-len:' value.
// Returns (value,true) only if an explicit non-empty positive integer is found; otherwise (0,false).
func ParseExplicitMaxModelLen(content string) (int, bool) {
	lines := strings.Split(content, "\n")
	inVLLM := false
	baseIndent := ""
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if !inVLLM {
			if strings.HasPrefix(trim, "vllm:") {
				inVLLM = true
				baseIndent = l[:len(l)-len(strings.TrimLeft(l, " \t"))] + "  "
			}
			continue
		}
		currentIndent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		if trim != "" && !strings.HasPrefix(trim, "#") && len(currentIndent) < len(baseIndent) {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(l), "max-model-len:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "max-model-len:"))
			if valStr != "" {
				if v, err := strconv.Atoi(valStr); err == nil && v > 0 {
					return v, true
				}
			}
			break
		}
	}
	return 0, false
}
