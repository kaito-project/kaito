// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package plugin

import (
	"os"
	"sync"

	"sigs.k8s.io/yaml"

	"github.com/kaito-project/kaito/pkg/model"
)

// ModelInstance is a struct that holds the name and an instance of a struct
// that implements the model.Model interface. It is used to register and manage
// different model instances within the Kaito framework.
type ModelInstance struct {
	Name     string
	Instance model.Model
}

// SupportModels is a struct that holds a list of supported models parsed
// from preset/workspace/models/supported_models.yaml. The YAML file is
// considered the source of truth for the model information, and any information
// in the YAML file should not be hardcoded in the codebase.
type SupportedModels struct {
	// Models is a slice of ModelInfo structs that contains information about
	// the supported models.
	Models []ModelInfo `json:"models,omitempty"`
}

// ModelInfo is a struct that holds information about a model
type ModelInfo struct {
	// Name is the name of the model, which serves as a unique identifier.
	// It is used to register the model instance and retrieve it later.
	Name string `json:"name,omitempty"`

	// ModelType is the type of the model, which indicates the kind of model
	// it is. Currently, the only supported types are "text-generation" and
	// "llama2-completion" (deprecated).
	ModelType string `json:"type,omitempty"`

	// Version is the version of the model. It is a URL that points to the
	// model's huggingface page, which contains the model's repository ID
	// and revision ID, e.g. https://huggingface.co/mistralai/Mistral-7B-v0.3/commit/d8cadc02ac76bd617a919d50b092e59d2d110aff.
	Version string `json:"version,omitempty"`

	// Runtime is the runtime environment in which the model operates.
	// Currently, the only supported runtime is "tfs".
	Runtime string `json:"runtime,omitempty"`

	// Tag is the tag of the container image used to run the model.
	Tag string `json:"tag,omitempty"`

	// DownloadAtRuntime indicates whether the model should be downloaded
	// at runtime. If set to true, the model will be downloaded when the
	// model deployment is created. If set to false, it will use a container
	// image that already contains the model weights.
	DownloadAtRuntime bool `json:"download_at_runtime,omitempty"`
}

type ModelRegister struct {
	sync.RWMutex
	instances map[string]*ModelInstance
	info      map[string]*ModelInfo
}

var KaitoModelRegister ModelRegister

// Init initializes the KaitoModelRegister with a path to the supported_models.yaml
// file. It reads the file, unmarshals the YAML data into a SupportedModels
// struct, and registers each model instance using the RegisterInfo method.
func (reg *ModelRegister) Init(supportedModelsFilePath string) error {
	reg.instances = make(map[string]*ModelInstance)
	reg.info = make(map[string]*ModelInfo)

	data, err := os.ReadFile(supportedModelsFilePath)
	if err != nil {
		return err
	}

	supportedModels := SupportedModels{}
	if err := yaml.Unmarshal(data, &supportedModels); err != nil {
		return err
	}

	for _, modelInfo := range supportedModels.Models {
		reg.RegisterInfo(&modelInfo)
	}

	return nil
}

// RegisterInfo allows model information to be added
func (reg *ModelRegister) RegisterInfo(i *ModelInfo) {
	reg.Lock()
	defer reg.Unlock()
	if i.Name == "" {
		panic("model name is not specified")
	}

	if reg.info == nil {
		reg.info = make(map[string]*ModelInfo)
	}

	reg.info[i.Name] = i
}

// RegisterInstance allows model to be added
func (reg *ModelRegister) RegisterInstance(r *ModelInstance) {
	reg.Lock()
	defer reg.Unlock()
	if r.Name == "" {
		panic("model name is not specified")
	}

	if reg.instances == nil {
		reg.instances = make(map[string]*ModelInstance)
	}

	reg.instances[r.Name] = r
}

func (reg *ModelRegister) MustGet(name string) model.Model {
	reg.Lock()
	defer reg.Unlock()
	if _, ok := reg.info[name]; !ok {
		panic("model is not defined in supported_models.yaml")
	}
	if _, ok := reg.instances[name]; ok {
		return reg.instances[name].Instance
	}
	panic("model is supported but not registered")
}

func (reg *ModelRegister) ListModelNames() []string {
	reg.Lock()
	defer reg.Unlock()
	n := []string{}
	for k := range reg.instances {
		n = append(n, k)
	}
	return n
}

func (reg *ModelRegister) Has(name string) bool {
	reg.Lock()
	defer reg.Unlock()
	_, ok := reg.instances[name]
	return ok
}

func IsValidPreset(preset string) bool {
	return KaitoModelRegister.Has(preset)
}
