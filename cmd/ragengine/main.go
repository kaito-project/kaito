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
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
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
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	"github.com/kaito-project/kaito/pkg/ragengine/controllers"
	"github.com/kaito-project/kaito/pkg/ragengine/webhooks"
	kaitoutils "github.com/kaito-project/kaito/pkg/utils"
	kaitocert "github.com/kaito-project/kaito/pkg/utils/cert"
	"github.com/kaito-project/kaito/pkg/version"
)

const (
	WebhookServiceName = "WEBHOOK_SERVICE"
	WebhookServicePort = "WEBHOOK_PORT"
	WebhookNamespace   = "SYSTEM_NAMESPACE"

	// webhookCertDir is where cert-controller writes the rotated key/cert pair
	// and where controller-runtime's webhook server reads from.
	webhookCertDir = "/tmp/k8s-webhook-server/serving-certs"

	// webhookSecretName is the Secret cert-controller manages on behalf of
	// the ragengine webhook. Matches the chart-managed Secret.
	webhookSecretName = "ragengine-webhook-cert"

	// validatingWebhookConfigName matches the ValidatingWebhookConfiguration
	// shipped in charts/kaito/ragengine/templates/webhooks.yaml.
	validatingWebhookConfigName = "validation.ragengine.kaito.sh"
)

var (
	scheme = runtime.NewScheme()

	ragengineController = fmt.Sprintf("kaito-ragengine/%s", version.Version)

	exitWithErrorFunc = func() {
		klog.Flush()
		os.Exit(1)
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kaitov1beta1.AddToScheme(scheme))
	utilruntime.Must(kaitov1alpha1.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.KarpenterSchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(azurev1beta1.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.AwsSchemeBuilder.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
	klog.InitFlags(nil)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var enableWebhook bool
	var probeAddr string
	var featureGates string
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
	flag.StringVar(&featureGates, "feature-gates", "vLLM=true", "Enable Kaito feature gates. Default,	vLLM=true.")
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

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx := withShutdownSignal(context.Background())

	cfg := ctrl.GetConfigOrDie()
	cfg.UserAgent = ragengineController
	setRestConfig(cfg, kubeClientQPS, kubeClientBurst)

	// kubeClient is built up-front because the Secret-informer-based webhook
	// cert loader (below) needs a kubernetes.Interface before the manager is
	// constructed.
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.ErrorS(err, "unable to create kubernetes client")
		exitWithErrorFunc()
	}

	// certReady is closed by cert-controller once the cert Secret (and disk
	// material) is populated. We block webhook handler registration on it
	// while letting the TCP listener bind immediately via the Secret-informer-
	// backed GetCertificate callback installed on the webhook server below.
	certReady := make(chan struct{})

	// webhookServer is wired into ctrl.Options.WebhookServer so that
	// controller-runtime constructs the server with our TLSOpts in place.
	// Mutating mgr.GetWebhookServer() *after* NewManager does not work: the
	// server's Start path checks `cfg.GetCertificate == nil` and otherwise
	// calls certwatcher.New, which fails synchronously if the cert files are
	// not on disk yet and brings the manager down. Setting GetCertificate via
	// TLSOpts here bypasses certwatcher entirely.
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
		LeaderElectionID:       "ef60f9b1.io",
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

	ragengineReconciler := controllers.NewRAGEngineReconciler(
		kClient,
		mgr.GetScheme(),
		log.Log.WithName("controllers").WithName("RAGEngine"),
		mgr.GetEventRecorderFor("KAITO-RAGEngine-controller"),
	)

	if err = ragengineReconciler.SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "RAG Eingine")
		exitWithErrorFunc()
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

	// certReady is closed by cert-controller once TLS material on disk is
	// usable. We block webhook handler registration on it so the webhook
	// server never serves a request without a valid cert.
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
			CAName:         "kaito-ragengine-ca",
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
