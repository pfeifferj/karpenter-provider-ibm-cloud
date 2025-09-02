//go:build e2e
// +build e2e

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
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// deleteNodeClaim deletes a specific NodeClaim
func (s *E2ETestSuite) deleteNodeClaim(t *testing.T, nodeClaimName string) {
	ctx := context.Background()
	var nodeClaim karpv1.NodeClaim
	err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodeClaimName}, &nodeClaim)
	require.NoError(t, err)
	err = s.kubeClient.Delete(ctx, &nodeClaim)
	require.NoError(t, err)
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

// cleanupTestResources performs comprehensive cleanup of test resources
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
		{
			name:    "PodDisruptionBudget",
			obj:     &policyv1.PodDisruptionBudget{},
			listObj: &policyv1.PodDisruptionBudgetList{},
		},
	}

	// Clean up resources by test name label
	for _, resource := range resources {
		t.Logf("Cleaning up %s resources for test %s", resource.name, testName)

		// List resources with the test label
		err := s.kubeClient.List(ctx, resource.listObj, client.MatchingLabels{
			"test-name": testName,
		})
		if err != nil {
			t.Logf("Failed to list %s resources: %v", resource.name, err)
			continue
		}

		// Extract items using reflection or type assertions
		switch list := resource.listObj.(type) {
		case *appsv1.DeploymentList:
			for _, item := range list.Items {
				if err := s.kubeClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					t.Logf("Failed to delete %s %s: %v", resource.name, item.Name, err)
				}
			}
		case *karpv1.NodeClaimList:
			for _, item := range list.Items {
				if err := s.kubeClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					t.Logf("Failed to delete %s %s: %v", resource.name, item.Name, err)
				}
			}
		case *karpv1.NodePoolList:
			for _, item := range list.Items {
				if err := s.kubeClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					t.Logf("Failed to delete %s %s: %v", resource.name, item.Name, err)
				}
			}
		case *v1alpha1.IBMNodeClassList:
			for _, item := range list.Items {
				if err := s.kubeClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					t.Logf("Failed to delete %s %s: %v", resource.name, item.Name, err)
				}
			}
		case *policyv1.PodDisruptionBudgetList:
			for _, item := range list.Items {
				if err := s.kubeClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
					t.Logf("Failed to delete %s %s: %v", resource.name, item.Name, err)
				}
			}
		}
	}

	// Wait a bit for cleanup to propagate
	time.Sleep(5 * time.Second)
	t.Logf("✅ Cleanup completed for test %s", testName)
}

// cleanupFloatingIP removes a floating IP from IBM Cloud
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

	// Find the floating IP ID from the output
	lines := strings.Split(outputStr, "\n")
	var floatingIPID string
	for _, line := range lines {
		if strings.Contains(line, floatingIP) && strings.Contains(line, "\"id\"") {
			// Parse JSON to extract ID - simplified parsing
			parts := strings.Split(line, "\"")
			for i, part := range parts {
				if part == "id" && i+2 < len(parts) {
					floatingIPID = parts[i+2]
					break
				}
			}
			break
		}
	}

	if floatingIPID == "" {
		t.Logf("Could not find ID for floating IP %s", floatingIP)
		return
	}

	// Delete the floating IP
	deleteCmd := exec.Command("ibmcloud", "is", "floating-ip-delete", floatingIPID, "--force")
	if err := deleteCmd.Run(); err != nil {
		t.Logf("Failed to delete floating IP %s: %v", floatingIP, err)
		return
	}

	t.Logf("✅ Successfully cleaned up floating IP: %s", floatingIP)
}

// cleanupAllE2EResources performs a comprehensive cleanup of all E2E test resources
func (s *E2ETestSuite) cleanupAllE2EResources(t *testing.T) {
	ctx := context.Background()
	t.Logf("Starting comprehensive E2E resource cleanup")

	// Clean up all resources with the e2e test label
	err := s.kubeClient.List(ctx, &appsv1.DeploymentList{}, client.MatchingLabels{"test": "e2e"})
	if err == nil {
		var deploymentList appsv1.DeploymentList
		s.kubeClient.List(ctx, &deploymentList, client.MatchingLabels{"test": "e2e"})
		for _, deployment := range deploymentList.Items {
			s.kubeClient.Delete(ctx, &deployment)
			t.Logf("Deleted deployment: %s", deployment.Name)
		}
	}

	// Clean up NodeClaims
	var nodeClaimList karpv1.NodeClaimList
	err = s.kubeClient.List(ctx, &nodeClaimList, client.MatchingLabels{"test": "e2e"})
	if err == nil {
		for _, nodeClaim := range nodeClaimList.Items {
			s.kubeClient.Delete(ctx, &nodeClaim)
			t.Logf("Deleted NodeClaim: %s", nodeClaim.Name)
		}
	}

	// Clean up NodePools
	var nodePoolList karpv1.NodePoolList
	err = s.kubeClient.List(ctx, &nodePoolList, client.MatchingLabels{"test": "e2e"})
	if err == nil {
		for _, nodePool := range nodePoolList.Items {
			s.kubeClient.Delete(ctx, &nodePool)
			t.Logf("Deleted NodePool: %s", nodePool.Name)
		}
	}

	// Clean up IBMNodeClasses
	var nodeClassList v1alpha1.IBMNodeClassList
	err = s.kubeClient.List(ctx, &nodeClassList, client.MatchingLabels{"test": "e2e"})
	if err == nil {
		for _, nodeClass := range nodeClassList.Items {
			s.kubeClient.Delete(ctx, &nodeClass)
			t.Logf("Deleted IBMNodeClass: %s", nodeClass.Name)
		}
	}

	// Clean up PodDisruptionBudgets
	var pdbList policyv1.PodDisruptionBudgetList
	err = s.kubeClient.List(ctx, &pdbList, client.MatchingLabels{"test": "e2e"})
	if err == nil {
		for _, pdb := range pdbList.Items {
			s.kubeClient.Delete(ctx, &pdb)
			t.Logf("Deleted PodDisruptionBudget: %s", pdb.Name)
		}
	}

	t.Logf("✅ Comprehensive E2E resource cleanup completed")
}

// cleanupOrphanedKubernetesResources removes resources that may be left behind after tests
func (s *E2ETestSuite) cleanupOrphanedKubernetesResources(t *testing.T) {
	ctx := context.Background()
	t.Logf("Cleaning up orphaned Kubernetes resources")

	// Find nodes without corresponding NodeClaims
	var nodeList corev1.NodeList
	err := s.kubeClient.List(ctx, &nodeList, client.MatchingLabels{
		"karpenter.sh/nodepool": "",
	})
	if err != nil {
		t.Logf("Failed to list Karpenter nodes: %v", err)
		return
	}

	var nodeClaimList karpv1.NodeClaimList
	err = s.kubeClient.List(ctx, &nodeClaimList)
	if err != nil {
		t.Logf("Failed to list NodeClaims: %v", err)
		return
	}

	// Create a map of NodeClaim names to check for orphaned nodes
	nodeClaimNames := make(map[string]bool)
	for _, nodeClaim := range nodeClaimList.Items {
		if nodeClaim.Status.NodeName != "" {
			nodeClaimNames[nodeClaim.Status.NodeName] = true
		}
	}

	// Check for orphaned nodes
	orphanedNodes := 0
	for _, node := range nodeList.Items {
		if _, exists := node.Labels["karpenter.sh/nodepool"]; exists {
			if !nodeClaimNames[node.Name] {
				t.Logf("Found potentially orphaned node: %s", node.Name)
				orphanedNodes++
			}
		}
	}

	if orphanedNodes > 0 {
		t.Logf("Found %d potentially orphaned nodes", orphanedNodes)
	} else {
		t.Logf("✅ No orphaned nodes found")
	}

	// Clean up failed pods that might be stuck
	var podList corev1.PodList
	err = s.kubeClient.List(ctx, &podList, client.InNamespace("default"))
	if err != nil {
		t.Logf("Failed to list pods: %v", err)
		return
	}

	failedPods := 0
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodFailed && pod.Labels["test"] == "e2e" {
			err := s.kubeClient.Delete(ctx, &pod)
			if err != nil {
				t.Logf("Failed to delete failed pod %s: %v", pod.Name, err)
			} else {
				failedPods++
			}
		}
	}

	if failedPods > 0 {
		t.Logf("Cleaned up %d failed E2E pods", failedPods)
	}

	t.Logf("✅ Orphaned resource cleanup completed")
}
