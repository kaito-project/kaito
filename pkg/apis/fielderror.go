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

// Package apis provides a lightweight replacement for the subset of
// knative.dev/pkg/apis that KAITO's validation surface depends on. It is an
// independent reimplementation written to allow KAITO to drop its dependency
// on knative.dev/pkg without rewriting every Validate(...) method at once.
//
// The chaining helpers (Also, ViaField), the generic constructor (ErrGeneric),
// and the create-vs-update baseline helpers (GetBaseline, WithBaseline) mirror
// the semantics of the upstream Knative API closely enough that existing
// validation code can be migrated by changing only the import path. A future
// PR will replace this shim with k8s.io/apimachinery/pkg/util/validation/field
// and delete this package.
package apis

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// FieldError represents one or more validation errors for fields on an API
// object. It satisfies the error interface so it can be returned from APIs
// typed as error.
//
// FieldError values are intended to be accumulated via Also and re-pathed via
// ViaField. All methods are nil-safe on the receiver so that callers can write
//
//	var errs *FieldError
//	errs = errs.Also(somethingValidate())
//
// without first checking for nil.
type FieldError struct {
	Message string
	Paths   []string
	Details string

	// errors holds additional sibling errors accumulated through Also. It is
	// kept unexported because callers should always go through Also/ViaField
	// to manipulate the chain, which preserves nil-safety.
	errors []FieldError
}

// Error implements the error interface. It flattens the receiver and any
// accumulated sibling errors into a single newline-separated message in the
// same general shape as knative.dev/pkg/apis.FieldError.Error.
func (fe *FieldError) Error() string {
	if fe == nil {
		return ""
	}

	all := fe.flatten()
	if len(all) == 0 {
		return ""
	}

	// Stable order so test assertions and log output are deterministic.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Message != all[j].Message {
			return all[i].Message < all[j].Message
		}
		return strings.Join(all[i].Paths, ",") < strings.Join(all[j].Paths, ",")
	})

	parts := make([]string, 0, len(all))
	for _, e := range all {
		parts = append(parts, e.formatLeaf())
	}
	return strings.Join(parts, "\n")
}

// formatLeaf renders a single error entry (no children) as a string.
func (fe *FieldError) formatLeaf() string {
	if fe == nil || fe.Message == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(fe.Message)
	if path := strings.Join(dedupe(fe.Paths), ", "); path != "" {
		b.WriteString(": ")
		b.WriteString(path)
	}
	if fe.Details != "" {
		b.WriteString("\n")
		b.WriteString(fe.Details)
	}
	return b.String()
}

// flatten returns the receiver plus all accumulated sibling errors as a flat
// slice of leaf entries (i.e. each returned FieldError has no children of its
// own). Entries with an empty Message are dropped so that the zero value of
// FieldError contributes nothing.
func (fe *FieldError) flatten() []FieldError {
	if fe == nil {
		return nil
	}
	var out []FieldError
	if fe.Message != "" {
		out = append(out, FieldError{
			Message: fe.Message,
			Paths:   append([]string(nil), fe.Paths...),
			Details: fe.Details,
		})
	}
	for i := range fe.errors {
		out = append(out, fe.errors[i].flatten()...)
	}
	return out
}

// Also accumulates errs as siblings of the receiver and returns the combined
// FieldError. The variant is variadic to match Knative's signature so callers
// can write fe.Also(a, b, c). Both sides are nil-safe: Also() and Also(nil)
// return the receiver, and (*FieldError)(nil).Also(x) returns x.
func (fe *FieldError) Also(others ...*FieldError) *FieldError {
	out := fe
	for _, o := range others {
		out = out.alsoOne(o)
	}
	return out
}

// alsoOne is the single-argument worker for Also.
func (fe *FieldError) alsoOne(errs *FieldError) *FieldError {
	// Drop empty / nil inputs.
	if errs == nil || errs.isEmpty() {
		if fe == nil || fe.isEmpty() {
			return nil
		}
		return fe
	}
	if fe == nil || fe.isEmpty() {
		return errs
	}

	merged := &FieldError{
		Message: fe.Message,
		Paths:   append([]string(nil), fe.Paths...),
		Details: fe.Details,
		errors:  append([]FieldError(nil), fe.errors...),
	}
	// Append the new error's own leaf (if any) plus all of its accumulated
	// children, so chaining a.Also(b).Also(c) does not lose entries from b.
	if errs.Message != "" {
		merged.errors = append(merged.errors, FieldError{
			Message: errs.Message,
			Paths:   append([]string(nil), errs.Paths...),
			Details: errs.Details,
		})
	}
	merged.errors = append(merged.errors, errs.errors...)
	return merged
}

// ViaField returns a copy of the receiver with prefix prepended to every
// path on every accumulated error. Multiple prefix segments are joined in
// order, e.g. ViaField("spec", "resource") prepends "spec.resource.".
//
// ViaField is nil-safe.
func (fe *FieldError) ViaField(prefix ...string) *FieldError {
	if fe == nil || fe.isEmpty() {
		return nil
	}
	pfx := joinPrefix(prefix)
	if pfx == "" {
		return fe
	}

	out := &FieldError{
		Message: fe.Message,
		Paths:   prependPaths(pfx, fe.Paths),
		Details: fe.Details,
		errors:  make([]FieldError, len(fe.errors)),
	}
	for i := range fe.errors {
		child := fe.errors[i]
		out.errors[i] = FieldError{
			Message: child.Message,
			Paths:   prependPaths(pfx, child.Paths),
			Details: child.Details,
			errors:  child.errors, // already flattened by Also
		}
	}
	return out
}

// ViaIndex is a convenience wrapper for ViaField that produces an indexed
// path segment of the form "[i]". It mirrors the Knative helper of the same
// name.
func (fe *FieldError) ViaIndex(index int) *FieldError {
	return fe.ViaField(fmt.Sprintf("[%d]", index))
}

// ErrInvalidValue creates a FieldError indicating an invalid value at the
// given field path. Optional extra strings are joined with ", " and stored as
// Details, matching Knative's variadic signature.
func ErrInvalidValue(value any, field string, details ...string) *FieldError {
	return &FieldError{
		Message: fmt.Sprintf("invalid value: %v", value),
		Paths:   []string{field},
		Details: strings.Join(details, ", "),
	}
}

// ErrMissingField creates a FieldError indicating a required field is missing.
func ErrMissingField(fields ...string) *FieldError {
	return &FieldError{
		Message: "missing field(s)",
		Paths:   fields,
	}
}

// ErrGeneric creates a FieldError with an arbitrary message and an optional
// list of paths.
func ErrGeneric(msg string, paths ...string) *FieldError {
	return &FieldError{
		Message: msg,
		Paths:   append([]string(nil), paths...),
	}
}

// ConditionType is a type for condition type constants. It mirrors
// knative.dev/pkg/apis.ConditionType so call sites can be migrated one
// package at a time.
type ConditionType string

// ConditionReady is a condition type indicating readiness.
const ConditionReady ConditionType = "Ready"

// baselineKey is the unexported context key used by GetBaseline / WithBaseline.
// Using an unexported struct type prevents collisions with other packages'
// context values.
type baselineKey struct{}

// WithBaseline returns a new context that carries obj as the "baseline"
// (typically the existing object on the server side of an UPDATE admission
// request). Validate methods that need to distinguish create from update can
// call GetBaseline(ctx) to retrieve it.
func WithBaseline(ctx context.Context, obj any) context.Context {
	return context.WithValue(ctx, baselineKey{}, obj)
}

// WithinUpdate is an alias for WithBaseline that reads more naturally at
// admission-controller call sites (and matches the Knative spelling).
func WithinUpdate(ctx context.Context, base any) context.Context {
	return WithBaseline(ctx, base)
}

// WithinCreate returns ctx unchanged. It exists as the create-side counterpart
// to WithinUpdate so callers can be explicit about intent.
func WithinCreate(ctx context.Context) context.Context {
	return ctx
}

// GetBaseline returns the baseline object previously stored on ctx by
// WithBaseline, or nil if no baseline is present (e.g. during CREATE).
func GetBaseline(ctx context.Context) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(baselineKey{})
}

// isEmpty reports whether the receiver has no message and no accumulated
// children. nil is considered empty.
func (fe *FieldError) isEmpty() bool {
	if fe == nil {
		return true
	}
	if fe.Message != "" {
		return false
	}
	for i := range fe.errors {
		if !fe.errors[i].isEmpty() {
			return false
		}
	}
	return true
}

func joinPrefix(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ".")
}

// prependPaths returns a new slice with prefix prepended to each non-empty
// element of paths using "." as a separator. If paths is empty, the result is
// a single-element slice containing prefix so the error still has a path.
func prependPaths(prefix string, paths []string) []string {
	if len(paths) == 0 {
		return []string{prefix}
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		switch {
		case p == "":
			out = append(out, prefix)
		case strings.HasPrefix(p, "[") || strings.HasPrefix(p, "."):
			// Index or already-dotted continuation: don't insert a dot.
			out = append(out, prefix+p)
		default:
			out = append(out, prefix+"."+p)
		}
	}
	return out
}

// dedupe returns a copy of in with duplicate consecutive entries removed,
// preserving order of first occurrence. It is intended for path lists where
// duplicates can arise from re-pathing under nested ViaField calls.
func dedupe(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
