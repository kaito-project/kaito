// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package plugin

import (
	"sync"

	"github.com/kaito-project/kaito/pkg/model"
)

// Registration is a struct that holds the name and an instance of a struct
// that implements the model.Model interface. It is used to register and manage
// different model instances within the Kaito framework.
type Registration struct {
	Name     string
	Metadata *model.Metadata
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
	if r.Metadata != nil {
		reg.registerMetadata(r)
	}
	if r.Instance != nil {
		reg.registerInstance(r)
	}
}

// registerMetadata allows model metadata to be added
func (reg *ModelRegister) registerMetadata(r *Registration) {
	if existing, ok := reg.models[r.Name]; !ok {
		reg.models[r.Name] = r
	} else {
		existing.Metadata = r.Metadata
	}
}

// registerInstance allows model instance to be added
func (reg *ModelRegister) registerInstance(r *Registration) {
	if existing, ok := reg.models[r.Name]; !ok {
		reg.models[r.Name] = r
	} else {
		existing.Instance = r.Instance
	}
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
