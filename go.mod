module github.com/kaito-project/kaito

go 1.24.0

toolchain go1.24.1

require (
	github.com/Azure/karpenter-provider-azure v0.7.0
	github.com/aws/karpenter-provider-aws v1.0.6
	github.com/awslabs/operatorpkg v0.0.0-20240805231134-67d0acfb6306
	github.com/distribution/reference v0.5.0
	github.com/go-logr/logr v1.4.2
	github.com/onsi/ginkgo/v2 v2.22.2
	github.com/onsi/gomega v1.36.2
	github.com/prometheus/client_golang v1.20.5
	github.com/samber/lo v1.49.1
	github.com/stretchr/testify v1.10.0
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.30.3
	k8s.io/apimachinery v0.30.3
	k8s.io/client-go v0.30.3
	k8s.io/component-base v0.30.3
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubernetes v1.31.0
	k8s.io/utils v0.0.0-20240711033017-18e509b52bc8
	knative.dev/pkg v0.0.0-20240910170930-fdbc0b5adde7
	sigs.k8s.io/controller-runtime v0.18.4
	sigs.k8s.io/karpenter v1.0.4
)

require (
	contrib.go.opencensus.io/exporter/ocagent v0.7.1-0.20200907061046-05415f1de66d // indirect
	contrib.go.opencensus.io/exporter/prometheus v0.4.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/blendle/zapdriver v1.3.1 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/evanphx/json-patch v5.9.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gobuffalo/flect v1.0.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20241210010833-40e02aabc2ad // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.21.0 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/hashstructure/v2 v2.0.2 // indirect
	github.com/moby/spdystream v0.4.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/prometheus/statsd_exporter v0.24.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/spf13/cobra v1.8.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.uber.org/automaxprocs v1.5.3 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/exp v0.0.0-20240808152545-0cdaa3abc0fa // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/oauth2 v0.22.0 // indirect
	golang.org/x/sync v0.12.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/term v0.30.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	golang.org/x/time v0.6.0 // indirect
	golang.org/x/tools v0.28.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/api v0.183.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240808171019-573a1156607a // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240808171019-573a1156607a // indirect
	google.golang.org/grpc v1.65.0 // indirect
	google.golang.org/protobuf v1.36.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.30.3 // indirect
	k8s.io/apiserver v0.30.1 // indirect
	k8s.io/cloud-provider v0.30.3 // indirect
	k8s.io/csi-translation-lib v0.30.3 // indirect
	k8s.io/kube-openapi v0.0.0-20240808142205-8e686545bdb8 // indirect
	k8s.io/pod-security-admission v0.0.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	k8s.io/api => k8s.io/api v0.30.1
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.30.1
	k8s.io/apimachinery => k8s.io/apimachinery v0.30.1
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.30.1
	k8s.io/client-go => k8s.io/client-go v0.30.1
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.30.1
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.30.1
	k8s.io/code-generator => k8s.io/code-generator v0.30.1
	k8s.io/component-base => k8s.io/component-base v0.30.1
	k8s.io/component-helpers => k8s.io/component-helpers v0.30.1
	k8s.io/controller-manager => k8s.io/controller-manager v0.30.1
	k8s.io/cri-api => k8s.io/cri-api v0.30.1
	k8s.io/cri-client => k8s.io/cri-client v0.30.1
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.30.1
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.30.1
	k8s.io/endpointslice => k8s.io/endpointslice v0.30.1
	k8s.io/kms => k8s.io/kms v0.30.1
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.30.1
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.30.1
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.30.1
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.30.1
	k8s.io/kubectl => k8s.io/kubectl v0.30.1
	k8s.io/kubelet => k8s.io/kubelet v0.30.1
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.30.1
	k8s.io/metrics => k8s.io/metrics v0.30.1
	k8s.io/mount-utils => k8s.io/mount-utils v0.30.1
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.30.1
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.30.1
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.30.1
	k8s.io/sample-controller => k8s.io/sample-controller v0.30.1
)
