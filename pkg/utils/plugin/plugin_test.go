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
	"testing"

	"github.com/kaito-project/kaito/pkg/model"
)

// MockModel implements model.Model interface for testing
type MockModel struct{}

func (m *MockModel) GetInferenceParameters() *model.PresetParam {
	return nil
}

func (m *MockModel) GetTuningParameters() *model.PresetParam {
	return nil
}

func (m *MockModel) SupportDistributedInference() bool {
	return true
}

func (m *MockModel) SupportTuning() bool {
	return false
}

func TestRegister(t *testing.T) {
	t.Run("register valid model", func(t *testing.T) {
		reg := &ModelRegister{}
		r := &Registration{
			Name:     "test-model",
			Instance: &MockModel{},
		}

		reg.Register(r)

		if !reg.Has("test-model") {
			t.Error("expected model to be registered")
		}
	})

	t.Run("register multiple models", func(t *testing.T) {
		reg := &ModelRegister{}
		r1 := &Registration{Name: "model1", Instance: &MockModel{}}
		r2 := &Registration{Name: "model2", Instance: &MockModel{}}

		reg.Register(r1)
		reg.Register(r2)

		if !reg.Has("model1") || !reg.Has("model2") {
			t.Error("expected both models to be registered")
		}
	})

	t.Run("panic on empty name", func(t *testing.T) {
		reg := &ModelRegister{}
		r := &Registration{
			Name:     "",
			Instance: &MockModel{},
		}

		defer func() {
			if recover() == nil {
				t.Error("expected panic for empty model name")
			}
		}()

		reg.Register(r)
	})
}

func TestMustGet(t *testing.T) {
	t.Run("get existing model", func(t *testing.T) {
		reg := &ModelRegister{}
		mockModel := &MockModel{}
		r := &Registration{
			Name:     "test-model",
			Instance: mockModel,
		}
		reg.Register(r)

		result := reg.MustGet("test-model")

		if result != mockModel {
			t.Error("expected to get registered model instance")
		}
	})

	t.Run("get non-existing model returns nil", func(t *testing.T) {
		reg := &ModelRegister{}

		result := reg.MustGet("non-existing")

		if result != nil {
			t.Error("expected nil for non-existing model")
		}
	})
}

func TestListModelNames(t *testing.T) {
	t.Run("list empty registry", func(t *testing.T) {
		reg := &ModelRegister{}

		names := reg.ListModelNames()

		if len(names) != 0 {
			t.Error("expected empty list")
		}
	})

	t.Run("list registered models", func(t *testing.T) {
		reg := &ModelRegister{}
		reg.Register(&Registration{Name: "model1", Instance: &MockModel{}})
		reg.Register(&Registration{Name: "model2", Instance: &MockModel{}})

		names := reg.ListModelNames()

		if len(names) != 2 {
			t.Errorf("expected 2 names, got %d", len(names))
		}

		nameSet := make(map[string]bool)
		for _, n := range names {
			nameSet[n] = true
		}
		if !nameSet["model1"] || !nameSet["model2"] {
			t.Error("expected model1 and model2 in list")
		}
	})
}

func TestHas(t *testing.T) {
	t.Run("has existing model", func(t *testing.T) {
		reg := &ModelRegister{}
		reg.Register(&Registration{Name: "test-model", Instance: &MockModel{}})

		if !reg.Has("test-model") {
			t.Error("expected Has to return true for existing model")
		}
	})

	t.Run("has non-existing model", func(t *testing.T) {
		reg := &ModelRegister{}

		if reg.Has("non-existing") {
			t.Error("expected Has to return false for non-existing model")
		}
	})
}

func TestIsValidPreset(t *testing.T) {
	t.Run("valid preset registered in KaitoModelRegister", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}
		KaitoModelRegister.Register(&Registration{Name: "valid-preset", Instance: &MockModel{}})

		if !IsValidPreset("valid-preset") {
			t.Error("expected IsValidPreset to return true for registered preset")
		}
	})

	t.Run("invalid preset not registered", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if IsValidPreset("invalid-preset") {
			t.Error("expected IsValidPreset to return false for unregistered preset")
		}
	})

	t.Run("valid HuggingFace model ID format", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if !IsValidPreset("Qwen/Qwen2.5-Coder-7B-Instruct") {
			t.Error("expected IsValidPreset to return true for valid HF model ID")
		}
	})

	t.Run("valid HuggingFace model ID with simple names", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if !IsValidPreset("org/model") {
			t.Error("expected IsValidPreset to return true for simple HF model ID")
		}
	})

	t.Run("invalid HuggingFace model ID with empty org", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if IsValidPreset("/model") {
			t.Error("expected IsValidPreset to return false for empty org part")
		}
	})

	t.Run("invalid HuggingFace model ID with empty model name", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if IsValidPreset("org/") {
			t.Error("expected IsValidPreset to return false for empty model part")
		}
	})

	t.Run("invalid HuggingFace model ID with only slash", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if IsValidPreset("/") {
			t.Error("expected IsValidPreset to return false for just a slash")
		}
	})

	t.Run("valid HuggingFace model ID with multiple slashes", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		// "org/model/version" should be valid as SplitN with n=2 gives ["org", "model/version"]
		if !IsValidPreset("org/model/version") {
			t.Error("expected IsValidPreset to return true for HF model ID with multiple slashes")
		}
	})

	t.Run("empty preset string", func(t *testing.T) {
		KaitoModelRegister = ModelRegister{}

		if IsValidPreset("") {
			t.Error("expected IsValidPreset to return false for empty string")
		}
	})
}
