// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.
package models

import (
	_ "embed"

	"gopkg.in/yaml.v2"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/kaito-project/kaito/pkg/model"
)

var (
	//go:embed supported_models.yaml
	supportedModelsYAML []byte

	// SupportedModels is a global map that holds the source of truth
	// for all supported models and their metadata.
	SupportedModels map[string]*model.Metadata
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

	SupportedModels = make(map[string]*model.Metadata)
	for _, m := range catalog.Models {
		SupportedModels[m.Name] = &m
	}
}
