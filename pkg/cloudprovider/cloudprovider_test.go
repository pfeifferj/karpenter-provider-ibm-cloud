package cloudprovider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
	"sigs.k8s.io/karpenter/pkg/scheduling"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
)

// Mock Event Recorder
type mockEventRecorder struct {
	record.EventRecorder
	events []events.Event
}

func (m *mockEventRecorder) Publish(e ...events.Event) {
	m.events = append(m.events, e...)
}


// Mock Instance Type Provider
type mockInstanceTypeProvider struct {
	instanceTypes []*cloudprovider.InstanceType
	getError      error
	listError     error
}

func (m *mockInstanceTypeProvider) Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	for _, it := range m.instanceTypes {
		if it.Name == name {
			return it, nil
		}
	}
	return nil, fmt.Errorf("instance type %s not found", name)
}

func (m *mockInstanceTypeProvider) List(ctx context.Context) ([]*cloudprovider.InstanceType, error) {
	if m.listError != nil {
		return nil, m.listError
	}
	return m.instanceTypes, nil
}

func (m *mockInstanceTypeProvider) Create(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	return nil
}

func (m *mockInstanceTypeProvider) Delete(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	return nil
}

func (m *mockInstanceTypeProvider) FilterInstanceTypes(ctx context.Context, requirements *v1alpha1.InstanceTypeRequirements) ([]*cloudprovider.InstanceType, error) {
	return m.instanceTypes, nil
}

func (m *mockInstanceTypeProvider) RankInstanceTypes(instanceTypes []*cloudprovider.InstanceType) []*cloudprovider.InstanceType {
	return instanceTypes
}

// Mock Instance Provider
type mockInstanceProvider struct {
	createNode    *corev1.Node
	createError   error
	deleteError   error
	getInstance   *instance.Instance
	getError      error
	tagError      error
}

func (m *mockInstanceProvider) SetKubeClient(client client.Client) {}

func (m *mockInstanceProvider) Create(ctx context.Context, nodeClaim *karpv1.NodeClaim) (*corev1.Node, error) {
	if m.createError != nil {
		return nil, m.createError
	}
	if m.createNode != nil {
		return m.createNode, nil
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeClaim.Name,
			Labels: map[string]string{
				"node.kubernetes.io/instance-type": "test-instance-type",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: "ibm://test-instance-id",
		},
	}, nil
}

func (m *mockInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	return m.deleteError
}

func (m *mockInstanceProvider) GetInstance(ctx context.Context, node *corev1.Node) (*instance.Instance, error) {
	if m.getError != nil {
		return nil, m.getError
	}
	if m.getInstance != nil {
		return m.getInstance, nil
	}
	return &instance.Instance{
		ID:           "test-instance-id",
		Type:         "test-instance-type",
		Zone:         "us-south-1",
		Region:       "us-south",
		CapacityType: "on-demand",
		Status:       instance.InstanceStatusRunning,
	}, nil
}

func (m *mockInstanceProvider) TagInstance(ctx context.Context, instanceID string, tags map[string]string) error {
	return m.tagError
}

// Test helpers
func getTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	
	// Register Karpenter v1 types manually
	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv,
		&karpv1.NodeClaim{},
		&karpv1.NodeClaimList{},
		&karpv1.NodePool{},
		&karpv1.NodePoolList{},
	)
	metav1.AddToGroupVersion(s, gv)
	
	return s
}

func getTestNodeClass() *v1alpha1.IBMNodeClass {
	return &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          "us-south",
			Zone:            "us-south-1",
			InstanceProfile: "bx2-4x16",
			Image:           "test-image",
			VPC:             "test-vpc",
			Subnet:          "test-subnet",
		},
		Status: v1alpha1.IBMNodeClassStatus{
			SpecHash: 12345,
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "Ready",
					Message:            "NodeClass is ready",
				},
			},
		},
	}
}

func getTestNodeClaim(nodeClassName string) *karpv1.NodeClaim {
	return &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nodeclaim",
			Namespace: "default",
		},
		Spec: karpv1.NodeClaimSpec{
			NodeClassRef: &karpv1.NodeClassReference{
				Name: nodeClassName,
			},
			Requirements: []karpv1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: corev1.NodeSelectorRequirement{
						Key:      corev1.LabelInstanceTypeStable,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"test-instance-type"},
					},
				},
			},
			Resources: karpv1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
	}
}

func getTestInstanceType() *cloudprovider.InstanceType {
	return &cloudprovider.InstanceType{
		Name: "test-instance-type",
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
			corev1.ResourcePods:   resource.MustParse("110"),
		},
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, "test-instance-type"),
			scheduling.NewRequirement(karpv1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, karpv1.CapacityTypeOnDemand),
			scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, "us-south-1"),
		),
		Offerings: cloudprovider.Offerings{
			{
				Requirements: scheduling.NewRequirements(
					scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, "us-south-1"),
					scheduling.NewRequirement(karpv1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, karpv1.CapacityTypeOnDemand),
				),
				Price:     1.0,
				Available: true,
			},
		},
		Overhead: &cloudprovider.InstanceTypeOverhead{
			KubeReserved: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}
}

func TestCloudProvider_Create(t *testing.T) {
	tests := []struct {
		name             string
		nodeClaim        *karpv1.NodeClaim
		nodeClass        *v1alpha1.IBMNodeClass
		instanceProvider *mockInstanceProvider
		instanceTypes    []*cloudprovider.InstanceType
		expectError      bool
		errorContains    string
	}{
		{
			name:      "successful node creation",
			nodeClaim: getTestNodeClaim("test-nodeclass"),
			nodeClass: getTestNodeClass(),
			instanceProvider: &mockInstanceProvider{
				createNode: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"node.kubernetes.io/instance-type": "test-instance-type",
							corev1.LabelTopologyZone:           "us-south-1",
						},
					},
					Spec: corev1.NodeSpec{
						ProviderID: "ibm://test-instance-id",
					},
					Status: corev1.NodeStatus{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				},
			},
			instanceTypes: []*cloudprovider.InstanceType{getTestInstanceType()},
			expectError:   false,
		},
		{
			name:      "nodeclass not found",
			nodeClaim: getTestNodeClaim("non-existent"),
			nodeClass: nil,
			instanceProvider: &mockInstanceProvider{},
			expectError:   true,
			errorContains: "not found",
		},
		{
			name:      "instance creation failure",
			nodeClaim: getTestNodeClaim("test-nodeclass"),
			nodeClass: getTestNodeClass(),
			instanceProvider: &mockInstanceProvider{
				createError: fmt.Errorf("failed to create instance"),
			},
			instanceTypes: []*cloudprovider.InstanceType{getTestInstanceType()},
			expectError:   true,
			errorContains: "failed to create instance",
		},
		{
			name:      "no matching instance types",
			nodeClaim: getTestNodeClaim("test-nodeclass"),
			nodeClass: getTestNodeClass(),
			instanceProvider: &mockInstanceProvider{},
			instanceTypes: []*cloudprovider.InstanceType{},
			expectError:   true,
			errorContains: "all requested instance types were unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()
			
			// Build fake client with objects
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.nodeClass != nil {
				builder = builder.WithObjects(tt.nodeClass)
			}
			fakeClient := builder.Build()

			// Create CloudProvider
			cp := &CloudProvider{
				kubeClient:           fakeClient,
				recorder:             &mockEventRecorder{},
				ibmClient:            nil, // We don't use IBM client in tests
				instanceTypeProvider: &mockInstanceTypeProvider{instanceTypes: tt.instanceTypes},
				instanceProvider:     tt.instanceProvider,
			}

			// Test Create
			result, err := cp.Create(ctx, tt.nodeClaim)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.NotEmpty(t, result.Status.ProviderID)
				assert.Equal(t, "ibm://test-instance-id", result.Status.ProviderID)
			}
		})
	}
}

func TestCloudProvider_Delete(t *testing.T) {
	tests := []struct {
		name             string
		nodeClaim        *karpv1.NodeClaim
		node             *corev1.Node
		instanceProvider *mockInstanceProvider
		expectError      bool
		errorContains    string
	}{
		{
			name: "successful node deletion",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
				},
				Status: karpv1.NodeClaimStatus{
					ProviderID: "ibm://test-instance-id",
					NodeName:   "test-node",
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			instanceProvider: &mockInstanceProvider{},
			expectError:      false,
		},
		{
			name: "node not found - should succeed",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
				},
				Status: karpv1.NodeClaimStatus{
					NodeName: "non-existent-node",
				},
			},
			instanceProvider: &mockInstanceProvider{},
			expectError:      false,
		},
		{
			name: "instance deletion failure",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
				},
				Status: karpv1.NodeClaimStatus{
					ProviderID: "ibm://test-instance-id",
					NodeName:   "test-node",
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			instanceProvider: &mockInstanceProvider{
				deleteError: fmt.Errorf("failed to delete instance"),
			},
			expectError:   true,
			errorContains: "failed to delete instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()
			
			// Build fake client with objects
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.node != nil {
				builder = builder.WithObjects(tt.node)
			}
			fakeClient := builder.Build()

			// Create CloudProvider
			cp := &CloudProvider{
				kubeClient:       fakeClient,
				recorder:         &mockEventRecorder{},
				instanceProvider: tt.instanceProvider,
			}

			// Test Delete
			err := cp.Delete(ctx, tt.nodeClaim)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCloudProvider_GetInstanceTypes(t *testing.T) {
	tests := []struct {
		name          string
		nodePool      *karpv1.NodePool
		instanceTypes []*cloudprovider.InstanceType
		listError     error
		expectError   bool
		expectedCount int
	}{
		{
			name: "successful get instance types",
			nodePool: &karpv1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
				Spec: karpv1.NodePoolSpec{
					Template: karpv1.NodeClaimTemplate{
						Spec: karpv1.NodeClaimTemplateSpec{
							NodeClassRef: &karpv1.NodeClassReference{
								Name: "test-nodeclass",
							},
						},
					},
				},
			},
			instanceTypes: []*cloudprovider.InstanceType{
				getTestInstanceType(),
				{
					Name: "test-instance-type-2",
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("32Gi"),
					},
					Requirements: scheduling.NewRequirements(),
					Offerings:    cloudprovider.Offerings{},
				},
			},
			expectError:   false,
			expectedCount: 2,
		},
		{
			name: "provider error",
			nodePool: &karpv1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
				Spec: karpv1.NodePoolSpec{
					Template: karpv1.NodeClaimTemplate{
						Spec: karpv1.NodeClaimTemplateSpec{
							NodeClassRef: &karpv1.NodeClassReference{
								Name: "test-nodeclass",
							},
						},
					},
				},
			},
			listError:   fmt.Errorf("failed to list instance types"),
			expectError: true,
		},
		{
			name: "empty instance types",
			nodePool: &karpv1.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodepool",
				},
				Spec: karpv1.NodePoolSpec{
					Template: karpv1.NodeClaimTemplate{
						Spec: karpv1.NodeClaimTemplateSpec{
							NodeClassRef: &karpv1.NodeClassReference{
								Name: "test-nodeclass",
							},
						},
					},
				},
			},
			instanceTypes: []*cloudprovider.InstanceType{},
			expectError:   false,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()
			
			// Create fake client with NodeClass
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(getTestNodeClass()).
				Build()

			// Create CloudProvider
			cp := &CloudProvider{
				kubeClient: fakeClient,
				instanceTypeProvider: &mockInstanceTypeProvider{
					instanceTypes: tt.instanceTypes,
					listError:     tt.listError,
				},
			}

			// Test GetInstanceTypes
			result, err := cp.GetInstanceTypes(ctx, tt.nodePool)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, result, tt.expectedCount)
			}
		})
	}
}

func TestCloudProvider_IsDrifted(t *testing.T) {
	tests := []struct {
		name          string
		nodeClaim     *karpv1.NodeClaim
		expectedDrift cloudprovider.DriftReason
		expectError   bool
	}{
		{
			name: "no drift",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
					Annotations: map[string]string{
						v1alpha1.AnnotationIBMNodeClassHashVersion: v1alpha1.IBMNodeClassHashVersion,
						v1alpha1.AnnotationIBMNodeClassHash:        "12345", // Match the hash in getTestNodeClass
					},
				},
				Spec: karpv1.NodeClaimSpec{
					NodeClassRef: &karpv1.NodeClassReference{
						Name: "test-nodeclass",
					},
				},
			},
			expectedDrift: "",
			expectError:   false,
		},
		{
			name: "nodeclaim with hash drift",
			nodeClaim: &karpv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
					Annotations: map[string]string{
						v1alpha1.AnnotationIBMNodeClassHashVersion: v1alpha1.IBMNodeClassHashVersion,
						v1alpha1.AnnotationIBMNodeClassHash:        "54321", // Different hash to trigger drift
					},
				},
				Spec: karpv1.NodeClaimSpec{
					NodeClassRef: &karpv1.NodeClassReference{
						Name: "test-nodeclass",
					},
				},
			},
			expectedDrift: "NodeClassHashChanged",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()
			
			// Create fake client with NodeClass
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(getTestNodeClass()).
				Build()

			// Create CloudProvider
			cp := &CloudProvider{
				kubeClient: fakeClient,
			}

			// Test IsDrifted
			drift, err := cp.IsDrifted(ctx, tt.nodeClaim)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDrift, drift)
			}
		})
	}
}

func TestCloudProvider_Get(t *testing.T) {
	tests := []struct {
		name         string
		providerID   string
		node         *corev1.Node
		getInstance  *instance.Instance
		getError     error
		expectError  bool
		expectedName string
	}{
		{
			name:       "successful get",
			providerID: "ibm://test-instance-id",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			getInstance: &instance.Instance{
				ID:     "test-instance-id",
				Status: instance.InstanceStatusRunning,
			},
			expectError:  false,
			expectedName: "test-node",
		},
		{
			name:       "node not found",
			providerID: "ibm://non-existent",
			getError:   fmt.Errorf("instance not found"),
			expectError: true,
		},
		{
			name:       "instance provider error",
			providerID: "ibm://test-instance-id",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "ibm://test-instance-id",
				},
			},
			getError:    fmt.Errorf("failed to get instance"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()
			
			// Build fake client with objects
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.node != nil {
				builder = builder.WithObjects(tt.node)
			}
			fakeClient := builder.Build()

			// Create CloudProvider
			cp := &CloudProvider{
				kubeClient: fakeClient,
				instanceProvider: &mockInstanceProvider{
					getInstance: tt.getInstance,
					getError:    tt.getError,
				},
				instanceTypeProvider: &mockInstanceTypeProvider{
					instanceTypes: []*cloudprovider.InstanceType{getTestInstanceType()},
				},
			}

			// Test Get
			result, err := cp.Get(ctx, tt.providerID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.providerID, result.Status.ProviderID)
			}
		})
	}
}

func TestCloudProvider_RepairPolicies(t *testing.T) {
	cp := &CloudProvider{}
	
	policies := cp.RepairPolicies()
	
	// Should have at least one repair policy
	assert.NotEmpty(t, policies)
	
	// Check first policy
	assert.Equal(t, corev1.NodeReady, policies[0].ConditionType)
	assert.Equal(t, corev1.ConditionFalse, policies[0].ConditionStatus)
	assert.Equal(t, 5*time.Minute, policies[0].TolerationDuration)
}

func TestCloudProvider_Name(t *testing.T) {
	cp := &CloudProvider{}
	assert.Equal(t, "ibmcloud", cp.Name())
}

func TestCloudProvider_GetSupportedNodeClasses(t *testing.T) {
	cp := &CloudProvider{}
	
	nodeClasses := cp.GetSupportedNodeClasses()
	
	// Should return IBMNodeClass
	assert.Len(t, nodeClasses, 1)
	_, ok := nodeClasses[0].(*v1alpha1.IBMNodeClass)
	assert.True(t, ok)
}

func TestCloudProvider_List(t *testing.T) {
	tests := []struct {
		name        string
		nodes       []runtime.Object
		expectError bool
		expected    int
	}{
		{
			name: "list nodes with karpenter label",
			nodes: []runtime.Object{
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"karpenter.sh/managed-by": "test",
						},
					},
					Spec: corev1.NodeSpec{
						ProviderID: "ibm://instance-1",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"karpenter.sh/managed-by": "test",
						},
					},
					Spec: corev1.NodeSpec{
						ProviderID: "ibm://instance-2",
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						// No karpenter label
					},
				},
			},
			expectError: false,
			expected:    2,
		},
		{
			name:        "no nodes",
			nodes:       []runtime.Object{},
			expectError: false,
			expected:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := getTestScheme()
			
			// Build fake client with nodes
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.nodes...).
				Build()

			// Create CloudProvider
			cp := &CloudProvider{
				kubeClient:       fakeClient,
				instanceProvider: &mockInstanceProvider{},
			}

			// Test List
			result, err := cp.List(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, result, tt.expected)
			}
		})
	}
}