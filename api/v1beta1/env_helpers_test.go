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

package v1beta1

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/api/env"
	"github.com/kaito-project/kaito/pkg/consts"
	"github.com/kaito-project/kaito/pkg/sku"
)

// testCtx returns a context carrying an Azure Env with node auto
// provisioning on, after applying mods.
func testCtx(mods ...func(*env.Env)) context.Context {
	e := &env.Env{
		NodeProvisioner:  env.NodeProvisionerAzureGPU,
		NodeClassNames:   func() []string { return nil },
		CloudProvider:    consts.AzureCloudName,
		ReleaseNamespace: DefaultReleaseNamespace,
		SKUHandler:       sku.GetCloudSKUHandler(consts.AzureCloudName),
	}
	for _, mod := range mods {
		mod(e)
	}
	return env.WithEnv(context.Background(), e)
}

func withClient(c client.Reader) func(*env.Env) {
	return func(e *env.Env) { e.Client = c }
}

// withNodeProvisioner sets the provisioner; NodeProvisionerBYO turns node
// auto provisioning off.
func withNodeProvisioner(p env.NodeProvisioner) func(*env.Env) {
	return func(e *env.Env) { e.NodeProvisioner = p }
}

func withFeatures(f env.Features) func(*env.Env) {
	return func(e *env.Env) { e.Features = f }
}

// withCloud sets the cloud provider and its SKU handler.
func withCloud(provider string) func(*env.Env) {
	return func(e *env.Env) {
		e.CloudProvider = env.CloudProvider(provider)
		e.SKUHandler = sku.GetCloudSKUHandler(provider)
	}
}
