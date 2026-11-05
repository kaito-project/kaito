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

	"gopkg.in/yaml.v2"
)

// InferenceRuntimeConfig contains the inference settings needed during workload generation.
type InferenceRuntimeConfig struct {
	VLLM                        map[string]interface{} `yaml:"vllm"`
	KVCacheCPUMemoryUtilization *float64               `yaml:"kv_cache_cpu_memory_utilization"`
}

// ParseInferenceRuntimeConfig parses inference_config.yaml settings used by the controller.
func ParseInferenceRuntimeConfig(content string) (InferenceRuntimeConfig, error) {
	config := InferenceRuntimeConfig{}
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return InferenceRuntimeConfig{}, err
	}
	return config, nil
}

// ParseExplicitMaxModelLen returns the max-model-len set under the top-level vllm: key.
// Returns (value,true) only if it is a positive integer, given as a number or a numeric string; otherwise (0,false).
func ParseExplicitMaxModelLen(content string) (int, bool) {
	var config struct {
		VLLM map[string]interface{} `yaml:"vllm"`
	}
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return 0, false
	}
	var v int
	var err error
	switch value := config.VLLM["max-model-len"].(type) {
	case int:
		v = value
	case string:
		if v, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if v <= 0 {
		return 0, false
	}
	return v, true
}
