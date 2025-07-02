package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/subnet"
)

const (
	testTimeout = 10 * time.Minute
	pollInterval = 10 * time.Second
)

// E2ETestSuite contains the test environment
type E2ETestSuite struct {
	kubeClient    client.Client
	ibmClient     *ibm.Client
	cloudProvider *ibmcloud.CloudProvider
	
	// Test IBM Cloud resources
	testVPC       string
	testSubnet    string
	testImage     string
	testRegion    string
	testZone      string
}

// SetupE2ETestSuite initializes the test environment
func SetupE2ETestSuite(t *testing.T) *E2ETestSuite {
	// Skip if not running E2E tests
	if os.Getenv("RUN_E2E_TESTS") != "true" {
		t.Skip("Skipping E2E tests - set RUN_E2E_TESTS=true to run")
	}

	// Verify required environment variables
	requiredEnvVars := []string{
		"IBM_API_KEY",
		"VPC_API_KEY", 
		"IBM_REGION",
		"TEST_VPC_ID",
		"TEST_SUBNET_ID",
		"TEST_IMAGE_ID",
		"TEST_ZONE",
	}

	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			t.Fatalf("Required environment variable %s is not set", envVar)
		}
	}

	// Create Kubernetes client
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	// Note: Karpenter v1 API doesn't expose AddToScheme, using standard scheme
	require.NoError(t, corev1.AddToScheme(scheme))

	kubeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// Create IBM client
	ibmClient, err := ibm.NewClient()
	require.NoError(t, err)

	// Create providers
	instanceTypeProvider, err := instancetype.NewProvider()
	require.NoError(t, err)

	instanceProvider, err := instance.NewProvider()
	require.NoError(t, err)
	instanceProvider.SetKubeClient(kubeClient)

	// Create subnet provider
	subnetProvider, err := subnet.NewProvider()
	if err != nil {
		t.Fatalf("Failed to create subnet provider: %v", err)
	}

	// Create cloud provider
	cloudProvider := ibmcloud.New(
		kubeClient,
		nil, // No event recorder needed for tests
		ibmClient,
		instanceTypeProvider,
		instanceProvider,
		subnetProvider,
	)

	return &E2ETestSuite{
		kubeClient:    kubeClient,
		ibmClient:     ibmClient,
		cloudProvider: cloudProvider,
		testVPC:       os.Getenv("TEST_VPC_ID"),
		testSubnet:    os.Getenv("TEST_SUBNET_ID"),
		testImage:     os.Getenv("TEST_IMAGE_ID"),
		testRegion:    os.Getenv("IBM_REGION"),
		testZone:      os.Getenv("TEST_ZONE"),
	}
}

func TestE2EFullWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	ctx := context.Background()
	_ = ctx

	testName := fmt.Sprintf("e2e-test-%d", time.Now().Unix())

	t.Logf("Starting E2E test: %s", testName)

	// Clean up resources at the end
	defer func() {
		t.Logf("Cleaning up E2E test resources")
		suite.cleanupTestResources(t, testName)
	}()

	// Step 1: Create NodeClass
	nodeClass := suite.createTestNodeClass(t, testName)
	t.Logf("Created NodeClass: %s", nodeClass.Name)

	// Step 2: Wait for NodeClass to be ready
	suite.waitForNodeClassReady(t, nodeClass.Name)
	t.Logf("NodeClass is ready: %s", nodeClass.Name)

	// Step 3: Create NodePool
	nodePool := suite.createTestNodePool(t, testName, nodeClass.Name)
	t.Logf("Created NodePool: %s", nodePool.Name)

	// Step 4: Create NodeClaim
	nodeClaim := suite.createTestNodeClaim(t, testName, nodeClass.Name)
	t.Logf("Created NodeClaim: %s", nodeClaim.Name)

	// Step 5: Wait for instance to be created
	suite.waitForInstanceCreation(t, nodeClaim.Name)
	t.Logf("Instance created for NodeClaim: %s", nodeClaim.Name)

	// Step 6: Verify instance exists in IBM Cloud
	suite.verifyInstanceInIBMCloud(t, nodeClaim.Name)
	t.Logf("Verified instance exists in IBM Cloud")

	// Step 7: Test instance deletion
	suite.deleteNodeClaim(t, nodeClaim.Name)
	t.Logf("Deleted NodeClaim: %s", nodeClaim.Name)

	// Step 8: Wait for instance to be deleted
	suite.waitForInstanceDeletion(t, nodeClaim.Name)
	t.Logf("Instance deleted for NodeClaim: %s", nodeClaim.Name)

	t.Logf("E2E test completed successfully: %s", testName)
}

func (s *E2ETestSuite) createTestNodeClass(t *testing.T, testName string) *v1alpha1.IBMNodeClass {
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodeclass", testName),
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          s.testRegion,
			Zone:            s.testZone,
			InstanceProfile: "bx2-2x8", // Small instance for testing
			Image:           s.testImage,
			VPC:             s.testVPC,
			Subnet:          s.testSubnet,
			Tags: map[string]string{
				"test":        "e2e",
				"test-name":   testName,
				"created-by":  "karpenter-e2e",
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodeClass)
	require.NoError(t, err)

	return nodeClass
}

func (s *E2ETestSuite) createTestNodePool(t *testing.T, testName, nodeClassName string) *karpv1.NodePool {
	nodePool := &karpv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodepool", testName),
		},
		Spec: karpv1.NodePoolSpec{
			Template: karpv1.NodeClaimTemplate{
				Spec: karpv1.NodeClaimTemplateSpec{
					NodeClassRef: &karpv1.NodeClassReference{
						Name: nodeClassName,
					},
					Requirements: []karpv1.NodeSelectorRequirementWithMinValues{
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      "node.kubernetes.io/instance-type",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"bx2-2x8"},
							},
						},
					},
				},
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodePool)
	require.NoError(t, err)

	return nodePool
}

func (s *E2ETestSuite) createTestNodeClaim(t *testing.T, testName, nodeClassName string) *karpv1.NodeClaim {
	nodeClaim := &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodeclaim", testName),
		},
		Spec: karpv1.NodeClaimSpec{
			NodeClassRef: &karpv1.NodeClassReference{
				Name: nodeClassName,
			},
			Requirements: []karpv1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: corev1.NodeSelectorRequirement{
						Key:      "node.kubernetes.io/instance-type",
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"bx2-2x8"},
					},
				},
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodeClaim)
	require.NoError(t, err)

	return nodeClaim
}

func (s *E2ETestSuite) waitForNodeClassReady(t *testing.T, nodeClassName string) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, pollInterval, testTimeout, true, func(ctx context.Context) (bool, error) {
		var nodeClass v1alpha1.IBMNodeClass
		err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClassName}, &nodeClass)
		if err != nil {
			return false, err
		}

		// Check if Ready condition is True
		for _, condition := range nodeClass.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})

	require.NoError(t, err, "NodeClass should become ready within timeout")
}

func (s *E2ETestSuite) waitForInstanceCreation(t *testing.T, nodeClaimName string) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, pollInterval, testTimeout, true, func(ctx context.Context) (bool, error) {
		var nodeClaim karpv1.NodeClaim
		err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
		if err != nil {
			return false, err
		}

		// Check if ProviderID is set (indicates instance was created)
		return nodeClaim.Status.ProviderID != "", nil
	})

	require.NoError(t, err, "NodeClaim should get ProviderID within timeout")
}

func (s *E2ETestSuite) verifyInstanceInIBMCloud(t *testing.T, nodeClaimName string) {
	ctx := context.Background()

	// Get the NodeClaim to extract ProviderID
	var nodeClaim karpv1.NodeClaim
	err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
	require.NoError(t, err)
	require.NotEmpty(t, nodeClaim.Status.ProviderID)

	// Verify instance exists via cloud provider
	retrievedNodeClaim, err := s.cloudProvider.Get(ctx, nodeClaim.Status.ProviderID)
	require.NoError(t, err)
	assert.Equal(t, nodeClaim.Status.ProviderID, retrievedNodeClaim.Status.ProviderID)
}

func (s *E2ETestSuite) deleteNodeClaim(t *testing.T, nodeClaimName string) {
	ctx := context.Background()

	var nodeClaim karpv1.NodeClaim
	err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
	require.NoError(t, err)

	err = s.kubeClient.Delete(ctx, &nodeClaim)
	require.NoError(t, err)
}

func (s *E2ETestSuite) waitForInstanceDeletion(t *testing.T, nodeClaimName string) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, pollInterval, testTimeout, true, func(ctx context.Context) (bool, error) {
		var nodeClaim karpv1.NodeClaim
		err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
		if errors.IsNotFound(err) {
			return true, nil // NodeClaim was deleted
		}
		if err != nil {
			return false, err
		}

		return false, nil // Still exists
	})

	require.NoError(t, err, "NodeClaim should be deleted within timeout")
}

func (s *E2ETestSuite) cleanupTestResources(t *testing.T, testName string) {
	ctx := context.Background()

	// List of resources to clean up
	resources := []struct {
		name     string
		obj      client.Object
		listObj  client.ObjectList
	}{
		{
			name:    "NodeClaim",
			obj:     &karpv1.NodeClaim{},
			listObj: &karpv1.NodeClaimList{},
		},
		{
			name:    "NodePool", 
			obj:     &karpv1.NodePool{},
			listObj: &karpv1.NodePoolList{},
		},
		{
			name:    "IBMNodeClass",
			obj:     &v1alpha1.IBMNodeClass{},
			listObj: &v1alpha1.IBMNodeClassList{},
		},
	}

	for _, resource := range resources {
		err := s.kubeClient.List(ctx, resource.listObj)
		if err != nil {
			t.Logf("Failed to list %s for cleanup: %v", resource.name, err)
			continue
		}

		// Use reflection to get items from the list
		items := resource.listObj.(interface {
			GetItems() []client.Object
		}).GetItems()

		for _, item := range items {
			// Check if this resource belongs to our test
			if labels := item.GetLabels(); labels != nil {
				if labels["test-name"] == testName || labels["test"] == "e2e" {
					err := s.kubeClient.Delete(ctx, item)
					if err != nil && !errors.IsNotFound(err) {
						t.Logf("Failed to delete %s %s: %v", resource.name, item.GetName(), err)
					} else {
						t.Logf("Cleaned up %s: %s", resource.name, item.GetName())
					}
				}
			}

			// Also check by name prefix
			if len(item.GetName()) > len(testName) && item.GetName()[:len(testName)] == testName {
				err := s.kubeClient.Delete(ctx, item)
				if err != nil && !errors.IsNotFound(err) {
					t.Logf("Failed to delete %s %s: %v", resource.name, item.GetName(), err)
				} else {
					t.Logf("Cleaned up %s: %s", resource.name, item.GetName())
				}
			}
		}
	}
}

// TestE2ENodeClassValidation tests NodeClass validation
func TestE2ENodeClassValidation(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	ctx := context.Background()

	testName := fmt.Sprintf("validation-test-%d", time.Now().Unix())
	defer suite.cleanupTestResources(t, testName)

	// Test invalid NodeClass (missing required fields)
	invalidNodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-invalid", testName),
			Labels: map[string]string{
				"test-name": testName,
			},
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: "us-south",
			// Missing VPC and Image
		},
	}

	err := suite.kubeClient.Create(ctx, invalidNodeClass)
	require.NoError(t, err)

	// Wait a bit for controllers to process
	time.Sleep(5 * time.Second)

	// Check that NodeClass has validation errors
	var updatedNodeClass v1alpha1.IBMNodeClass
	err = suite.kubeClient.Get(ctx, types.NamespacedName{Name: invalidNodeClass.Name}, &updatedNodeClass)
	require.NoError(t, err)

	// Should have conditions indicating validation failure
	found := false
	for _, condition := range updatedNodeClass.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse {
			found = true
			break
		}
	}
	assert.True(t, found, "Invalid NodeClass should have Ready=False condition")
}

// TestE2EInstanceTypeSelection tests automatic instance type selection
func TestE2EInstanceTypeSelection(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	
	testName := fmt.Sprintf("instancetype-test-%d", time.Now().Unix())
	defer suite.cleanupTestResources(t, testName)

	// Test NodeClass with instance requirements instead of specific profile
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-autotype", testName),
			Labels: map[string]string{
				"test-name": testName,
			},
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: suite.testRegion,
			Zone:   suite.testZone,
			Image:  suite.testImage,
			VPC:    suite.testVPC,
			Subnet: suite.testSubnet,
			InstanceRequirements: &v1alpha1.InstanceTypeRequirements{
				MinimumCPU:    2,
				MinimumMemory: 8,
				Architecture:  "amd64",
			},
		},
	}

	err := suite.kubeClient.Create(context.Background(), nodeClass)
	require.NoError(t, err)

	// Wait for NodeClass to be processed and instance types selected
	suite.waitForNodeClassReady(t, nodeClass.Name)

	// Verify that instance types were selected
	var updatedNodeClass v1alpha1.IBMNodeClass
	err = suite.kubeClient.Get(context.Background(), types.NamespacedName{Name: nodeClass.Name}, &updatedNodeClass)
	require.NoError(t, err)

	assert.NotEmpty(t, updatedNodeClass.Status.SelectedInstanceTypes, "Instance types should be auto-selected")
	t.Logf("Auto-selected instance types: %v", updatedNodeClass.Status.SelectedInstanceTypes)
}

// BenchmarkE2EInstanceCreation benchmarks instance creation performance
func BenchmarkE2EInstanceCreation(b *testing.B) {
	if os.Getenv("RUN_E2E_BENCHMARKS") != "true" {
		b.Skip("Skipping E2E benchmarks - set RUN_E2E_BENCHMARKS=true to run")
	}

	suite := SetupE2ETestSuite(&testing.T{})
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			testName := fmt.Sprintf("bench-%d-%d", time.Now().UnixNano(), b.N)
			
			// Create NodeClass
			nodeClass := suite.createTestNodeClass(&testing.T{}, testName)
			
			// Create NodeClaim
			nodeClaim := suite.createTestNodeClaim(&testing.T{}, testName, nodeClass.Name)
			
			// Wait for creation (this is what we're benchmarking)
			suite.waitForInstanceCreation(&testing.T{}, nodeClaim.Name)
			
			// Clean up
			suite.deleteNodeClaim(&testing.T{}, nodeClaim.Name)
			suite.waitForInstanceDeletion(&testing.T{}, nodeClaim.Name)
			suite.cleanupTestResources(&testing.T{}, testName)
		}
	})
}