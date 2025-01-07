// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	awsv1beta1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1beta1"
	kaitov1alpha1 "github.com/kaito-project/kaito/api/v1alpha1"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kubernetes/test/e2e/framework"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
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
	utilruntime.Must(v1beta1.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(azurev1alpha2.SchemeBuilder.AddToScheme(scheme))
	utilruntime.Must(awsv1beta1.SchemeBuilder.AddToScheme(scheme))

	restConfig := config.GetConfigOrDie()

	k8sClient, err := client.New(restConfig, client.Options{Scheme: cluster.Scheme})
	framework.ExpectNoError(err, "failed to create k8s client for e2e")

	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Kube Client")
	TestingCluster.KubeClient = k8sClient

	cluster.DynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Dynamic Client")

}
