// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package test

import (
	"context"
	"reflect"

	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// MockClient Client is a mock for the controller-runtime dynamic client interface.
type MockClient struct {
	mock.Mock

	ObjectMap  map[reflect.Type]map[k8sClient.ObjectKey]k8sClient.Object
	StatusMock *MockStatusClient
	UpdateCb   func(key types.NamespacedName)
}

var _ k8sClient.Client = &MockClient{}

func NewClient() *MockClient {
	return &MockClient{
		StatusMock: &MockStatusClient{},
		ObjectMap:  map[reflect.Type]map[k8sClient.ObjectKey]k8sClient.Object{},
	}
}

// Retrieves or creates a map associated with the type of obj
func (m *MockClient) ensureMapForType(t reflect.Type) map[k8sClient.ObjectKey]k8sClient.Object {
	if _, ok := m.ObjectMap[t]; !ok {
		//create a new map with the object key if it doesn't exist
		m.ObjectMap[t] = map[k8sClient.ObjectKey]k8sClient.Object{}
	}
	return m.ObjectMap[t]
}

func (m *MockClient) CreateMapWithType(t interface{}) map[k8sClient.ObjectKey]k8sClient.Object {
	objType := reflect.TypeOf(t)

	return m.ensureMapForType(objType)
}

func (m *MockClient) CreateOrUpdateObjectInMap(obj k8sClient.Object) {
	t := reflect.TypeOf(obj)
	relevantMap := m.ensureMapForType(t)
	objKey := k8sClient.ObjectKeyFromObject(obj)

	relevantMap[objKey] = obj
}

func (m *MockClient) GetObjectFromMap(obj k8sClient.Object, key types.NamespacedName) {
	t := reflect.TypeOf(obj)
	relevantMap := m.ensureMapForType(t)

	if val, ok := relevantMap[key]; ok {
		v := reflect.ValueOf(obj).Elem()
		v.Set(reflect.ValueOf(val).Elem())
	}
}

// Get k8s Client interface
func (m *MockClient) Get(ctx context.Context, key types.NamespacedName, obj k8sClient.Object, opts ...k8sClient.GetOption) error {
	//make any necessary changes to the object
	if m.UpdateCb != nil {
		m.UpdateCb(key)
	}

	m.GetObjectFromMap(obj, key)

	args := m.Called(ctx, key, obj, opts)
	return args.Error(0)
}

func (m *MockClient) List(ctx context.Context, list k8sClient.ObjectList, opts ...k8sClient.ListOption) error {
	v := reflect.ValueOf(list).Elem()
	newList := m.getObjectListFromMap(list)
	v.Set(reflect.ValueOf(newList).Elem())

	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *MockClient) getObjectListFromMap(list k8sClient.ObjectList) k8sClient.ObjectList {
	objType := reflect.TypeOf(list)
	relevantMap := m.ensureMapForType(objType)

	switch list.(type) {
	case *corev1.NodeList:
		nodeList := &corev1.NodeList{}
		for _, obj := range relevantMap {
			if node, ok := obj.(*corev1.Node); ok {
				nodeList.Items = append(nodeList.Items, *node)
			}
		}
		return nodeList
	case *karpenterv1.NodeClaimList:
		nodeClaimList := &karpenterv1.NodeClaimList{}
		for _, obj := range relevantMap {
			if m, ok := obj.(*karpenterv1.NodeClaim); ok {
				nodeClaimList.Items = append(nodeClaimList.Items, *m)
			}
		}
		return nodeClaimList
	case *appsv1.ControllerRevisionList:
		controllerRevisionList := &appsv1.ControllerRevisionList{}
		for _, obj := range relevantMap {
			if m, ok := obj.(*appsv1.ControllerRevision); ok {
				controllerRevisionList.Items = append(controllerRevisionList.Items, *m)
			}
		}
		return controllerRevisionList
	}
	//add additional object lists as needed
	return nil
}

func (m *MockClient) Create(ctx context.Context, obj k8sClient.Object, opts ...k8sClient.CreateOption) error {
	m.CreateOrUpdateObjectInMap(obj)

	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Delete(ctx context.Context, obj k8sClient.Object, opts ...k8sClient.DeleteOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Update(ctx context.Context, obj k8sClient.Object, opts ...k8sClient.UpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockClient) Patch(ctx context.Context, obj k8sClient.Object, patch k8sClient.Patch, opts ...k8sClient.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

func (m *MockClient) DeleteAllOf(ctx context.Context, obj k8sClient.Object, opts ...k8sClient.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

// SubResource implements client.Client
func (m *MockClient) SubResource(subResource string) k8sClient.SubResourceClient {
	panic("unimplemented")
}

// GroupVersionKindFor implements client.Client
func (m *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	panic("unimplemented")
}

// IsObjectNamespaced implements client.Client
func (m *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	panic("unimplemented")
}

func (m *MockClient) Scheme() *runtime.Scheme {
	args := m.Called()
	return args.Get(0).(*runtime.Scheme)
}

func (m *MockClient) RESTMapper() meta.RESTMapper {
	args := m.Called()
	return args.Get(0).(meta.RESTMapper)
}

// StatusClient interface

func (m *MockClient) Status() k8sClient.StatusWriter {
	return m.StatusMock
}

type MockStatusClient struct {
	mock.Mock
}

func (m *MockStatusClient) Create(ctx context.Context, obj k8sClient.Object, subResource k8sClient.Object, opts ...k8sClient.SubResourceCreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockStatusClient) Patch(ctx context.Context, obj k8sClient.Object, patch k8sClient.Patch, opts ...k8sClient.SubResourcePatchOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockStatusClient) Update(ctx context.Context, obj k8sClient.Object, opts ...k8sClient.SubResourceUpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

var _ k8sClient.StatusWriter = &MockStatusClient{}
