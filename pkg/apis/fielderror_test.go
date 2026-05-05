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

package apis

import (
	"context"
	"strings"
	"testing"
)

func TestFieldErrorNilSafety(t *testing.T) {
	var fe *FieldError
	if got := fe.Error(); got != "" {
		t.Errorf("nil.Error() = %q, want empty", got)
	}
	if fe.Also(nil) != nil {
		t.Errorf("nil.Also(nil) should be nil")
	}
	if fe.ViaField("spec") != nil {
		t.Errorf("nil.ViaField should be nil")
	}
}

func TestErrInvalidValue(t *testing.T) {
	fe := ErrInvalidValue("bad", "spec.foo")
	if !strings.Contains(fe.Error(), "invalid value: bad") {
		t.Errorf("missing message: %q", fe.Error())
	}
	if !strings.Contains(fe.Error(), "spec.foo") {
		t.Errorf("missing path: %q", fe.Error())
	}
}

func TestErrMissingField(t *testing.T) {
	fe := ErrMissingField("a", "b")
	got := fe.Error()
	if !strings.Contains(got, "missing field") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("missing paths in %q", got)
	}
}

func TestErrGeneric(t *testing.T) {
	fe := ErrGeneric("boom", "x", "y")
	got := fe.Error()
	if !strings.Contains(got, "boom") || !strings.Contains(got, "x") || !strings.Contains(got, "y") {
		t.Errorf("got %q", got)
	}

	// No paths.
	fe2 := ErrGeneric("naked")
	if got := fe2.Error(); got != "naked" {
		t.Errorf("naked got %q want %q", got, "naked")
	}
}

func TestAlsoAccumulates(t *testing.T) {
	a := ErrGeneric("first", "f1")
	b := ErrGeneric("second", "f2")
	c := ErrGeneric("third", "f3")
	combined := a.Also(b).Also(c)

	got := combined.Error()
	for _, want := range []string{"first", "second", "third", "f1", "f2", "f3"} {
		if !strings.Contains(got, want) {
			t.Errorf("combined error %q missing %q", got, want)
		}
	}
}

func TestAlsoNilSides(t *testing.T) {
	a := ErrGeneric("only", "p")

	if got := a.Also(nil).Error(); !strings.Contains(got, "only") {
		t.Errorf("Also(nil) dropped errors: %q", got)
	}
	var nilFE *FieldError
	if got := nilFE.Also(a).Error(); !strings.Contains(got, "only") {
		t.Errorf("nil.Also(a) dropped errors: %q", got)
	}
}

func TestAlsoEmpty(t *testing.T) {
	empty := &FieldError{}
	a := ErrGeneric("only", "p")
	if got := a.Also(empty).Error(); !strings.Contains(got, "only") {
		t.Errorf("Also(empty) dropped: %q", got)
	}
	if got := empty.Also(a).Error(); !strings.Contains(got, "only") {
		t.Errorf("empty.Also(a) dropped: %q", got)
	}
	if empty.Also(nil) != nil {
		t.Errorf("empty.Also(nil) should be nil")
	}
}

func TestViaFieldPrependsPaths(t *testing.T) {
	fe := ErrGeneric("nope", "name").Also(ErrInvalidValue("x", "value"))
	scoped := fe.ViaField("spec", "child")

	got := scoped.Error()
	if !strings.Contains(got, "spec.child.name") {
		t.Errorf("missing prefixed path 'spec.child.name' in %q", got)
	}
	if !strings.Contains(got, "spec.child.value") {
		t.Errorf("missing prefixed path 'spec.child.value' in %q", got)
	}
}

func TestViaFieldEmptyPath(t *testing.T) {
	fe := ErrGeneric("naked")
	scoped := fe.ViaField("root")
	if !strings.Contains(scoped.Error(), "root") {
		t.Errorf("expected path 'root' to be added to a path-less error: %q", scoped.Error())
	}
}

func TestViaIndex(t *testing.T) {
	fe := ErrGeneric("nope", "name").ViaIndex(2).ViaField("adapters")
	got := fe.Error()
	if !strings.Contains(got, "adapters[2].name") {
		t.Errorf("expected adapters[2].name in %q", got)
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	type obj struct{ Name string }
	original := &obj{Name: "old"}

	if got := GetBaseline(context.Background()); got != nil {
		t.Errorf("expected nil baseline on bare context, got %v", got)
	}

	ctx := WithBaseline(context.Background(), original)
	got := GetBaseline(ctx)
	if got == nil {
		t.Fatalf("expected non-nil baseline")
	}
	if got.(*obj) != original {
		t.Errorf("baseline round trip mismatch: %v vs %v", got, original)
	}

	// nil ctx must not panic. We construct it via a variable so static
	// analyzers don't flag the literal nil context argument.
	var nilCtx context.Context
	if got := GetBaseline(nilCtx); got != nil {
		t.Errorf("expected nil baseline from nil ctx, got %v", got)
	}
}

func TestDetailsRendered(t *testing.T) {
	fe := &FieldError{Message: "boom", Paths: []string{"x"}, Details: "extra info"}
	got := fe.Error()
	if !strings.Contains(got, "boom") || !strings.Contains(got, "x") || !strings.Contains(got, "extra info") {
		t.Errorf("expected message, path, and details in %q", got)
	}
}

func TestErrorImplementsErrorInterface(t *testing.T) {
	var _ error = (*FieldError)(nil)
}
