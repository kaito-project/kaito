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
	"fmt"
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
		reg := NewModelRegister(ModelRegistryCacheSize)
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
		reg := NewModelRegister(ModelRegistryCacheSize)
		r1 := &Registration{Name: "model1", Instance: &MockModel{}}
		r2 := &Registration{Name: "model2", Instance: &MockModel{}}

		reg.Register(r1)
		reg.Register(r2)

		if !reg.Has("model1") || !reg.Has("model2") {
			t.Error("expected both models to be registered")
		}
	})

	t.Run("panic on empty name", func(t *testing.T) {
		reg := NewModelRegister(ModelRegistryCacheSize)
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
		reg := NewModelRegister(ModelRegistryCacheSize)
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
		reg := NewModelRegister(ModelRegistryCacheSize)

		result := reg.MustGet("non-existing")

		if result != nil {
			t.Error("expected nil for non-existing model")
		}
	})
}

func TestHas(t *testing.T) {
	t.Run("has existing model", func(t *testing.T) {
		reg := NewModelRegister(ModelRegistryCacheSize)
		reg.Register(&Registration{Name: "test-model", Instance: &MockModel{}})

		if !reg.Has("test-model") {
			t.Error("expected Has to return true for existing model")
		}
	})

	t.Run("has non-existing model", func(t *testing.T) {
		reg := NewModelRegister(ModelRegistryCacheSize)

		if reg.Has("non-existing") {
			t.Error("expected Has to return false for non-existing model")
		}
	})
}

func TestModelRegisterLRU(t *testing.T) {
	reg := NewModelRegister(ModelRegistryCacheSize)
	reg.Register(&Registration{Name: "pinned", Instance: &MockModel{}})
	for i := 0; i < ModelRegistryCacheSize; i++ {
		reg.Register(&Registration{Name: fmt.Sprintf("model-%d", i), Instance: &MockModel{}, Evictable: true})
	}
	// Touch the oldest entry so model-1 becomes the least recently used.
	if !reg.Has("model-0") {
		t.Fatal("expected model-0 to be cached")
	}
	reg.Register(&Registration{Name: "overflow", Instance: &MockModel{}, Evictable: true})

	if reg.Has("model-1") {
		t.Error("expected least recently used model-1 to be evicted")
	}
	for _, name := range []string{"pinned", "model-0", "model-2", "overflow"} {
		if !reg.Has(name) {
			t.Errorf("expected %s to remain registered", name)
		}
	}

	t.Run("re-registering switches between pinned and evictable", func(t *testing.T) {
		reg := NewModelRegister(ModelRegistryCacheSize)
		reg.Register(&Registration{Name: "m", Instance: &MockModel{}, Evictable: true})
		reg.Register(&Registration{Name: "m", Instance: &MockModel{}})
		for i := 0; i < ModelRegistryCacheSize+1; i++ {
			reg.Register(&Registration{Name: fmt.Sprintf("model-%d", i), Instance: &MockModel{}, Evictable: true})
		}
		if !reg.Has("m") {
			t.Error("expected pinned registration to survive cache eviction")
		}
	})
}

func TestIsValidPreset(t *testing.T) {
	prev := KaitoModelRegister
	t.Cleanup(func() { KaitoModelRegister = prev })

	t.Run("valid preset registered in KaitoModelRegister", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)
		KaitoModelRegister.Register(&Registration{Name: "valid-preset", Instance: &MockModel{}})

		if !IsValidPreset("valid-preset") {
			t.Error("expected IsValidPreset to return true for registered preset")
		}
	})

	t.Run("invalid preset not registered", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if IsValidPreset("invalid-preset") {
			t.Error("expected IsValidPreset to return false for unregistered preset")
		}
	})

	t.Run("legacy short preset is valid", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if !IsValidPreset("Phi-4") {
			t.Error("expected IsValidPreset to return true for legacy short preset")
		}
	})

	t.Run("valid HuggingFace model ID format", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if !IsValidPreset("Qwen/Qwen2.5-Coder-7B-Instruct") {
			t.Error("expected IsValidPreset to return true for valid HF model ID")
		}
	})

	t.Run("valid HuggingFace model ID with simple names", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if !IsValidPreset("org/model") {
			t.Error("expected IsValidPreset to return true for simple HF model ID")
		}
	})

	t.Run("invalid HuggingFace model ID with empty org", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if IsValidPreset("/model") {
			t.Error("expected IsValidPreset to return false for empty org part")
		}
	})

	t.Run("invalid HuggingFace model ID with empty model name", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if IsValidPreset("org/") {
			t.Error("expected IsValidPreset to return false for empty model part")
		}
	})

	t.Run("invalid HuggingFace model ID with only slash", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if IsValidPreset("/") {
			t.Error("expected IsValidPreset to return false for just a slash")
		}
	})

	t.Run("valid HuggingFace model ID with multiple slashes", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		// "org/model/version" should be valid as SplitN with n=2 gives ["org", "model/version"]
		if !IsValidPreset("org/model/version") {
			t.Error("expected IsValidPreset to return true for HF model ID with multiple slashes")
		}
	})

	t.Run("empty preset string", func(t *testing.T) {
		KaitoModelRegister = NewModelRegister(ModelRegistryCacheSize)

		if IsValidPreset("") {
			t.Error("expected IsValidPreset to return false for empty string")
		}
	})

}
