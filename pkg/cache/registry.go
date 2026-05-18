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

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

var (
	mu        sync.RWMutex
	providers = map[kaitov1beta1.CacheProvider]Provider{}
)

// Register adds a cache provider to the registry.
// Should be called during init() by each provider package.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[kaitov1beta1.CacheProvider(p.Name())] = p
}

// Get returns the provider registered under the given name.
func Get(name kaitov1beta1.CacheProvider) (Provider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("cache provider %q not registered", name)
	}
	return p, nil
}

// RegisteredProviders returns the names of all registered providers.
func RegisteredProviders() []kaitov1beta1.CacheProvider {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]kaitov1beta1.CacheProvider, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	return names
}
