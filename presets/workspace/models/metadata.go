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

package models

import (
	_ "embed"
	"sync"

	"gopkg.in/yaml.v2"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/kaito-project/kaito/pkg/model"
)

const (
	DefaultVLLMCommand              = "python3 /workspace/vllm/inference_api.py"
	DefaultVLLMRayLeaderBaseCommand = "/workspace/vllm/multi-node-serving.sh leader"
	DefaultVLLMRayWorkerBaseCommand = "/workspace/vllm/multi-node-serving.sh worker"
)

var (
	//go:embed base_images.yaml
	baseImagesYAML []byte

	// baseImages holds runtime metadata for KAITO base images.
	baseImages sync.Map
)

// BaseImageCatalog holds runtime image metadata parsed from base_images.yaml.
type BaseImageCatalog struct {
	Images []model.Metadata `yaml:"images,omitempty"`
}

// init loads the embedded base image metadata.
func init() {
	catalog := BaseImageCatalog{}
	utilruntime.Must(yaml.Unmarshal(baseImagesYAML, &catalog))

	for _, m := range catalog.Images {
		utilruntime.Must(m.Validate())
		baseImages.Store(m.Name, &m)
	}
}

// MustGet retrieves base image metadata by name or panics if it is missing.
func MustGet(name string) model.Metadata {
	m, ok := baseImages.Load(name)
	if !ok {
		panic("model metadata not found: " + name)
	}

	return *(m.(*model.Metadata))
}

// Get retrieves base image metadata by name.
func Get(name string) (model.Metadata, bool) {
	m, ok := baseImages.Load(name)
	if !ok {
		return model.Metadata{}, false
	}

	return *(m.(*model.Metadata)), true
}
