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
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	//+kubebuilder:scaffold:imports
	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/webhook"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimecache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/featuregates"
	"github.com/kaito-project/kaito/pkg/k8sclient"
	kaitoutils "github.com/kaito-project/kaito/pkg/utils"
	"github.com/kaito-project/kaito/pkg/workspace/controllers"
	"github.com/kaito-project/kaito/pkg/workspace/controllers/garbagecollect"
	"github.com/kaito-project/kaito/pkg/workspace/webhooks"
)

const (
	WebhookServiceName = "WEBHOOK_SERVICE"
	WebhookServicePort = "WEBHOOK_PORT"
)

var (
	scheme = runtime.NewScheme()

	exitWithErrorFunc = func() {
		klog.Flush()
		os.Exit(1)
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kaitov1beta1.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.KarpenterSchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(azurev1alpha2.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.AwsSchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
	klog.InitFlags(nil)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var enableWebhook bool
	var probeAddr string
	var featureGates string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableWebhook, "webhook", true,
		"Enable webhook for controller manager. Default is true.")
	flag.StringVar(&featureGates, "feature-gates", "vLLM=true,disableNodeAutoProvisioning=false", "Enable Kaito feature gates. Default: vLLM=true,disableNodeAutoProvisioning=false.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if err := featuregates.ParseAndValidateFeatureGates(featureGates); err != nil {
		klog.ErrorS(err, "unable to set `feature-gates` flag")
		exitWithErrorFunc()
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	ctx := withShutdownSignal(context.Background())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
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

	workspaceReconciler := controllers.NewWorkspaceReconciler(
		kClient,
		mgr.GetScheme(),
		log.Log.WithName("controllers").WithName("Workspace"),
		mgr.GetEventRecorderFor("KAITO-Workspace-controller"),
	)

	if err = workspaceReconciler.SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "Workspace")
		exitWithErrorFunc()
	}

	pvGCReconciler := garbagecollect.NewPersistentVolumeGCReconciler(
		kClient,
		mgr.GetEventRecorderFor("KAITO-PersistentVolumeGC-controller"),
	)
	if err = pvGCReconciler.SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "PersistentVolumeGC")
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

	if enableWebhook {
		klog.InfoS("starting webhook reconcilers")
		p, err := strconv.Atoi(os.Getenv(WebhookServicePort))
		if err != nil {
			klog.ErrorS(err, "unable to parse the webhook port number")
			exitWithErrorFunc()
		}
		ctx := webhook.WithOptions(ctx, webhook.Options{
			ServiceName: os.Getenv(WebhookServiceName),
			Port:        p,
			SecretName:  "workspace-webhook-cert",
		})
		ctx = sharedmain.WithHealthProbesDisabled(ctx)
		ctx = sharedmain.WithHADisabled(ctx)
		go sharedmain.MainWithConfig(ctx, "webhook", ctrl.GetConfigOrDie(), webhooks.NewWorkspaceWebhooks()...)

		// wait 2 seconds to allow reconciling webhookconfiguration and service endpoint.
		time.Sleep(2 * time.Second)
	}

	klog.InfoS("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
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
