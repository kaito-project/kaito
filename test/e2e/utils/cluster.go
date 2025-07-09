// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	kaitoutils "github.com/kaito-project/kaito/pkg/utils"
)

const (
	E2eNamespace = "kaito-e2e"
)

var (
	scheme         = runtime.NewScheme()
	TestingCluster = NewCluster(scheme)
)

// Cluster object defines the required clients of the test cluster.
type Cluster struct {
	Scheme        *runtime.Scheme
	KubeClient    client.Client
	DynamicClient dynamic.Interface
}

func NewCluster(scheme *runtime.Scheme) *Cluster {
	return &Cluster{
		Scheme: scheme,
	}
}

// GetClusterClient returns a Cluster client for the cluster.
func GetClusterClient(cluster *Cluster) {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kaitov1alpha1.AddToScheme(scheme))
	utilruntime.Must(kaitov1beta1.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.KarpenterSchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(azurev1alpha2.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(kaitoutils.AwsSchemeBuilder.AddToScheme(scheme))

	restConfig := config.GetConfigOrDie()

	k8sClient, err := client.New(restConfig, client.Options{Scheme: cluster.Scheme})

	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Kube Client")
	TestingCluster.KubeClient = k8sClient

	cluster.DynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Dynamic Client")

}
