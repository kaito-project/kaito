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

var (
	//go:embed supported_models.yaml
	supportedModelsYAML []byte

	// supportedModels is a map that holds the source of truth
	// for all supported models and their metadata.
	supportedModels sync.Map
)

// Catalog is a struct that holds a list of supported models parsed
// from preset/workspace/models/supported_models.yaml. The YAML file is
// considered the source of truth for the model metadata, and any
// information in the YAML file should not be hardcoded in the codebase.
type Catalog struct {
	Models []model.Metadata `yaml:"models,omitempty"`
}

// init unmarshals the YAML data in supportedModelsYAML into the SupportedModels struct.
func init() {
	catalog := Catalog{}
	utilruntime.Must(yaml.Unmarshal(supportedModelsYAML, &catalog))

	for _, m := range catalog.Models {
		utilruntime.Must(m.Validate())
		supportedModels.Store(m.Name, &m)
	}
}

// MustGet retrieves the model metadata for the given model name or
// panics if the model name is not found in the SupportedModels map.
func MustGet(name string) model.Metadata {
	m, ok := supportedModels.Load(name)
	if !ok {
		panic("model metadata not found: " + name)
	}

	return *(m.(*model.Metadata))
}
