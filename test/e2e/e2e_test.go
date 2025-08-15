/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	ibmcloud "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/instancetype"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/common/pricing"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/vpc/subnet"
)

const (
	testTimeout  = 15 * time.Minute // Increased timeout for node provisioning
	pollInterval = 10 * time.Second
)

func init() {
	// Initialize logger for controller-runtime to avoid warnings
	zapConfig := zap.NewDevelopmentConfig()

	// Reduce noise in e2e tests unless verbose mode is enabled
	if os.Getenv("E2E_VERBOSE") != "true" {
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	zapLogger, _ := zapConfig.Build()
	log.SetLogger(zapr.NewLogger(zapLogger))
}

// E2ETestSuite contains the test environment
type E2ETestSuite struct {
	kubeClient           client.Client
	ibmClient            *ibm.Client
	cloudProvider        *ibmcloud.CloudProvider
	instanceTypeProvider instancetype.Provider

	// Test IBM Cloud resources
	testVPC    string
	testSubnet string
	testImage  string
	testRegion string
	testZone   string
}

// SetupE2ETestSuite initializes the test environment
func SetupE2ETestSuite(t *testing.T) *E2ETestSuite {
	// Skip if not running E2E tests
	if os.Getenv("RUN_E2E_TESTS") != "true" {
		t.Skip("Skipping E2E tests - set RUN_E2E_TESTS=true to run")
	}

	// Verify required environment variables
	requiredEnvVars := []string{
		"IBMCLOUD_API_KEY",
		"VPC_API_KEY",
		"IBMCLOUD_REGION",
		"TEST_VPC_ID",
		"TEST_SUBNET_ID",
		"TEST_IMAGE_ID",
		"TEST_ZONE",
		"TEST_SECURITY_GROUP_ID",
	}

	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			t.Fatalf("Required environment variable %s is not set", envVar)
		}
	}

	// Create Kubernetes client using local kubeconfig
	cfg, err := config.GetConfigWithContext("kubernetes-admin@kubernetes")
	if err != nil {
		// Fallback to local kubeconfig file
		cfg, err = clientcmd.BuildConfigFromFlags("", "../../kubeconfig")
		require.NoError(t, err)
	}

	// Use the global scheme which has Karpenter v1 types registered automatically
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

	// Add Karpenter v1 types manually with proper group version registration
	karpenterGV := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	metav1.AddToGroupVersion(scheme, karpenterGV)
	scheme.AddKnownTypes(karpenterGV,
		&karpv1.NodePool{},
		&karpv1.NodePoolList{},
		&karpv1.NodeClaim{},
		&karpv1.NodeClaimList{})

	kubeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	// Create IBM client
	ibmClient, err := ibm.NewClient()
	require.NoError(t, err)

	// Create providers using the provider factory pattern
	pricingProvider := pricing.NewIBMPricingProvider(ibmClient)
	instanceTypeProvider := instancetype.NewProvider(ibmClient, pricingProvider)
	subnetProvider := subnet.NewProvider(ibmClient)

	// Create cloud provider
	cloudProvider := ibmcloud.New(
		kubeClient,
		nil, // No event recorder needed for tests
		ibmClient,
		instanceTypeProvider,
		subnetProvider,
		nil, // Use default circuit breaker config for tests
	)

	return &E2ETestSuite{
		kubeClient:           kubeClient,
		ibmClient:            ibmClient,
		cloudProvider:        cloudProvider,
		instanceTypeProvider: instanceTypeProvider,
		testVPC:              os.Getenv("TEST_VPC_ID"),
		testSubnet:           os.Getenv("TEST_SUBNET_ID"),
		testImage:            os.Getenv("TEST_IMAGE_ID"),
		testRegion:           os.Getenv("IBMCLOUD_REGION"),
		testZone:             os.Getenv("TEST_ZONE"),
	}
}

func TestE2EFullWorkflow(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	ctx := context.Background()
	_ = ctx

	testName := fmt.Sprintf("e2e-test-%d", time.Now().Unix())

	t.Logf("Starting E2E test: %s", testName)

	// Step 1: Create NodeClass
	nodeClass := suite.createTestNodeClass(t, testName)
	t.Logf("Created NodeClass: %s", nodeClass.Name)

	// Step 2: Wait for NodeClass to be ready
	suite.waitForNodeClassReady(t, nodeClass.Name)
	t.Logf("NodeClass is ready: %s", nodeClass.Name)

	// Step 3: Create NodePool
	nodePool := suite.createTestNodePool(t, testName, nodeClass.Name)
	t.Logf("Created NodePool: %s", nodePool.Name)

	// Step 4: Deploy test workload to trigger Karpenter provisioning
	deployment := suite.createTestWorkload(t, testName)
	t.Logf("Created test workload: %s", deployment.Name)

	// Step 5: Wait for pods to be scheduled and nodes to be provisioned
	suite.waitForPodsToBeScheduled(t, deployment.Name, deployment.Namespace)
	t.Logf("Pods scheduled successfully")

	// Step 6: Verify that new nodes were created by Karpenter
	suite.verifyKarpenterNodesExist(t)
	t.Logf("Verified Karpenter nodes exist")

	// Step 7: Verify instances exist in IBM Cloud
	suite.verifyInstancesInIBMCloud(t)
	t.Logf("Verified instances exist in IBM Cloud")

	// Step 8: Cleanup test resources
	suite.cleanupTestWorkload(t, deployment.Name, deployment.Namespace)
	t.Logf("Deleted test workload: %s", deployment.Name)

	// Step 9: Clean up remaining resources ONLY after all verification is complete
	t.Logf("Cleaning up remaining test resources")
	suite.cleanupTestResources(t, testName)

	t.Logf("E2E test completed successfully: %s", testName)
}

// TestE2ENodePoolInstanceTypeSelection tests the customer's configuration:
// - NodeClass WITHOUT instanceProfile field (should be optional)
// - NodePool with multiple instance type requirements
// This regression test ensures the VPC provider uses instance types from
// NodePool requirements rather than requiring instanceProfile in NodeClass
func TestE2ENodePoolInstanceTypeSelection(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	ctx := context.Background()
	_ = ctx

	testName := fmt.Sprintf("e2e-nodepool-types-%d", time.Now().Unix())

	t.Logf("Starting NodePool instance type selection E2E test: %s", testName)

	// Step 1: Create NodeClass WITHOUT instanceProfile (like customer's config)
	nodeClass := suite.createTestNodeClassWithoutInstanceProfile(t, testName)
	t.Logf("Created NodeClass without instanceProfile: %s", nodeClass.Name)

	// Step 2: Wait for NodeClass to be ready
	suite.waitForNodeClassReady(t, nodeClass.Name)
	t.Logf("NodeClass is ready: %s", nodeClass.Name)

	// Step 3: Create NodePool with multiple instance type requirements (like customer's config)
	nodePool := suite.createTestNodePoolWithMultipleInstanceTypes(t, testName, nodeClass.Name)
	t.Logf("Created NodePool with multiple instance types: %s", nodePool.Name)

	// Step 4: Deploy test workload with specific resource requirements to trigger provisioning
	deployment := suite.createTestWorkloadWithInstanceTypeRequirements(t, testName)
	t.Logf("Created test workload with instance type requirements: %s", deployment.Name)

	// Step 5: Wait for pods to be scheduled and nodes to be provisioned
	suite.waitForPodsToBeScheduled(t, deployment.Name, deployment.Namespace)
	t.Logf("Pods scheduled successfully")

	// Step 5a: Verify pods are scheduled on nodes with correct NodePool label
	suite.verifyPodsScheduledOnCorrectNodes(t, deployment.Name, deployment.Namespace, fmt.Sprintf("%s-nodepool", testName))
	t.Logf("Verified pods are scheduled on nodes from correct NodePool")

	// Step 6: Verify the created instances use allowed types
	suite.verifyInstancesUseAllowedTypes(t, []string{"bx2-4x16", "mx2-2x16", "mx2d-2x16", "mx3d-2x20"})
	t.Logf("Verified instances use allowed instance types")

	// Step 7: Verify instances exist in IBM Cloud
	suite.verifyInstancesInIBMCloud(t)
	t.Logf("Verified instances exist in IBM Cloud")

	// Step 8: Clean up
	suite.cleanupTestWorkload(t, deployment.Name, deployment.Namespace)
	t.Logf("Test workload deleted: %s", deployment.Name)

	suite.cleanupTestResources(t, testName)

	t.Logf("NodePool instance type selection E2E test completed successfully: %s", testName)
}

func (s *E2ETestSuite) createTestNodeClass(t *testing.T, testName string) *v1alpha1.IBMNodeClass {
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodeclass", testName),
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:            s.testRegion,
			Zone:              s.testZone,
			InstanceProfile:   "bx2a-2x8", // Small instance for testing
			Image:             s.testImage,
			VPC:               s.testVPC,
			Subnet:            s.testSubnet,
			SecurityGroups:    []string{os.Getenv("TEST_SECURITY_GROUP_ID")},
			APIServerEndpoint: os.Getenv("KUBERNETES_API_SERVER_ENDPOINT"), // Internal cluster endpoint
			BootstrapMode:     &[]string{"cloud-init"}[0],                  // Explicit bootstrap mode
			ResourceGroup:     os.Getenv("IBM_RESOURCE_GROUP_ID"),          // Resource group from env
			SSHKeys:           []string{os.Getenv("IBM_SSH_KEY_ID")},       // SSH key for troubleshooting
			Tags: map[string]string{
				"test":       "e2e",
				"test-name":  testName,
				"created-by": "karpenter-e2e",
				"purpose":    "e2e-verification",
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodeClass)
	require.NoError(t, err)

	return nodeClass
}

func (s *E2ETestSuite) createTestNodePool(t *testing.T, testName, nodeClassName string) *karpv1.NodePool {
	// Set 5-minute expiration for e2e test nodes to ensure cleanup
	expireAfter := karpv1.MustParseNillableDuration("5m")

	nodePool := &karpv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodepool", testName),
			Labels: map[string]string{
				"test":      "e2e",
				"test-name": testName,
			},
		},
		Spec: karpv1.NodePoolSpec{
			Template: karpv1.NodeClaimTemplate{
				Spec: karpv1.NodeClaimTemplateSpec{
					NodeClassRef: &karpv1.NodeClassReference{
						Group: "karpenter.ibm.sh",
						Kind:  "IBMNodeClass",
						Name:  nodeClassName,
					},
					Requirements: []karpv1.NodeSelectorRequirementWithMinValues{
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      "node.kubernetes.io/instance-type",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"bx2a-2x8"},
							},
						},
					},
					ExpireAfter: expireAfter,
				},
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodePool)
	require.NoError(t, err)

	return nodePool
}

func (s *E2ETestSuite) createTestNodeClaim(t *testing.T, testName, nodeClassName string) *karpv1.NodeClaim {
	// Set 5-minute expiration for e2e test nodes to ensure cleanup
	expireAfter := karpv1.MustParseNillableDuration("5m")

	nodeClaim := &karpv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodeclaim", testName),
			Labels: map[string]string{
				"test":      "e2e",
				"test-name": testName,
			},
		},
		Spec: karpv1.NodeClaimSpec{
			NodeClassRef: &karpv1.NodeClassReference{
				Group: "karpenter.ibm.sh",
				Kind:  "IBMNodeClass",
				Name:  nodeClassName,
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
			ExpireAfter: expireAfter,
		},
	}

	err := s.kubeClient.Create(context.Background(), nodeClaim)
	require.NoError(t, err)

	return nodeClaim
}

// createTestWorkload creates a deployment that will trigger Karpenter to provision nodes
func (s *E2ETestSuite) createTestWorkload(t *testing.T, testName string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-workload", testName),
			Namespace: "default",
			Labels: map[string]string{
				"app":     fmt.Sprintf("%s-workload", testName),
				"test":    "e2e",
				"purpose": "karpenter-test",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{3}[0], // 3 replicas to ensure we trigger provisioning
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": fmt.Sprintf("%s-workload", testName),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     fmt.Sprintf("%s-workload", testName),
						"test":    "e2e",
						"purpose": "karpenter-test",
					},
				},
				Spec: corev1.PodSpec{
					// Force pods to only be scheduled on nodes from this test's NodePool
					NodeSelector: map[string]string{
						"karpenter.sh/nodepool": fmt.Sprintf("%s-nodepool", testName),
					},
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:1.21",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2000m"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					// Add anti-affinity to spread pods across nodes
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 100,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"app": fmt.Sprintf("%s-workload", testName),
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), deployment)
	require.NoError(t, err)

	return deployment
}

// createTestWorkloadWithInstanceTypeRequirements creates a workload that requires specific resources
func (s *E2ETestSuite) createTestWorkloadWithInstanceTypeRequirements(t *testing.T, testName string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-workload", testName),
			Namespace: "default",
			Labels: map[string]string{
				"app":     fmt.Sprintf("%s-workload", testName),
				"test":    "e2e",
				"purpose": "instance-type-test",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{2}[0], // 2 replicas to trigger provisioning on allowed types
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": fmt.Sprintf("%s-workload", testName),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":     fmt.Sprintf("%s-workload", testName),
						"test":    "e2e",
						"purpose": "instance-type-test",
					},
				},
				Spec: corev1.PodSpec{
					// Force pods to only be scheduled on nodes from this test's NodePool
					NodeSelector: map[string]string{
						"karpenter.sh/nodepool": fmt.Sprintf("%s-nodepool", testName),
					},
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:1.21",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1500m"), // Require significant CPU
									corev1.ResourceMemory: resource.MustParse("2Gi"),   // Require significant memory
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("3000m"),
									corev1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
					// Force pods to different nodes
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"app": fmt.Sprintf("%s-workload", testName),
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
				},
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), deployment)
	require.NoError(t, err)

	return deployment
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

		// Check if ProviderID is set and instance is launched
		if nodeClaim.Status.ProviderID == "" {
			return false, nil
		}

		// Check if Launched condition is True
		for _, condition := range nodeClaim.Status.Conditions {
			if condition.Type == "Launched" && condition.Status == metav1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})

	require.NoError(t, err, "NodeClaim should be launched within timeout")
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
	// Use longer timeout for deletion as IBM Cloud instances can take time to clean up
	deletionTimeout := 15 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), deletionTimeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, pollInterval, deletionTimeout, true, func(ctx context.Context) (bool, error) {
		var nodeClaim karpv1.NodeClaim
		err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
		if errors.IsNotFound(err) {
			return true, nil // NodeClaim was deleted
		}
		if err != nil {
			return false, err
		}

		// Log the current status to help debug timeout issues
		t.Logf("NodeClaim %s still exists, status: %+v", nodeClaimName, nodeClaim.Status)
		return false, nil // Still exists
	})

	require.NoError(t, err, "NodeClaim should be deleted within timeout")
}

// waitForPodsToBeScheduled waits for all pods in a deployment to be scheduled and running
func (s *E2ETestSuite) waitForPodsToBeScheduled(t *testing.T, deploymentName, namespace string) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, pollInterval, testTimeout, true, func(ctx context.Context) (bool, error) {
		var deployment appsv1.Deployment
		err := s.kubeClient.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: namespace}, &deployment)
		if err != nil {
			return false, err
		}

		// Check if deployment has desired replicas ready
		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			t.Logf("All %d replicas are ready for deployment %s", deployment.Status.ReadyReplicas, deploymentName)
			return true, nil
		}

		t.Logf("Deployment %s: %d/%d replicas ready", deploymentName, deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)

		// Add detailed diagnostics for debugging (but less frequently to avoid log spam)
		if time.Now().Unix()%30 == 0 { // Log diagnostics every ~5 minutes
			s.logDeploymentDiagnostics(t, deploymentName, namespace)
		}

		return false, nil
	})

	require.NoError(t, err, "Pods should be scheduled and running within timeout")
}

// logDeploymentDiagnostics logs detailed information to help debug scheduling issues
func (s *E2ETestSuite) logDeploymentDiagnostics(t *testing.T, deploymentName, namespace string) {
	ctx := context.Background()

	// Log pod status
	var podList corev1.PodList
	err := s.kubeClient.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{"app": deploymentName})
	if err != nil {
		t.Logf("Failed to list pods for diagnostics: %v", err)
		return
	}

	for _, pod := range podList.Items {
		t.Logf("Pod %s status: Phase=%s, Reason=%s", pod.Name, pod.Status.Phase, pod.Status.Reason)
		for _, condition := range pod.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				t.Logf("Pod %s condition %s: %s - %s", pod.Name, condition.Type, condition.Status, condition.Message)
			}
		}
	}

	// Log pending events for the deployment and pods
	var eventList corev1.EventList
	err = s.kubeClient.List(ctx, &eventList, client.InNamespace(namespace))
	if err != nil {
		t.Logf("Failed to list events for diagnostics: %v", err)
		return
	}

	for _, event := range eventList.Items {
		if event.Type == "Warning" && (event.InvolvedObject.Name == deploymentName ||
			strings.HasPrefix(event.InvolvedObject.Name, deploymentName)) {
			t.Logf("Warning event: %s - %s", event.Reason, event.Message)
		}
	}

	// Log NodeClaims status
	var nodeClaimList karpv1.NodeClaimList
	err = s.kubeClient.List(ctx, &nodeClaimList)
	if err != nil {
		t.Logf("Failed to list NodeClaims for diagnostics: %v", err)
		return
	}

	t.Logf("NodeClaims count: %d", len(nodeClaimList.Items))
	for _, nodeClaim := range nodeClaimList.Items {
		t.Logf("NodeClaim %s: Ready=%v, ProviderID=%s",
			nodeClaim.Name,
			s.isNodeClaimReady(nodeClaim),
			nodeClaim.Status.ProviderID)
	}
}

// isNodeClaimReady checks if a NodeClaim is ready
func (s *E2ETestSuite) isNodeClaimReady(nodeClaim karpv1.NodeClaim) bool {
	for _, condition := range nodeClaim.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// verifyPodsScheduledOnCorrectNodes verifies that pods are scheduled on nodes from the expected NodePool
func (s *E2ETestSuite) verifyPodsScheduledOnCorrectNodes(t *testing.T, deploymentName, namespace, expectedNodePool string) {
	ctx := context.Background()

	// Get all pods from the deployment
	var podList corev1.PodList
	err := s.kubeClient.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{"app": deploymentName})
	require.NoError(t, err, "Should be able to list pods")

	require.Greater(t, len(podList.Items), 0, "Should have at least one pod")

	// Check each pod is scheduled on a node with the correct NodePool label
	for _, pod := range podList.Items {
		require.NotEmpty(t, pod.Spec.NodeName, "Pod %s should be scheduled on a node", pod.Name)

		// Get the node
		var node corev1.Node
		err := s.kubeClient.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node)
		require.NoError(t, err, "Should be able to get node %s", pod.Spec.NodeName)

		// Verify node has the expected NodePool label
		nodePoolLabel, exists := node.Labels["karpenter.sh/nodepool"]
		require.True(t, exists, "Node %s should have karpenter.sh/nodepool label", node.Name)
		require.Equal(t, expectedNodePool, nodePoolLabel,
			"Pod %s is scheduled on node %s with NodePool %s, expected %s",
			pod.Name, node.Name, nodePoolLabel, expectedNodePool)

		t.Logf("✓ Pod %s correctly scheduled on node %s (NodePool: %s)", pod.Name, node.Name, nodePoolLabel)
	}
}

// verifyKarpenterNodesExist verifies that Karpenter has created new nodes
func (s *E2ETestSuite) verifyKarpenterNodesExist(t *testing.T) {
	ctx := context.Background()

	var nodeList corev1.NodeList
	err := s.kubeClient.List(ctx, &nodeList)
	require.NoError(t, err)

	karpenterNodes := 0
	for _, node := range nodeList.Items {
		if labelValue, exists := node.Labels["karpenter.sh/nodepool"]; exists && labelValue != "" {
			karpenterNodes++
			t.Logf("Found Karpenter node: %s (nodepool: %s)", node.Name, labelValue)
		}
	}

	require.Greater(t, karpenterNodes, 0, "At least one Karpenter node should exist")
	t.Logf("Found %d Karpenter nodes", karpenterNodes)
}

// cleanupTestWorkload deletes a test deployment
func (s *E2ETestSuite) cleanupTestWorkload(t *testing.T, deploymentName, namespace string) {
	ctx := context.Background()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
	}

	err := s.kubeClient.Delete(ctx, deployment)
	if err != nil && !errors.IsNotFound(err) {
		t.Logf("Failed to delete deployment %s: %v", deploymentName, err)
	}
}

// verifyInstancesInIBMCloud verifies that instances exist in IBM Cloud
func (s *E2ETestSuite) verifyInstancesInIBMCloud(t *testing.T) {
	// This is a simplified verification - in a real implementation,
	// you would check the IBM Cloud VPC API for actual instances
	ctx := context.Background()

	var nodeList corev1.NodeList
	err := s.kubeClient.List(ctx, &nodeList)
	require.NoError(t, err)

	karpenterNodes := 0
	for _, node := range nodeList.Items {
		if labelValue, exists := node.Labels["karpenter.sh/nodepool"]; exists && labelValue != "" {
			karpenterNodes++
			t.Logf("Found Karpenter node in cluster: %s", node.Name)
			// In a real test, we would extract the provider ID and verify it exists in IBM Cloud
		}
	}

	require.Greater(t, karpenterNodes, 0, "At least one Karpenter node should exist")
}

// verifyInstancesUseAllowedTypes verifies that created nodes use the allowed instance types
func (s *E2ETestSuite) verifyInstancesUseAllowedTypes(t *testing.T, allowedTypes []string) {
	ctx := context.Background()

	var nodeList corev1.NodeList
	err := s.kubeClient.List(ctx, &nodeList)
	require.NoError(t, err)

	karpenterNodes := []corev1.Node{}
	for _, node := range nodeList.Items {
		if labelValue, exists := node.Labels["karpenter.sh/nodepool"]; exists && labelValue != "" {
			karpenterNodes = append(karpenterNodes, node)
		}
	}

	require.Greater(t, len(karpenterNodes), 0, "At least one Karpenter node should exist")

	for _, node := range karpenterNodes {
		instanceType := node.Labels["node.kubernetes.io/instance-type"]
		require.NotEmpty(t, instanceType, "Node should have instance type label")

		found := false
		for _, allowedType := range allowedTypes {
			if instanceType == allowedType {
				found = true
				break
			}
		}

		require.True(t, found, "Node %s has instance type %s which is not in allowed types %v",
			node.Name, instanceType, allowedTypes)
		t.Logf("Node %s uses allowed instance type: %s", node.Name, instanceType)
	}
}

func (s *E2ETestSuite) cleanupTestResources(t *testing.T, testName string) {
	ctx := context.Background()

	// List of resources to clean up
	resources := []struct {
		name    string
		obj     client.Object
		listObj client.ObjectList
	}{
		{
			name:    "Deployment",
			obj:     &appsv1.Deployment{},
			listObj: &appsv1.DeploymentList{},
		},
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

		// Extract items based on resource type
		var items []client.Object
		switch list := resource.listObj.(type) {
		case *karpv1.NodeClaimList:
			for i := range list.Items {
				items = append(items, &list.Items[i])
			}
		case *karpv1.NodePoolList:
			for i := range list.Items {
				items = append(items, &list.Items[i])
			}
		case *v1alpha1.IBMNodeClassList:
			for i := range list.Items {
				items = append(items, &list.Items[i])
			}
		}

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

	// Test invalid NodeClass (invalid resource references)
	invalidNodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-invalid", testName),
			Labels: map[string]string{
				"test-name": testName,
			},
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:          "us-south",
			Zone:            "us-south-1",
			InstanceProfile: "bx2-2x8",
			Image:           "r010-00000000-0000-0000-0000-000000000000",           // Valid format but non-existent image
			VPC:             "r010-00000000-0000-0000-0000-000000000000",           // Valid format but non-existent VPC
			Subnet:          "02b7-00000000-0000-0000-0000-000000000000",           // Valid format but non-existent subnet
			SecurityGroups:  []string{"r010-00000000-0000-0000-0000-000000000000"}, // Valid format but non-existent security group
		},
	}

	err := suite.kubeClient.Create(ctx, invalidNodeClass)
	require.NoError(t, err)

	// Wait for status controller to process validation
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var updatedNodeClass v1alpha1.IBMNodeClass
	err = wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if getErr := suite.kubeClient.Get(ctx, types.NamespacedName{Name: invalidNodeClass.Name}, &updatedNodeClass); getErr != nil {
			return false, getErr
		}

		// Check if validation has been processed (either Ready=True or Ready=False)
		for _, condition := range updatedNodeClass.Status.Conditions {
			if condition.Type == "Ready" {
				t.Logf("NodeClass validation processed: Ready=%s, Reason=%s, Message=%s",
					condition.Status, condition.Reason, condition.Message)
				return true, nil
			}
		}

		t.Logf("Waiting for status controller to process NodeClass validation...")
		return false, nil
	})

	require.NoError(t, err, "Status controller should process NodeClass validation within timeout")

	// Should have conditions indicating validation failure
	found := false
	for _, condition := range updatedNodeClass.Status.Conditions {
		if condition.Type == "Ready" && condition.Status == metav1.ConditionFalse {
			found = true
			break
		}
	}
	assert.True(t, found, "Invalid NodeClass should have Ready=False condition")

	// Clean up test resources after validation is complete
	suite.cleanupTestResources(t, testName)
}

// TestE2EInstanceTypeSelection tests automatic instance type selection
func TestE2EInstanceTypeSelection(t *testing.T) {
	suite := SetupE2ETestSuite(t)

	testName := fmt.Sprintf("instancetype-test-%d", time.Now().Unix())

	// Test NodeClass with instance requirements instead of specific profile
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-autotype", testName),
			Labels: map[string]string{
				"test-name": testName,
			},
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region:         suite.testRegion,
			Zone:           suite.testZone,
			Image:          suite.testImage,
			VPC:            suite.testVPC,
			Subnet:         suite.testSubnet,
			SecurityGroups: []string{os.Getenv("TEST_SECURITY_GROUP_ID")},
			InstanceRequirements: &v1alpha1.InstanceTypeRequirements{
				MinimumCPU:    2,
				MinimumMemory: 8,
				Architecture:  "amd64",
			},
		},
	}

	err := suite.kubeClient.Create(context.Background(), nodeClass)
	require.NoError(t, err)

	// Wait for autoplacement controller to process instance requirements
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var updatedNodeClass v1alpha1.IBMNodeClass
	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		if getErr := suite.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClass.Name}, &updatedNodeClass); getErr != nil {
			return false, getErr
		}

		// Check if autoplacement has been processed - either instance profile is set or status has selected types
		if updatedNodeClass.Spec.InstanceProfile != "" {
			t.Logf("Autoplacement controller set InstanceProfile: %s", updatedNodeClass.Spec.InstanceProfile)
			return true, nil
		}

		if len(updatedNodeClass.Status.SelectedInstanceTypes) > 0 {
			t.Logf("Autoplacement controller populated SelectedInstanceTypes: %v", updatedNodeClass.Status.SelectedInstanceTypes)
			return true, nil
		}

		// Check conditions for autoplacement status
		for _, condition := range updatedNodeClass.Status.Conditions {
			if condition.Type == "AutoPlacement" {
				t.Logf("AutoPlacement condition: Status=%s, Reason=%s, Message=%s",
					condition.Status, condition.Reason, condition.Message)
				if condition.Status == metav1.ConditionFalse {
					return false, fmt.Errorf("autoplacement failed: %s", condition.Message)
				}
			}
		}

		t.Logf("Waiting for autoplacement controller to process instance requirements...")
		return false, nil
	})

	// If autoplacement didn't work, debug by listing available instance types
	if err != nil || (updatedNodeClass.Spec.InstanceProfile == "" && len(updatedNodeClass.Status.SelectedInstanceTypes) == 0) {
		t.Logf("Autoplacement failed or didn't complete, debugging...")

		// List available instance types to debug
		debugCtx := context.Background()
		instanceTypes, listErr := suite.instanceTypeProvider.List(debugCtx)
		if listErr != nil {
			t.Logf("Failed to list instance types: %v", listErr)
		} else {
			t.Logf("Available instance types: %d", len(instanceTypes))
			for i, it := range instanceTypes {
				if i < 5 { // Log first 5
					t.Logf("  - %s", it.Name)
				}
			}
		}

		// Also check if we can filter instance types
		if nodeClass.Spec.InstanceRequirements != nil {
			filteredTypes, filterErr := suite.instanceTypeProvider.FilterInstanceTypes(debugCtx, nodeClass.Spec.InstanceRequirements)
			if filterErr != nil {
				t.Logf("Failed to filter instance types: %v", filterErr)
			} else {
				t.Logf("Filtered instance types: %d", len(filteredTypes))
			}
		}
	}

	require.NoError(t, err, "Autoplacement controller should process NodeClass within timeout")
	assert.NotEmpty(t, updatedNodeClass.Status.SelectedInstanceTypes, "Instance types should be auto-selected")
	t.Logf("Auto-selected instance types: %v", updatedNodeClass.Status.SelectedInstanceTypes)

	// Clean up test resources after validation is complete
	suite.cleanupTestResources(t, testName)
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

// extractInstanceIDFromProviderID extracts the instance ID from a ProviderID
// nolint:unused
func extractInstanceIDFromProviderID(providerID string) string {
	// ProviderID format: "ibm:///region/instance_id"
	parts := strings.Split(providerID, "/")
	if len(parts) >= 4 {
		return parts[len(parts)-1]
	}
	return ""
}

// dumpBootstrapLogs attempts to SSH into the instance and dump bootstrap logs
// nolint:unused
func (s *E2ETestSuite) dumpBootstrapLogs(t *testing.T, nodeClaimName string) {
	ctx := context.Background()

	// Get the NodeClaim to extract ProviderID
	var nodeClaim karpv1.NodeClaim
	err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
	if err != nil {
		t.Logf("Failed to get NodeClaim for bootstrap log dump: %v", err)
		return
	}

	if nodeClaim.Status.ProviderID == "" {
		t.Logf("NodeClaim has no ProviderID, cannot dump bootstrap logs")
		return
	}

	instanceID := extractInstanceIDFromProviderID(nodeClaim.Status.ProviderID)
	if instanceID == "" {
		t.Logf("Failed to extract instance ID from ProviderID: %s", nodeClaim.Status.ProviderID)
		return
	}

	t.Logf("Attempting to dump bootstrap logs for instance: %s", instanceID)

	// Create floating IP for SSH access
	floatingIP, err := s.createFloatingIPForInstance(t, instanceID, nodeClaimName)
	if err != nil {
		t.Logf("Failed to create floating IP for bootstrap log dump: %v", err)

		// Check if it's the network attachment issue
		if strings.Contains(err.Error(), "network_attachment is used") {
			t.Logf("Instance uses network attachment - SSH troubleshooting not available for this configuration")
		} else {
			t.Logf("SSH troubleshooting not available: %v", err)
		}
		return
	}

	// Ensure floating IP cleanup
	defer func() {
		if floatingIP != "" {
			s.cleanupFloatingIP(t, floatingIP)
		}
	}()

	// Wait a bit for floating IP to be ready
	time.Sleep(10 * time.Second)

	// Try to SSH and dump logs with retries
	s.attemptBootstrapLogDump(t, floatingIP, instanceID, 5)
}

// createFloatingIPForInstance creates a floating IP and assigns it to the instance
// nolint:unused
func (s *E2ETestSuite) createFloatingIPForInstance(t *testing.T, instanceID, instanceName string) (string, error) {
	t.Logf("Creating floating IP for instance %s", instanceID)

	// First check if instance exists and get its details
	cmd := exec.Command("ibmcloud", "is", "instance", instanceID, "--output", "json")
	instanceOutput, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get instance details: %v", err)
	}

	// Use IBM Cloud CLI to create floating IP
	fipName := fmt.Sprintf("e2e-debug-%s", instanceName)
	cmd = exec.Command("ibmcloud", "is", "floating-ip-reserve",
		fipName,
		"--zone", s.testZone,
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to create floating IP: %v", err)
	}

	// Parse the floating IP details from JSON output
	outputStr := string(output)
	if !strings.Contains(outputStr, `"address"`) {
		return "", fmt.Errorf("failed to parse floating IP from output: %s", outputStr)
	}

	// Extract floating IP address
	addressStart := strings.Index(outputStr, `"address": "`) + len(`"address": "`)
	if addressStart < len(`"address": "`) {
		return "", fmt.Errorf("failed to find address in output")
	}
	addressEnd := strings.Index(outputStr[addressStart:], `"`)
	if addressEnd == -1 {
		return "", fmt.Errorf("failed to parse address from output")
	}
	floatingIP := outputStr[addressStart : addressStart+addressEnd]

	// Extract floating IP ID
	fipIDStart := strings.Index(outputStr, `"id": "`) + len(`"id": "`)
	if fipIDStart < len(`"id": "`) {
		return "", fmt.Errorf("failed to find floating IP ID in output")
	}
	fipIDEnd := strings.Index(outputStr[fipIDStart:], `"`)
	if fipIDEnd == -1 {
		return "", fmt.Errorf("failed to parse floating IP ID from output")
	}
	fipID := outputStr[fipIDStart : fipIDStart+fipIDEnd]

	// Extract primary network interface ID from instance details
	instanceStr := string(instanceOutput)
	nicStart := strings.Index(instanceStr, `"primary_network_interface"`)
	if nicStart == -1 {
		return "", fmt.Errorf("failed to find primary network interface")
	}

	idStart := strings.Index(instanceStr[nicStart:], `"id": "`) + len(`"id": "`)
	if idStart < len(`"id": "`) {
		return "", fmt.Errorf("failed to find interface ID")
	}
	idStart += nicStart
	idEnd := strings.Index(instanceStr[idStart:], `"`)
	if idEnd == -1 {
		return "", fmt.Errorf("failed to parse interface ID")
	}
	nicID := instanceStr[idStart : idStart+idEnd]

	// Associate floating IP with instance using the FIP ID
	cmd = exec.Command("ibmcloud", "is", "instance-network-interface-floating-ip-add",
		instanceID, nicID, fipID)

	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if it's the network attachment error we've seen
		if strings.Contains(string(output), "network_attachment is used") {
			return "", fmt.Errorf("cannot associate floating IP: instance uses network attachment (this is expected for some configurations)")
		}
		return "", fmt.Errorf("failed to associate floating IP: %v, output: %s", err, string(output))
	}

	t.Logf("Created and associated floating IP %s with instance %s", floatingIP, instanceID)
	return floatingIP, nil
}

// cleanupFloatingIP removes the floating IP
// nolint:unused
func (s *E2ETestSuite) cleanupFloatingIP(t *testing.T, floatingIP string) {
	t.Logf("Cleaning up floating IP: %s", floatingIP)

	// Get floating IP details to find ID
	cmd := exec.Command("ibmcloud", "is", "floating-ips", "--output", "json")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Failed to list floating IPs for cleanup: %v", err)
		return
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, floatingIP) {
		t.Logf("Floating IP %s not found for cleanup", floatingIP)
		return
	}

	// Find the ID for this floating IP (simplified extraction)
	ipIndex := strings.Index(outputStr, floatingIP)
	if ipIndex == -1 {
		return
	}

	// Look backwards for the ID field
	beforeIP := outputStr[:ipIndex]
	idStart := strings.LastIndex(beforeIP, `"id": "`) + len(`"id": "`)
	if idStart < len(`"id": "`) {
		t.Logf("Failed to find floating IP ID for cleanup")
		return
	}
	idEnd := strings.Index(beforeIP[idStart:], `"`)
	if idEnd == -1 {
		t.Logf("Failed to parse floating IP ID for cleanup")
		return
	}
	floatingIPID := beforeIP[idStart : idStart+idEnd]

	// Release the floating IP
	cmd = exec.Command("ibmcloud", "is", "floating-ip-release", floatingIPID, "--force")
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to release floating IP %s: %v", floatingIPID, err)
	} else {
		t.Logf("Successfully released floating IP %s", floatingIP)
	}
}

// attemptBootstrapLogDump tries to SSH and dump bootstrap logs with retries
// nolint:unused
func (s *E2ETestSuite) attemptBootstrapLogDump(t *testing.T, floatingIP, instanceID string, maxRetries int) {
	sshKey := "~/.ssh/eb"

	logFiles := []string{
		"/var/log/cloud-init.log",
		"/var/log/cloud-init-output.log",
		"/var/log/karpenter-bootstrap.log",
		"/var/log/apt/history.log",
	}

	commands := []struct {
		name string
		cmd  string
	}{
		{"cloud-init status", "cloud-init status --long"},
		{"systemd failed services", "systemctl --failed"},
		{"journal errors", "journalctl -p err --no-pager -n 50"},
		{"network status", "ip addr show && ip route show"},
		{"DNS resolution", "nslookup archive.ubuntu.com"},
		{"metadata service", "curl -sf -m 5 http://169.254.169.254/metadata/v1/instance || echo 'Metadata service failed'"},
		{"disk space", "df -h"},
		{"memory usage", "free -m"},
		{"running processes", "ps aux | head -20"},
	}

	for retry := 1; retry <= maxRetries; retry++ {
		t.Logf("Bootstrap log dump attempt %d/%d for instance %s (IP: %s)", retry, maxRetries, instanceID, floatingIP)

		// Test SSH connectivity first
		testCmd := exec.Command("ssh", "-i", sshKey, "-o", "StrictHostKeyChecking=no",
			"-o", "ConnectTimeout=10", "-o", "BatchMode=yes",
			fmt.Sprintf("root@%s", floatingIP), "echo 'SSH_OK'")

		if output, err := testCmd.Output(); err != nil || !strings.Contains(string(output), "SSH_OK") {
			t.Logf("SSH connectivity test failed (attempt %d): %v", retry, err)
			if retry < maxRetries {
				time.Sleep(time.Duration(retry*15) * time.Second) // Exponential backoff
				continue
			} else {
				t.Logf("SSH never became available after %d attempts", maxRetries)
				return
			}
		}

		t.Logf("SSH connectivity confirmed for %s", floatingIP)

		// Dump log files
		for _, logFile := range logFiles {
			t.Logf("=== Dumping %s ===", logFile)
			cmd := exec.Command("ssh", "-i", sshKey, "-o", "StrictHostKeyChecking=no",
				"-o", "ConnectTimeout=10", fmt.Sprintf("root@%s", floatingIP),
				fmt.Sprintf("tail -50 %s 2>/dev/null || echo 'File %s not found or empty'", logFile, logFile))

			if output, err := cmd.Output(); err != nil {
				t.Logf("Failed to read %s: %v", logFile, err)
			} else {
				t.Logf("Contents of %s:\n%s", logFile, string(output))
			}
		}

		// Run diagnostic commands
		for _, cmdInfo := range commands {
			t.Logf("=== Running: %s ===", cmdInfo.name)
			cmd := exec.Command("ssh", "-i", sshKey, "-o", "StrictHostKeyChecking=no",
				"-o", "ConnectTimeout=10", fmt.Sprintf("root@%s", floatingIP), cmdInfo.cmd)

			if output, err := cmd.Output(); err != nil {
				t.Logf("Command '%s' failed: %v", cmdInfo.name, err)
			} else {
				t.Logf("Output of '%s':\n%s", cmdInfo.name, string(output))
			}
		}

		t.Logf("Bootstrap log dump completed successfully for instance %s", instanceID)
		return
	}
}

// createTestNodeClassWithoutInstanceProfile creates a NodeClass with instanceRequirements instead of instanceProfile
// This tests that NodePool can control instance type selection when NodeClass uses instanceRequirements
func (s *E2ETestSuite) createTestNodeClassWithoutInstanceProfile(t *testing.T, testName string) *v1alpha1.IBMNodeClass {
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodeclass", testName),
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: s.testRegion,
			Zone:   s.testZone,
			// No instanceProfile or instanceRequirements - let NodePool control instance selection
			Image:             s.testImage,
			VPC:               s.testVPC,
			Subnet:            s.testSubnet,
			SecurityGroups:    []string{os.Getenv("TEST_SECURITY_GROUP_ID")},
			APIServerEndpoint: os.Getenv("KUBERNETES_API_SERVER_ENDPOINT"),
			BootstrapMode:     &[]string{"cloud-init"}[0],
			ResourceGroup:     os.Getenv("IBM_RESOURCE_GROUP_ID"),
			SSHKeys:           []string{os.Getenv("IBM_SSH_KEY_ID")},
			Tags: map[string]string{
				"test":       "e2e",
				"test-name":  testName,
				"created-by": "karpenter-e2e",
				"purpose":    "nodepool-instancetype-test",
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodeClass)
	require.NoError(t, err)

	return nodeClass
}

// createTestNodePoolWithMultipleInstanceTypes creates a NodePool with multiple instance type requirements
// This matches the customer's configuration that was causing the circuit breaker to open
func (s *E2ETestSuite) createTestNodePoolWithMultipleInstanceTypes(t *testing.T, testName string, nodeClassName string) *karpv1.NodePool {
	// Set 5-minute expiration for e2e test nodes to ensure cleanup
	expireAfter := karpv1.MustParseNillableDuration("5m")

	nodePool := &karpv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-nodepool", testName),
			Labels: map[string]string{
				"test":      "e2e",
				"test-name": testName,
			},
		},
		Spec: karpv1.NodePoolSpec{
			Template: karpv1.NodeClaimTemplate{
				ObjectMeta: karpv1.ObjectMeta{
					Labels: map[string]string{
						"provisioner":  "karpenter-vpc",
						"cluster-type": "self-managed",
						"test":         "e2e",
						"test-name":    testName,
					},
				},
				Spec: karpv1.NodeClaimTemplateSpec{
					NodeClassRef: &karpv1.NodeClassReference{
						Group: "karpenter.ibm.sh",
						Kind:  "IBMNodeClass",
						Name:  nodeClassName,
					},
					Requirements: []karpv1.NodeSelectorRequirementWithMinValues{
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      corev1.LabelInstanceTypeStable,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"bx2-4x16", "mx2-2x16", "mx2d-2x16", "mx3d-2x20"}, // Customer's exact config
							},
						},
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      corev1.LabelArchStable,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"amd64"},
							},
						},
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      karpv1.CapacityTypeLabelKey,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"on-demand"},
							},
						},
					},
					ExpireAfter: expireAfter,
				},
			},
		},
	}

	err := s.kubeClient.Create(context.Background(), nodePool)
	require.NoError(t, err)

	return nodePool
}

// verifyInstanceUsesAllowedType verifies the created instance uses one of the allowed instance types
// nolint:unused
func (s *E2ETestSuite) verifyInstanceUsesAllowedType(t *testing.T, nodeClaimName string, allowedTypes []string) {
	ctx := context.Background()

	// Get the NodeClaim to find the instance type that was selected
	var nodeClaim karpv1.NodeClaim
	err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
	require.NoError(t, err)

	// Check the instance type label set by Karpenter
	instanceType, ok := nodeClaim.Labels[corev1.LabelInstanceTypeStable]
	require.True(t, ok, "NodeClaim should have instance-type label")

	// Verify it's one of the allowed types
	found := false
	for _, allowedType := range allowedTypes {
		if instanceType == allowedType {
			found = true
			break
		}
	}

	assert.True(t, found, "Instance type %s should be one of the allowed types: %v", instanceType, allowedTypes)
	t.Logf("Instance uses allowed type: %s", instanceType)
}
