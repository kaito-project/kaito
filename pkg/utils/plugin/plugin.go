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

package plugin

import (
	"strings"
	"sync"

	"github.com/kaito-project/kaito/pkg/model"
)

// Registration is a struct that holds the name and an instance of a struct
// that implements the model.Model interface. It is used to register and manage
// different model instances within the KAITO framework.
type Registration struct {
	// Name is the name of the model. It is used as a key to register and
	// retrieve the model metadata and instance.
	Name string

	// Instance is the actual model instance that implements the model.Model
	// interface. It is used to retrieve the model's compute/storage requirements
	// and runtime parameters.
	Instance model.Model
}

type ModelRegister struct {
	sync.RWMutex
	models map[string]*Registration
}

var KaitoModelRegister ModelRegister

// Register allows model to be added
func (reg *ModelRegister) Register(r *Registration) {
	reg.Lock()
	defer reg.Unlock()
	if r.Name == "" {
		panic("model name is not specified")
	}

	if reg.models == nil {
		reg.models = make(map[string]*Registration)
	}

	reg.models[r.Name] = r
}

func (reg *ModelRegister) MustGet(name string) model.Model {
	reg.Lock()
	defer reg.Unlock()
	r, ok := reg.models[name]
	if !ok {
		return nil
	}
	return r.Instance
}

func (reg *ModelRegister) ListModelNames() []string {
	reg.Lock()
	defer reg.Unlock()
	n := []string{}
	for k := range reg.models {
		n = append(n, k)
	}
	return n
}

func (reg *ModelRegister) Has(name string) bool {
	reg.Lock()
	defer reg.Unlock()
	_, ok := reg.models[name]
	return ok
}

// IsValidPreset returns true if:
// 1. the given preset name is registered in the KaitoModelRegister.
// 2. the given preset name is a valid huggingface model card ID, e.g. "Qwen/Qwen2.5-Coder-7B-Instruct"
func IsValidPreset(preset string) bool {
	if KaitoModelRegister.Has(preset) {
		return true
	}
	// if preset is like "a/b", consider it as a valid HF model ID
	if strings.Contains(preset, "/") {
		parts := strings.SplitN(preset, "/", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return true
		}
	}
	return false
}
