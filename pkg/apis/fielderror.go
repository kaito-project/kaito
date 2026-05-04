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
// error types used in webhook validation. It implements the same chaining
// API (Also, ViaField) so validation code requires only an import path change.
package apis

import (
	"fmt"
	"strings"
)

// FieldError represents one or more validation errors, supporting
// nil-safe chaining with Also() and field-path scoping with ViaField().
type FieldError struct {
	Message string
	Paths   []string
	Details string
	errors  []*FieldError // child errors accumulated via Also()
}

// Error implements the error interface.
func (fe *FieldError) Error() string {
	if fe == nil {
		return ""
	}
	var msgs []string
	fe.collectMessages(&msgs)
	return strings.Join(msgs, "; ")
}

func (fe *FieldError) collectMessages(msgs *[]string) {
	if fe == nil {
		return
	}
	if fe.Message != "" {
		path := strings.Join(fe.Paths, ", ")
		if path != "" {
			*msgs = append(*msgs, fmt.Sprintf("%s: %s", fe.Message, path))
		} else {
			*msgs = append(*msgs, fe.Message)
		}
	}
	for _, child := range fe.errors {
		child.collectMessages(msgs)
	}
}

// Also accumulates another FieldError. Both receiver and argument may be nil.
func (fe *FieldError) Also(others ...*FieldError) *FieldError {
	var nonNil []*FieldError
	if fe != nil {
		nonNil = append(nonNil, fe)
	}
	for _, o := range others {
		if o != nil {
			nonNil = append(nonNil, o)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	default:
		return &FieldError{errors: nonNil}
	}
}

// ViaField prepends a field path segment to all errors in this FieldError.
func (fe *FieldError) ViaField(field string) *FieldError {
	if fe == nil || field == "" {
		return fe
	}
	fe.prependPath(field)
	return fe
}

func (fe *FieldError) prependPath(prefix string) {
	if fe == nil {
		return
	}
	if len(fe.Paths) > 0 {
		for i, p := range fe.Paths {
			if p == "" {
				fe.Paths[i] = prefix
			} else {
				fe.Paths[i] = prefix + "." + p
			}
		}
	} else if fe.Message != "" {
		fe.Paths = []string{prefix}
	}
	for _, child := range fe.errors {
		child.prependPath(prefix)
	}
}

// ErrGeneric creates a FieldError with the given message and optional field paths.
func ErrGeneric(msg string, paths ...string) *FieldError {
	return &FieldError{
		Message: msg,
		Paths:   paths,
	}
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

// ConditionType is a type for condition type constants.
type ConditionType string

// ConditionReady is a condition type indicating readiness.
// This replaces knative.dev/pkg/apis.ConditionReady.
const ConditionReady ConditionType = "Ready"
