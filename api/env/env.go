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

// Package env carries the runtime state that API defaulting and validation
// read, passed through the request context instead of package globals.
package env

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/sku"
)

// NodeProvisioner names the component that provisions GPU nodes.
type NodeProvisioner string

const (
	NodeProvisionerAzureGPU  NodeProvisioner = "azure-gpu-provisioner"
	NodeProvisionerKarpenter NodeProvisioner = "karpenter"
	NodeProvisionerBYO       NodeProvisioner = "byo"
)

// CloudProvider names the cloud KAITO runs in, from CLOUD_PROVIDER.
type CloudProvider string

// Features holds the feature gates that API validation reads.
type Features struct {
	ModelStreaming    bool
	EnableMIG         bool
	EnableAccelerator bool
}

// Env is the runtime state available to API defaulting and validation.
type Env struct {
	// Client is nil when no cluster is available; validators skip lookups then.
	Client   client.Reader
	Features Features
	// NodeProvisioner replaces the disableNodeAutoProvisioning gate: auto
	// provisioning is off when it is NodeProvisionerBYO.
	NodeProvisioner NodeProvisioner
	// NodeClassNames returns the Karpenter NodeClass allowlist. It is a func
	// because the allowlist is only known after the node provisioner starts.
	NodeClassNames   func() []string
	CloudProvider    CloudProvider
	ReleaseNamespace string
	SKUHandler       sku.CloudSKUHandler
}

type envKey struct{}

// WithEnv returns a copy of ctx that carries e.
func WithEnv(ctx context.Context, e *Env) context.Context {
	return context.WithValue(ctx, envKey{}, e)
}

// BaseContext returns a background context that carries e. Use it as the
// manager BaseContext so every controller's Reconcile context carries e.
func (e *Env) BaseContext() context.Context {
	return WithEnv(context.Background(), e)
}

// FromContext returns the Env carried by ctx, or a default Env (no client,
// every feature gate off) when none was injected.
func FromContext(ctx context.Context) *Env {
	if e, ok := ctx.Value(envKey{}).(*Env); ok && e != nil {
		return e
	}
	return &Env{NodeClassNames: func() []string { return nil }}
}
