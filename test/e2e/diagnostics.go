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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// logDeploymentDiagnostics logs detailed diagnostic information for a deployment
func (s *E2ETestSuite) logDeploymentDiagnostics(t *testing.T, deploymentName, namespace string) {
	ctx := context.Background()

	// Log pod status
	var podList corev1.PodList
	err := s.kubeClient.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{"app": deploymentName})
	if err != nil {
		t.Logf("Failed to list pods for diagnostics: %v", err)
		return
	}

	t.Logf("Diagnostic information for deployment %s:", deploymentName)

	for _, pod := range podList.Items {
		t.Logf("Pod %s status: Phase=%s, Reason=%s", pod.Name, pod.Status.Phase, pod.Status.Reason)

		// Log conditions that are not True
		for _, condition := range pod.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				t.Logf("Pod %s condition %s: %s - %s", pod.Name, condition.Type, condition.Status, condition.Message)
			}
		}

		// Log container statuses
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				t.Logf("Container %s not ready: %+v", containerStatus.Name, containerStatus.State)
			}
		}

		// Log events for this pod
		var eventList corev1.EventList
		err := s.kubeClient.List(ctx, &eventList, client.InNamespace(namespace))
		if err == nil {
			for _, event := range eventList.Items {
				if event.InvolvedObject.Kind == "Pod" && event.InvolvedObject.Name == pod.Name {
					t.Logf("Pod %s event: %s - %s", pod.Name, event.Reason, event.Message)
				}
			}
		}
	}
}

// logNodeClassEvents logs all events related to a specific NodeClass
func (s *E2ETestSuite) logNodeClassEvents(t *testing.T, nodeClassName string) {
	ctx := context.Background()

	// List events in all namespaces
	var eventList corev1.EventList
	err := s.kubeClient.List(ctx, &eventList)
	if err != nil {
		t.Logf("Failed to list events: %v", err)
		return
	}

	t.Logf("Events related to NodeClass %s:", nodeClassName)
	eventFound := false

	for _, event := range eventList.Items {
		// Check if event is related to our NodeClass
		if event.InvolvedObject.Kind == "IBMNodeClass" && event.InvolvedObject.Name == nodeClassName {
			eventFound = true
			t.Logf("  [%s] %s: %s - %s",
				event.FirstTimestamp.Format("15:04:05"),
				event.Type, event.Reason, event.Message)
		}
	}

	if !eventFound {
		t.Logf("  No events found for NodeClass %s", nodeClassName)
	}
}

// logNodePoolStatus logs the current status of a NodePool
func (s *E2ETestSuite) logNodePoolStatus(t *testing.T, nodePoolName string) {
	ctx := context.Background()

	var nodePool karpv1.NodePool
	err := s.kubeClient.Get(ctx, types.NamespacedName{Name: nodePoolName}, &nodePool)
	if err != nil {
		t.Logf("Failed to get NodePool %s: %v", nodePoolName, err)
		return
	}

	t.Logf("NodePool %s status:", nodePoolName)
	t.Logf("  Conditions:")
	for _, condition := range nodePool.Status.Conditions {
		t.Logf("    - Type: %s, Status: %s, Reason: %s, Message: %s",
			condition.Type, condition.Status, condition.Reason, condition.Message)
	}

	// Log NodePool resources
	t.Logf("  Resources:")
	if nodePool.Status.Resources != nil {
		t.Logf("    - CPU: %v", nodePool.Status.Resources[corev1.ResourceCPU])
		t.Logf("    - Memory: %v", nodePool.Status.Resources[corev1.ResourceMemory])
		t.Logf("    - Pods: %v", nodePool.Status.Resources[corev1.ResourcePods])
	}
}

// logNodeClaimStatus logs the status of all NodeClaims, focusing on test-related ones
func (s *E2ETestSuite) logNodeClaimStatus(t *testing.T) {
	ctx := context.Background()

	var nodeClaimList karpv1.NodeClaimList
	err := s.kubeClient.List(ctx, &nodeClaimList)
	if err != nil {
		t.Logf("Failed to list NodeClaims: %v", err)
		return
	}

	t.Logf("NodeClaims in cluster: %d", len(nodeClaimList.Items))

	for _, nodeClaim := range nodeClaimList.Items {
		// Only log test-related NodeClaims
		if val, ok := nodeClaim.Labels["test"]; ok && val == "e2e" {
			t.Logf("  NodeClaim %s:", nodeClaim.Name)
			t.Logf("    ProviderID: %s", nodeClaim.Status.ProviderID)
			t.Logf("    NodeName: %s", nodeClaim.Status.NodeName)
			t.Logf("    Conditions:")

			for _, condition := range nodeClaim.Status.Conditions {
				t.Logf("      - %s: %s (%s)", condition.Type, condition.Status, condition.Reason)
				if condition.Message != "" {
					t.Logf("        Message: %s", condition.Message)
				}
			}

			// Log allocatable resources
			if len(nodeClaim.Status.Allocatable) > 0 {
				t.Logf("    Allocatable:")
				for resource, quantity := range nodeClaim.Status.Allocatable {
					t.Logf("      - %s: %v", resource, quantity)
				}
			}
		}
	}
}
