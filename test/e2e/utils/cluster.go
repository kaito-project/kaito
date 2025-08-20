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

package utils

import (
	azurev1alpha2 "github.com/Azure/karpenter-provider-azure/pkg/apis/v1alpha2"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
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
	utilruntime.Must(helmv2.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))

	restConfig := config.GetConfigOrDie()

	k8sClient, err := client.New(restConfig, client.Options{Scheme: cluster.Scheme})

	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Kube Client")
	TestingCluster.KubeClient = k8sClient

	cluster.DynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to set up Dynamic Client")
}
