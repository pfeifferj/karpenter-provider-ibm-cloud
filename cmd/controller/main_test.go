package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpcp "sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmcp "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
)

type MockInstanceTypeProvider struct {
	mock.Mock
}

func (m *MockInstanceTypeProvider) Get(ctx context.Context, name string) (*karpcp.InstanceType, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*karpcp.InstanceType), args.Error(1)
}

func (m *MockInstanceTypeProvider) List(ctx context.Context) ([]*karpcp.InstanceType, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*karpcp.InstanceType), args.Error(1)
}

func (m *MockInstanceTypeProvider) Create(ctx context.Context, instanceType *karpcp.InstanceType) error {
	args := m.Called(ctx, instanceType)
	return args.Error(0)
}

func (m *MockInstanceTypeProvider) Delete(ctx context.Context, instanceType *karpcp.InstanceType) error {
	args := m.Called(ctx, instanceType)
	return args.Error(0)
}

type MockInstanceProvider struct {
	mock.Mock
}

func (m *MockInstanceProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*corev1.Node, error) {
	args := m.Called(ctx, nodeClaim)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*corev1.Node), args.Error(1)
}

func (m *MockInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	args := m.Called(ctx, node)
	return args.Error(0)
}

func (m *MockInstanceProvider) GetInstance(ctx context.Context, node *corev1.Node) (*instance.Instance, error) {
	args := m.Called(ctx, node)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*instance.Instance), args.Error(1)
}

func (m *MockInstanceProvider) TagInstance(ctx context.Context, instanceID string, tags map[string]string) error {
	args := m.Called(ctx, instanceID, tags)
	return args.Error(0)
}

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add core scheme: %v", err)
	}

	// Register the NodeClaim type
	scheme.AddKnownTypes(metav1.SchemeGroupVersion, &karpv1.NodeClaim{}, &karpv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)

	// Register the IBMNodeClass type
	scheme.AddKnownTypes(metav1.SchemeGroupVersion, &v1alpha1.IBMNodeClass{})
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)

	t.Run("should delete node claim", func(t *testing.T) {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
			},
			Spec: corev1.NodeSpec{
				ProviderID: "ibm://test-instance-id",
			},
		}

		now := metav1.Now()
		nodeClaim := &karpv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-node",
				Namespace:         "default",
				DeletionTimestamp: &now,
				Finalizers:        []string{"karpenter.sh/finalizer"},
			},
			Status: karpv1.NodeClaimStatus{
				ProviderID: "ibm://test-instance-id",
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(nodeClaim, node).
			Build()

		mockInstanceTypeProvider := &MockInstanceTypeProvider{}
		mockInstanceProvider := &MockInstanceProvider{}
		mockCloudProvider := ibmcp.New(fakeClient, events.NewRecorder(nil), mockInstanceTypeProvider, mockInstanceProvider)

		mockInstanceProvider.On("Delete", mock.Anything, mock.AnythingOfType("*v1.Node")).Return(nil).Once()

		reconciler := NewIBMCloudReconciler(fakeClient, mockCloudProvider)

		_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-node",
				Namespace: "default",
			},
		})

		assert.NoError(t, err)
		mockInstanceProvider.AssertExpectations(t)

		var updatedNodeClaim karpv1.NodeClaim
		err = fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      "test-node",
			Namespace: "default",
		}, &updatedNodeClaim)
		assert.Error(t, err)
	})

	t.Run("should handle not found node claim", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		mockInstanceTypeProvider := &MockInstanceTypeProvider{}
		mockInstanceProvider := &MockInstanceProvider{}
		mockCloudProvider := ibmcp.New(fakeClient, events.NewRecorder(nil), mockInstanceTypeProvider, mockInstanceProvider)

		reconciler := NewIBMCloudReconciler(fakeClient, mockCloudProvider)

		_, err := reconciler.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "non-existent",
				Namespace: "default",
			},
		})

		assert.NoError(t, err)
	})
}
