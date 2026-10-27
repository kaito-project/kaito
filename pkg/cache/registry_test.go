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

package cache

import (
	"testing"
)

func TestGetValidatesRegisteredProvider(t *testing.T) {
	isolateProviderRegistry(t)
	Register(&noopTestProvider{})

	if _, err := Get("unknown-provider"); err == nil {
		t.Fatal("expected unknown provider validation to fail")
	}
	if _, err := Get("noop-test"); err != nil {
		t.Fatalf("expected registered provider validation to succeed, got %v", err)
	}
}
