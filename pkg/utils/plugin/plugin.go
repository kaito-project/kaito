// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package plugin

import (
	"fmt"
	"sync"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/presets/workspace/models"
)

// Registration is a struct that holds the name and an instance of a struct
// that implements the model.Model interface. It is used to register and manage
// different model instances within the Kaito framework.
type Registration struct {
	// Name is the name of the model. It is used as a key to register and
	// retrieve the model metadata and instance.
	Name string

	// Metadata contains the metadata information about the model. It is used to
	// provide additional information about the model, such as its model type,
	// which HuggingFace model it is based on, and other relevant details. If empty
	// during registration, it will be automatically populated from presets/workspace/models/supported_models.yaml.
	Metadata *model.Metadata

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

func (reg *ModelRegister) Register(r *Registration) {
	reg.Lock()
	defer reg.Unlock()
	if r.Name == "" {
		panic("model name is not specified")
	}

	if reg.models == nil {
		reg.models = make(map[string]*Registration)
	}

	if r.Metadata == nil {
		var ok bool
		r.Metadata, ok = models.SupportedModels[r.Name]
		if !ok {
			panic(fmt.Sprintf("model '%s' cannot be found in supported_models.yaml", r.Name))
		}
	}

	reg.models[r.Name] = r
}

func (reg *ModelRegister) MustGet(name string) model.Model {
	reg.Lock()
	defer reg.Unlock()
	r, ok := reg.models[name]
	if !ok {
		panic("model is not registered")
	}
	if r.Metadata == nil {
		panic("model metadata is not registered")
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

func IsValidPreset(preset string) bool {
	return KaitoModelRegister.Has(preset)
}
