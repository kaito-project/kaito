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

	"github.com/stretchr/testify/assert"

	"github.com/kaito-project/kaito/pkg/model"
)

// mockModel is a simple mock implementation of model.Model interface for testing
type mockModel struct{}

func (m *mockModel) GetInferenceParameters() *model.PresetParam { return nil }
func (m *mockModel) GetTuningParameters() *model.PresetParam    { return nil }
func (m *mockModel) SupportDistributedInference() bool          { return false }
func (m *mockModel) SupportTuning() bool                        { return false }

func TestModelRegister_Register(t *testing.T) {
	t.Run("register new model", func(t *testing.T) {
		reg := &ModelRegister{}
		r := &Registration{
			Name:     "test-model",
			Instance: &mockModel{},
		}
		reg.Register(r)
		assert.True(t, reg.Has("test-model"))
	})

	t.Run("panic when name is empty", func(t *testing.T) {
		reg := &ModelRegister{}
		r := &Registration{
			Name:     "",
			Instance: &mockModel{},
		}
		assert.Panics(t, func() {
			reg.Register(r)
		})
	})

	t.Run("skip registering duplicate model", func(t *testing.T) {
		reg := &ModelRegister{}
		r1 := &Registration{
			Name:     "test-model",
			Instance: &mockModel{},
		}
		r2 := &Registration{
			Name:     "test-model",
			Instance: &mockModel{},
		}
		reg.Register(r1)
		reg.Register(r2) // should skip without panic
		assert.True(t, reg.Has("test-model"))
	})
}

func TestModelRegister_MustGet(t *testing.T) {
	t.Run("get registered model", func(t *testing.T) {
		reg := &ModelRegister{}
		mockInst := &mockModel{}
		reg.Register(&Registration{
			Name:     "test-model",
			Instance: mockInst,
		})

		result := reg.MustGet("test-model")
		assert.Equal(t, mockInst, result)
	})

	t.Run("panic when model not registered", func(t *testing.T) {
		reg := &ModelRegister{}
		assert.Panics(t, func() {
			reg.MustGet("non-existent")
		})
	})
}

func TestModelRegister_ListModelNames(t *testing.T) {
	t.Run("list empty registry", func(t *testing.T) {
		reg := &ModelRegister{}
		names := reg.ListModelNames()
		assert.Empty(t, names)
	})

	t.Run("list multiple models", func(t *testing.T) {
		reg := &ModelRegister{}
		reg.Register(&Registration{Name: "model1", Instance: &mockModel{}})
		reg.Register(&Registration{Name: "model2", Instance: &mockModel{}})
		reg.Register(&Registration{Name: "model3", Instance: &mockModel{}})

		names := reg.ListModelNames()
		assert.Len(t, names, 3)
		assert.Contains(t, names, "model1")
		assert.Contains(t, names, "model2")
		assert.Contains(t, names, "model3")
	})
}

func TestModelRegister_Has(t *testing.T) {
	t.Run("has returns true for registered model", func(t *testing.T) {
		reg := &ModelRegister{}
		reg.Register(&Registration{Name: "test-model", Instance: &mockModel{}})
		assert.True(t, reg.Has("test-model"))
	})

	t.Run("has returns false for non-registered model", func(t *testing.T) {
		reg := &ModelRegister{}
		assert.False(t, reg.Has("non-existent"))
	})
}

func TestIsValidPreset(t *testing.T) {
	// Reset global register for testing
	KaitoModelRegister = ModelRegister{}

	t.Run("returns true for valid preset", func(t *testing.T) {
		KaitoModelRegister.Register(&Registration{Name: "valid-preset", Instance: &mockModel{}})
		assert.True(t, IsValidPreset("valid-preset"))
	})

	t.Run("returns false for invalid preset", func(t *testing.T) {
		assert.False(t, IsValidPreset("invalid-preset"))
	})
}
