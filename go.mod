module github.com/kaito-project/kaito

go 1.26.10

require (
	github.com/Azure/karpenter-provider-azure v1.10.2
	github.com/aws/karpenter-provider-aws v1.10.0
	github.com/awslabs/operatorpkg v0.0.0-20251222193911-34e9a1898737
	github.com/distribution/reference v0.6.0
	github.com/fluxcd/helm-controller/api v1.3.0
	github.com/fluxcd/pkg/apis/kustomize v1.10.0
	github.com/fluxcd/source-controller/api v1.6.0
	github.com/go-logr/logr v1.4.4
	github.com/gowebpki/jcs v1.0.1
	github.com/onsi/ginkgo/v2 v2.28.1
	github.com/onsi/gomega v1.39.1
	github.com/prometheus/client_golang v1.25.0
	github.com/prometheus/client_model v0.6.3
	github.com/prometheus/common v0.72.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/samber/lo v1.52.0
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.36.5
	k8s.io/apiextensions-apiserver v0.36.5
	k8s.io/apimachinery v0.36.5
	k8s.io/client-go v0.36.5
	k8s.io/component-base v0.36.5
	k8s.io/klog/v2 v2.140.0
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2
	knative.dev/pkg v0.0.0-20261026113028-cfa0ac991c5a
	sigs.k8s.io/controller-runtime v0.22.4
	sigs.k8s.io/gateway-api-inference-extension v1.3.1
	sigs.k8s.io/karpenter v1.10.0
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.296.0 // indirect
	github.com/aws/smithy-go v1.24.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/blendle/zapdriver v1.3.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fluxcd/pkg/apis/acl v0.7.0 // indirect
	github.com/fluxcd/pkg/apis/meta v1.12.0 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/go-openapi/jsonreference v1.0.0 // indirect
	github.com/go-openapi/swag v0.28.0 // indirect
	github.com/go-openapi/swag/cmdutils v0.28.0 // indirect
	github.com/go-openapi/swag/conv v0.28.0 // indirect
	github.com/go-openapi/swag/fileutils v0.28.0 // indirect
	github.com/go-openapi/swag/jsonutils v0.28.0 // indirect
	github.com/go-openapi/swag/loading v0.28.0 // indirect
	github.com/go-openapi/swag/mangling v0.28.0 // indirect
	github.com/go-openapi/swag/netutils v0.28.0 // indirect
	github.com/go-openapi/swag/pools v0.28.0 // indirect
	github.com/go-openapi/swag/stringutils v0.28.0 // indirect
	github.com/go-openapi/swag/typeutils v0.28.0 // indirect
	github.com/go-openapi/swag/yamlutils v0.28.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gobuffalo/flect v1.0.3 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260115054156-294ebfa9ad83 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/moby/spdystream v0.5.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.1-0.20231025023718-d50d2fec9c98 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/otlptranslator v1.0.0 // indirect
	github.com/prometheus/procfs v0.22.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/runtime v0.72.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.69.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/automaxprocs v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/cloud-provider v0.35.0 // indirect
	k8s.io/component-helpers v0.35.0 // indirect
	k8s.io/csi-translation-lib v0.35.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260317180543-43fb72c5454a // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.3 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.35.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.35.0
	k8s.io/client-go => k8s.io/client-go v0.35.0
	k8s.io/component-base => k8s.io/component-base v0.35.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.35.0
)
