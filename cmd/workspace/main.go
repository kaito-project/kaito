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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	//+kubebuilder:scaffold:imports
	azurev1beta1 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1beta1"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimecache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	drift "github.com/kaito-project/kaito/pkg/controllers/drift"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/inferenceset"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	nodeprovisionmanager "github.com/kaito-project/kaito/pkg/nodeprovision/manager"
	kaitoutils "github.com/kaito-project/kaito/pkg/utils"
	kaitocert "github.com/kaito-project/kaito/pkg/utils/cert"
	"github.com/kaito-project/kaito/pkg/utils/consts"
	"github.com/kaito-project/kaito/pkg/version"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/pkg/workspace/webhooks"
)

const (
	WebhookServiceName = "WEBHOOK_SERVICE"
	WebhookServicePort = "WEBHOOK_PORT"
	WebhookNamespace   = "WEBHOOK_NAMESPACE"

	// webhookCertDir is the on-disk location cert-controller writes its
	// rotated key/cert pair to and that controller-runtime's webhook server
	// reads from. Kept as a constant so the two halves stay in sync.
	webhookCertDir = "/tmp/k8s-webhook-server/serving-certs"

	// webhookSecretName is the Secret cert-controller manages on behalf of
	// the workspace webhook. Matches the legacy Knative SecretName so that
	// existing Helm charts / operator manifests do not need to change.
	webhookSecretName = "workspace-webhook-cert"

	// validatingWebhookConfigName is the cluster-scoped object cert-controller
	// patches with the rotated CA bundle. Matches what the validation webhook
	// registers under (validation.workspace.kaito.sh).
	validatingWebhookConfigName = "validation.workspace.kaito.sh"
)

var (
	scheme = runtime.NewScheme()

	workspaceController = fmt.Sprintf("kaito-workspace/%s", version.Version)

	exitWithErrorFunc = func() {
		klog.Flush()
		os.Exit(1)
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kaitov1alpha1.AddToScheme(scheme))
	utilruntime.Must(kaitov1beta1.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.KarpenterSchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(azurev1beta1.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.AwsSchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
	klog.InitFlags(nil)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var enableWebhook bool
	var probeAddr string
	var featureGates string
	var defaultNodeImageFamily string
	var nodeProvisionerType string
	var karpenterNodeClassGroup string
	var karpenterNodeClassKind string
	var karpenterNodeClassVersion string
	var karpenterNodeClassResourceName string
	var kubeClientQPS int = 30
	var kubeClientBurst int = 50
	var printVersionAndExit bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.IntVar(&kubeClientQPS, "kube-client-qps", kubeClientQPS, "the rate of qps to kube-apiserver.")
	flag.IntVar(&kubeClientBurst, "kube-client-burst", kubeClientBurst, "the max allowed burst of queries to the kube-apiserver.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableWebhook, "webhook", true,
		"Enable webhook for controller manager. Default is true.")
	flag.StringVar(&featureGates, "feature-gates", "vLLM=true,disableNodeAutoProvisioning=false", "Enable Kaito feature gates. Default: vLLM=true,disableNodeAutoProvisioning=false.")
	flag.StringVar(&defaultNodeImageFamily, "default-node-image-family", "", "Default node image family annotation for generated NodeClaims. Supported values: azurelinux, ubuntu. Empty means ubuntu. Unsupported values cause startup failure.")
	flag.StringVar(&nodeProvisionerType, "node-provisioner", "", "Node provisioner type. Supported values: azure-gpu-provisioner, karpenter, byo. Default: azure-gpu-provisioner. If empty, inferred from feature gates for backward compatibility.")
	flag.StringVar(&karpenterNodeClassGroup, "karpenter-node-class-group", "karpenter.azure.com", "Karpenter NodeClass API group. Only used when node-provisioner=karpenter.")
	flag.StringVar(&karpenterNodeClassKind, "karpenter-node-class-kind", "AKSNodeClass", "Karpenter NodeClass API kind. Only used when node-provisioner=karpenter.")
	flag.StringVar(&karpenterNodeClassVersion, "karpenter-node-class-version", "v1beta1", "Karpenter NodeClass API version. Only used when node-provisioner=karpenter.")
	flag.StringVar(&karpenterNodeClassResourceName, "karpenter-node-class-resource-name", "aksnodeclasses", "Plural resource name for the NodeClass CRD (e.g. aksnodeclasses). Combined with --karpenter-node-class-group to form the full CRD name. Only used when node-provisioner=karpenter.")
	flag.BoolVar(&printVersionAndExit, "version", false, "Print version and exit.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if printVersionAndExit {
		fmt.Println(version.VersionInfo())
		os.Exit(0)
	}
	klog.Info("version: ", version.VersionInfo())

	if err := featuregates.ParseAndValidateFeatureGates(featureGates); err != nil {
		klog.ErrorS(err, "unable to set `feature-gates` flag")
		exitWithErrorFunc()
	}

	// Resolve node provisioner type: if --node-provisioner is not explicitly set,
	// infer from feature gates for backward compatibility.
	if nodeProvisionerType == "" {
		switch {
		case featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning]:
			nodeProvisionerType = consts.NodeProvisionerBYO
		default:
			nodeProvisionerType = consts.NodeProvisionerAzureGPU
		}
		klog.InfoS("--node-provisioner not set, inferred from feature gates", "type", nodeProvisionerType)
	}

	// Expose the resolved provisioner type for downstream scheduling logic.
	consts.ActiveNodeProvisioner = nodeProvisionerType

	// Sync feature gate internal state based on --node-provisioner for downstream consumers.
	switch nodeProvisionerType {
	case consts.NodeProvisionerBYO:
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = true
	case consts.NodeProvisionerKarpenter:
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	case consts.NodeProvisionerAzureGPU:
		featuregates.FeatureGates[consts.FeatureFlagDisableNodeAutoProvisioning] = false
	default:
		klog.ErrorS(fmt.Errorf("unsupported node provisioner type %q", nodeProvisionerType), "unable to set --node-provisioner")
		exitWithErrorFunc()
	}

	if defaultNodeImageFamily == "" {
		defaultNodeImageFamily = consts.NodeImageFamilyUbuntu
	} else {
		normalizedNodeImageFamily, valid := consts.NormalizeSupportedNodeImageFamily(defaultNodeImageFamily)
		if !valid {
			klog.ErrorS(fmt.Errorf("unsupported node image family %q", defaultNodeImageFamily), "unable to set `default-node-image-family` flag")
			exitWithErrorFunc()
		}
		defaultNodeImageFamily = normalizedNodeImageFamily
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx := withShutdownSignal(context.Background())

	cfg := ctrl.GetConfigOrDie()
	cfg.UserAgent = workspaceController
	setRestConfig(cfg, kubeClientQPS, kubeClientBurst)

	// kubeClient is built up-front because the Secret-informer-based webhook
	// cert loader (below) needs a kubernetes.Interface before the manager is
	// constructed. It is also re-used as the global client-go client.
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.ErrorS(err, "unable to create kubernetes client")
		exitWithErrorFunc()
	}

	// certReady is closed by cert-controller once it has populated the cert
	// Secret (and disk). We block webhook handler registration on it so the
	// server never serves a request before a real cert exists, but the TCP
	// listener itself binds immediately thanks to the Secret-informer-backed
	// GetCertificate callback installed on the webhook server below.
	certReady := make(chan struct{})

	// webhookServer is wired into ctrl.Options.WebhookServer so that
	// controller-runtime constructs the server with our TLSOpts in place.
	// Mutating mgr.GetWebhookServer() *after* NewManager does not work: the
	// webhook server's Start path checks `cfg.GetCertificate == nil` and
	// otherwise calls certwatcher.New, which fails synchronously when the
	// cert files are not yet on disk and brings the whole manager down.
	// Setting GetCertificate here bypasses certwatcher entirely.
	var webhookServer webhook.Server
	if enableWebhook {
		p, err := strconv.Atoi(os.Getenv(WebhookServicePort))
		if err != nil || p == 0 {
			klog.ErrorS(err, "unable to parse the webhook port number")
			exitWithErrorFunc()
		}
		webhookNS := os.Getenv(WebhookNamespace)
		if webhookNS == "" {
			klog.ErrorS(fmt.Errorf("%s env var not set", WebhookNamespace), "unable to determine webhook namespace")
			exitWithErrorFunc()
		}

		// Namespace-scoped, single-Secret-name informer factory: the lister
		// cache only ever holds the one webhook cert Secret.
		factory := informers.NewSharedInformerFactoryWithOptions(
			kubeClient,
			24*time.Hour,
			informers.WithNamespace(webhookNS),
			informers.WithTweakListOptions(func(o *metav1.ListOptions) {
				o.FieldSelector = fields.OneTermEqualSelector("metadata.name", webhookSecretName).String()
			}),
		)
		secretInformer := factory.Core().V1().Secrets().Informer()
		secretLister := factory.Core().V1().Secrets().Lister()
		factory.Start(ctx.Done())
		if !cache.WaitForCacheSync(ctx.Done(), secretInformer.HasSynced) {
			klog.ErrorS(fmt.Errorf("timed out waiting for informer cache sync"), "unable to sync webhook cert informer")
			exitWithErrorFunc()
		}

		certLoader := kaitocert.NewServerCertLoader(secretLister, webhookNS, webhookSecretName, "tls.crt", "tls.key")
		webhookServer = webhook.NewServer(webhook.Options{
			Port: p,
			TLSOpts: []func(*tls.Config){
				func(c *tls.Config) { c.GetCertificate = certLoader },
			},
		})
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ef60f9b0.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Cache: runtimecache.Options{
			DefaultTransform: runtimecache.TransformStripManagedFields(),
		},
	})
	if err != nil {
		klog.ErrorS(err, "unable to start manager")
		exitWithErrorFunc()
	}

	k8sclient.SetGlobalClient(mgr.GetClient())
	kClient := k8sclient.GetGlobalClient()

	k8sclient.SetGlobalClientGoClient(kubeClient)

	// Create a direct (non-cached) client for provisioner initialization.
	// This is necessary because nodeProvisioner.Start() runs before mgr.Start(),
	// and the manager's cached client is not usable until the cache is started.
	// The direct client is only used for lightweight CRD existence checks and
	// global AKSNodeClass creation during startup.
	directClient, directErr := client.New(cfg, client.Options{Scheme: scheme})
	if directErr != nil {
		klog.ErrorS(directErr, "unable to create direct client for provisioner Start")
		exitWithErrorFunc()
	}

	// Select and initialize the node provisioner based on feature gates.
	recorder := mgr.GetEventRecorderFor("KAITO-Workspace-controller")
	nodeProvisioner := nodeprovisionmanager.NewNodeProvisioner(nodeprovisionmanager.ProvisionerConfig{
		KClient:                kClient,
		DirectClient:           directClient,
		Recorder:               recorder,
		DefaultNodeImageFamily: defaultNodeImageFamily,
		ProvisionerType:        nodeProvisionerType,
		NodeClassGroup:         karpenterNodeClassGroup,
		NodeClassKind:          karpenterNodeClassKind,
		NodeClassVersion:       karpenterNodeClassVersion,
		NodeClassResourceName:  karpenterNodeClassResourceName,
	})
	klog.InfoS("Node provisioner selected", "name", nodeProvisioner.Name())
	if err := nodeProvisioner.Start(ctx); err != nil {
		klog.ErrorS(err, "failed to start node provisioner")
		exitWithErrorFunc()
	}

	workspaceReconciler := controllers.NewWorkspaceReconciler(
		kClient,
		mgr.GetScheme(),
		log.Log.WithName("controllers").WithName("Workspace"),
		recorder,
		nodeProvisioner,
	)

	if err = workspaceReconciler.SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "Workspace")
		exitWithErrorFunc()
	}

	if featuregates.FeatureGates[consts.FeatureFlagEnableInferenceSetController] {
		inferenceSetReconciler := inferenceset.NewInferenceSetReconciler(
			kClient,
			mgr.GetScheme(),
			log.Log.WithName("controllers").WithName("InferenceSet"),
			mgr.GetEventRecorderFor("KAITO-InferenceSet-controller"),
		)

		if err = inferenceSetReconciler.SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "unable to create controller", "controller", "InferenceSet")
			exitWithErrorFunc()
		}

		if consts.IsKarpenterProvisioner() {
			driftReconciler := drift.NewDriftReconciler(
				kClient,
				mgr.GetScheme(),
				mgr.GetEventRecorderFor("drift-controller"),
				nodeProvisioner,
			)
			if err = driftReconciler.SetupWithManager(mgr); err != nil {
				klog.ErrorS(err, "unable to create controller", "controller", "Drift")
				exitWithErrorFunc()
			}
		}
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check")
		exitWithErrorFunc()
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		exitWithErrorFunc()
	}

	if enableWebhook {
		klog.InfoS("setting up cert rotator for webhook")
		webhookNamespace := os.Getenv(WebhookNamespace)
		webhookServiceName := os.Getenv(WebhookServiceName)

		if err := rotator.AddRotator(mgr, &rotator.CertRotator{
			SecretKey: types.NamespacedName{
				Namespace: webhookNamespace,
				Name:      webhookSecretName,
			},
			CertDir:        webhookCertDir,
			CAName:         "kaito-workspace-ca",
			CAOrganization: "kaito",
			DNSName:        fmt.Sprintf("%s.%s.svc", webhookServiceName, webhookNamespace),
			IsReady:        certReady,
			Webhooks: []rotator.WebhookInfo{{
				Name: validatingWebhookConfigName,
				Type: rotator.Validating,
			}},
		}); err != nil {
			klog.ErrorS(err, "unable to set up cert rotator")
			exitWithErrorFunc()
		}

		// Register webhook handlers only after cert-controller signals the
		// cert is on disk. Done in a goroutine so it doesn't block mgr.Start.
		go func() {
			select {
			case <-certReady:
				klog.InfoS("cert rotator reports certs are ready, registering webhooks")
				if err := webhooks.SetupWebhooksWithManager(mgr); err != nil {
					klog.ErrorS(err, "unable to register webhooks")
					exitWithErrorFunc()
				}
			case <-ctx.Done():
				return
			}
		}()
	} else {
		// No webhook: don't make downstream code wait on certReady.
		close(certReady)
	}

	klog.InfoS("starting manager")
	if err := mgr.Start(ctx); err != nil {
		klog.ErrorS(err, "problem running manager")
		exitWithErrorFunc()
	}
}

// withShutdownSignal returns a copy of the parent context that will close if
// the process receives termination signals.
func withShutdownSignal(ctx context.Context) context.Context {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT, os.Interrupt)

	nctx, cancel := context.WithCancel(ctx)

	go func() {
		<-signalChan
		klog.Info("received shutdown signal")
		cancel()
	}()
	return nctx
}

func setRestConfig(c *rest.Config, kubeClientQPS, kubeClientBurst int) {
	if kubeClientQPS > 0 {
		c.QPS = float32(kubeClientQPS)
	}
	if kubeClientBurst > 0 {
		c.Burst = kubeClientBurst
	}
}
