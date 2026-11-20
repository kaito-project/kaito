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

package ragengine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	knativeresource "knative.dev/pkg/observability/resource"
	"knative.dev/pkg/observability/semconv"
)

func TestDefaultTelemetryResource(t *testing.T) {
	t.Setenv("SYSTEM_NAMESPACE", "kaito-system")
	t.Setenv("POD_NAME", "ragengine-controller")

	for _, tt := range []struct {
		name        string
		override    string
		serviceName string
	}{
		{name: "default service", serviceName: "webhook"},
		{name: "environment override", override: "custom-webhook", serviceName: "custom-webhook"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_SERVICE_NAME", tt.override)

			var resource *otelresource.Resource
			require.NotPanics(t, func() {
				resource = knativeresource.Default("webhook")
			})
			require.NotNil(t, resource)
			assert.Equal(t, otelresource.Default().SchemaURL(), resource.SchemaURL())
			assert.Equal(t, semconv.SchemaURL, resource.SchemaURL())
			assert.Contains(t, resource.Attributes(), semconv.ServiceName(tt.serviceName))
			assert.Contains(t, resource.Attributes(), semconv.K8SNamespaceName("kaito-system"))
			assert.Contains(t, resource.Attributes(), semconv.K8SPodName("ragengine-controller"))
		})
	}
}
