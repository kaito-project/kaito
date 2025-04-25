package models

import (
	_ "embed"

	"gopkg.in/yaml.v2"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/plugin"
)

var (
	//go:embed supported_models.yaml
	supportedModelsYAML []byte
)

// SupportModels is a struct that holds a list of supported models parsed
// from preset/workspace/models/supported_models.yaml. The YAML file is
// considered the source of truth for the model metadata, and any information
// in the YAML file should not be hardcoded in the codebase.
type SupportedModels struct {
	Models []model.Metadata `yaml:"models,omitempty"`
}

// init initializes the KaitoModelRegister with the supported models defined in
// the supported_models.yaml file. It unmarshals the YAML data into a
// struct, and registers each model metadata.
func init() {
	supportedModels := SupportedModels{}
	utilruntime.Must(yaml.Unmarshal(supportedModelsYAML, &supportedModels))
	for _, metadata := range supportedModels.Models {
		plugin.KaitoModelRegister.Register(&plugin.Registration{
			Name:     metadata.Name,
			Metadata: &metadata,
		})
	}
}
