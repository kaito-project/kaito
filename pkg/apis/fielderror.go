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

// Package apis provides a lightweight replacement for knative.dev/pkg/apis
// types used by KAITO. This first slice exposes only what non-webhook callers
// need; chaining helpers (Also, ViaField, ErrGeneric) will be added in the
// follow-up PR that migrates the validation files themselves.
package apis

import (
	"fmt"
	"strings"
)

// FieldError represents a validation error for one or more fields. It satisfies
// the error interface so it can be returned from APIs typed as error.
type FieldError struct {
	Message string
	Paths   []string
}

// Error implements the error interface.
func (fe *FieldError) Error() string {
	if fe == nil || fe.Message == "" {
		return ""
	}
	if path := strings.Join(fe.Paths, ", "); path != "" {
		return fmt.Sprintf("%s: %s", fe.Message, path)
	}
	return fe.Message
}

// ErrInvalidValue creates a FieldError indicating an invalid value.
func ErrInvalidValue(value interface{}, field string) *FieldError {
	return &FieldError{
		Message: fmt.Sprintf("invalid value: %v", value),
		Paths:   []string{field},
	}
}

// ErrMissingField creates a FieldError indicating a required field is missing.
func ErrMissingField(fields ...string) *FieldError {
	return &FieldError{
		Message: "missing field(s)",
		Paths:   fields,
	}
}

// ConditionType is a type for condition type constants. It mirrors
// knative.dev/pkg/apis.ConditionType so call sites can be migrated one
// package at a time.
type ConditionType string

// ConditionReady is a condition type indicating readiness.
const ConditionReady ConditionType = "Ready"
