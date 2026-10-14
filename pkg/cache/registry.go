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
	"fmt"
	"sync"
)

var (
	mu        sync.RWMutex
	providers = map[ProviderName]Provider{}
)

// Register adds a cache provider to the registry.
// Should be called during init() by each provider package.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[ProviderName(p.Name())] = p
}

// Get returns the provider registered under the given name.
func Get(name ProviderName) (Provider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("cache provider %q not registered", name)
	}
	return p, nil
}

// List returns all registered provider instances.
func List() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]Provider, 0, len(providers))
	for _, p := range providers {
		result = append(result, p)
	}
	return result
}
