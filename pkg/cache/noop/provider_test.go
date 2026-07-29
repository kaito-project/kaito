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

package noop

import (
	"context"
	"testing"

	"github.com/kaito-project/kaito/pkg/cache"
)

func TestProvider(t *testing.T) {
	p := NewProvider()

	if p.Name() != ProviderName {
		t.Fatalf("Name: got %q, want %q", p.Name(), ProviderName)
	}

	available, err := p.IsAvailable(context.Background(), "")
	if err != nil {
		t.Fatalf("IsAvailable returned error: %v", err)
	}
	if !available {
		t.Fatal("IsAvailable: got false, want true")
	}

	ready, _, err := p.IsReady(context.Background(), "")
	if err != nil {
		t.Fatalf("IsReady returned error: %v", err)
	}
	if !ready {
		t.Fatal("IsReady: got false, want true")
	}

	mutations, err := p.PodMutations(context.Background(), cache.CacheConcernModelWeights, nil, "", "", "")
	if err != nil {
		t.Fatalf("PodMutations returned error: %v", err)
	}
	if mutations == nil {
		t.Fatal("PodMutations returned nil")
	}
	if len(mutations.Labels) != 0 || len(mutations.EnvVars) != 0 || len(mutations.Volumes) != 0 || len(mutations.VolumeMounts) != 0 || len(mutations.InitContainers) != 0 {
		t.Fatalf("expected empty mutations, got %+v", mutations)
	}

	if err := p.Cleanup(context.Background(), nil, ""); err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}

	if obj := p.EventObject(); obj != nil {
		t.Fatalf("EventObject: got %T, want nil", obj)
	}
}
