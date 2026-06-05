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

package karpenter

import (
	awsapis "github.com/aws/karpenter-provider-aws/pkg/apis"
	awsv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	karpenterapis "sigs.k8s.io/karpenter/pkg/apis"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var (
	karpenterSchemeGroupVersion = schema.GroupVersion{Group: karpenterapis.Group, Version: "v1"}
	awsSchemeGroupVersion       = schema.GroupVersion{Group: awsapis.Group, Version: "v1"}

	KarpenterSchemeBuilder = runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(karpenterSchemeGroupVersion,
			&karpenterv1.NodePool{},
			&karpenterv1.NodePoolList{},
			&karpenterv1.NodeClaim{},
			&karpenterv1.NodeClaimList{},
		)
		metav1.AddToGroupVersion(scheme, karpenterSchemeGroupVersion)
		return nil
	})
	AwsSchemeBuilder = runtime.NewSchemeBuilder(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(awsSchemeGroupVersion,
			&awsv1.EC2NodeClass{},
			&awsv1.EC2NodeClassList{},
		)
		metav1.AddToGroupVersion(scheme, awsSchemeGroupVersion)
		return nil
	})
)
