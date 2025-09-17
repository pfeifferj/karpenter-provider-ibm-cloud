//go:build e2e
// +build e2e

/*
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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

// TestE2EUserDataAppend tests that userDataAppend is correctly appended to the bootstrap script
func TestE2EUserDataAppend(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	testName := fmt.Sprintf("userdata-append-%d", time.Now().Unix())
	t.Logf("Starting userDataAppend test: %s", testName)
	ctx := context.Background()

	// Create NodeClass with userDataAppend
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: testName + "-nodeclass",
		},
		Spec: suite.CreateDefaultNodeClassSpec(),
	}

	// Add userDataAppend to the spec
	nodeClass.Spec.UserDataAppend = &[]string{`
echo "USERDATA_APPEND_E2E_TEST_START" | tee /var/log/userdata-append-e2e.log
touch /tmp/userdata-append-e2e-marker
echo "E2E test userDataAppend executed at $(date)" >> /tmp/userdata-append-e2e-marker
apt-get update && apt-get install -y jq curl
echo "USERDATA_APPEND_E2E_TEST_END" | tee -a /var/log/userdata-append-e2e.log
`}[0]

	// Add test-specific tags
	nodeClass.Spec.Tags = map[string]string{
		"test-name":    testName,
		"test-type":    "userdata-append",
		"created-by":   "e2e-test",
		"feature-test": "userdata-append",
	}

	// Create the NodeClass
	err := suite.Client.Create(ctx, nodeClass)
	require.NoError(t, err, "Failed to create NodeClass")
	suite.AddCleanupResource(nodeClass)

	// Wait for NodeClass to be ready
	suite.waitForNodeClassReady(t, nodeClass.Name)

	// Create NodePool
	nodePool := suite.createTestNodePool(t, testName, nodeClass.Name)

	// Add specific node selector and taint for this test
	nodePool.Spec.Template.ObjectMeta.Labels = map[string]string{
		"test-type": "userdata-append-e2e",
	}
	nodePool.Spec.Template.Spec.Taints = []corev1.Taint{
		{
			Key:    "userdata-append-e2e",
			Value:  "true",
			Effect: corev1.TaintEffectNoSchedule,
		},
	}

	// Update the NodePool
	err = suite.Client.Update(ctx, nodePool)
	require.NoError(t, err, "Failed to update NodePool")

	// Create a test pod that will trigger node provisioning
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName + "-pod",
			Namespace: "default",
			Labels: map[string]string{
				"test-name": testName,
				"test-type": "userdata-append-e2e",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"test-type": "userdata-append-e2e",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "userdata-append-e2e",
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
					Operator: corev1.TolerationOpEqual,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
		},
	}

	// Create the test pod
	err = suite.Client.Create(ctx, pod)
	require.NoError(t, err, "Failed to create test pod")
	suite.AddCleanupResource(pod)

	t.Logf("Waiting for pod %s to be scheduled and running", pod.Name)

	// Wait for pod to be scheduled and running
	suite.waitForPodRunning(t, pod.Name, "default", 10*time.Minute)

	// Get the node that was created
	err = suite.Client.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	require.NoError(t, err, "Failed to get pod")
	require.NotEmpty(t, pod.Spec.NodeName, "Pod should be scheduled to a node")

	nodeName := pod.Spec.NodeName
	t.Logf("Pod scheduled to node: %s", nodeName)

	// Verify the node has the correct labels
	node := &corev1.Node{}
	err = suite.Client.Get(ctx, client.ObjectKey{Name: nodeName}, node)
	require.NoError(t, err, "Failed to get node")

	require.Equal(t, "userdata-append-e2e", node.Labels["test-type"], "Node should have correct test-type label")
	require.Equal(t, nodeClass.Name, node.Labels["karpenter.ibm.sh/ibmnodeclass"], "Node should have correct nodeclass label")

	t.Logf("Node %s has correct labels, creating verification pod", nodeName)

	// Create a verification pod that will check for userDataAppend execution
	verificationPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName + "-verify",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName, // Force to run on the same node
			HostPID:       true,
			HostNetwork:   true,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "verify",
					Image:   "ubuntu:22.04",
					Command: []string{"/bin/bash"},
					Args: []string{
						"-c",
						`
						set -e
						echo "=== Verifying userDataAppend execution on node ==="

						# Check for marker file
						if [ -f /host/tmp/userdata-append-e2e-marker ]; then
							echo "SUCCESS: E2E marker file found"
							echo "Marker content:"
							cat /host/tmp/userdata-append-e2e-marker
						else
							echo "FAIL: E2E marker file not found"
							exit 1
						fi

						# Check for log file
						if [ -f /host/var/log/userdata-append-e2e.log ]; then
							echo "SUCCESS: E2E log file found"
							echo "Log content:"
							cat /host/var/log/userdata-append-e2e.log
						else
							echo "FAIL: E2E log file not found"
							exit 1
						fi

						# Check if jq was installed by userDataAppend
						if chroot /host which jq > /dev/null; then
							echo "SUCCESS: jq is installed (by userDataAppend)"
							echo "jq version: $(chroot /host jq --version)"
						else
							echo "FAIL: jq not installed (userDataAppend did not execute)"
							exit 1
						fi

						# Check if curl was installed by userDataAppend
						if chroot /host which curl > /dev/null; then
							echo "SUCCESS: curl is installed (by userDataAppend)"
						else
							echo "FAIL: curl not installed (userDataAppend did not execute)"
							exit 1
						fi

						echo "=== All userDataAppend verification checks passed! ==="
						`,
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &[]bool{true}[0],
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "host-root",
							MountPath: "/host",
							ReadOnly:  true,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "host-root",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
						},
					},
				},
			},
		},
	}

	// Create verification pod
	err = suite.Client.Create(ctx, verificationPod)
	require.NoError(t, err, "Failed to create verification pod")
	suite.AddCleanupResource(verificationPod)

	t.Logf("Waiting for verification pod %s to complete", verificationPod.Name)

	// Wait for verification pod to complete successfully
	suite.waitForPodCompletion(t, verificationPod.Name, "default", 5*time.Minute)

	// Verify that the verification pod succeeded
	err = suite.Client.Get(ctx, client.ObjectKeyFromObject(verificationPod), verificationPod)
	require.NoError(t, err, "Failed to get verification pod")
	require.Equal(t, corev1.PodSucceeded, verificationPod.Status.Phase, "Verification pod should have succeeded")

	t.Logf("✅ UserDataAppend E2E test completed successfully!")
}

// TestE2EStandardBootstrap tests that standard bootstrap works without userData or userDataAppend
func TestE2EStandardBootstrap(t *testing.T) {
	suite := SetupE2ETestSuite(t)
	testName := fmt.Sprintf("standard-bootstrap-%d", time.Now().Unix())
	t.Logf("Starting standard bootstrap test: %s", testName)
	ctx := context.Background()

	// Create standard NodeClass without any custom user data
	nodeClass := suite.createTestNodeClass(t, testName)

	// Ensure no userData or userDataAppend is set
	require.Nil(t, nodeClass.Spec.UserData, "UserData should be nil for standard bootstrap")
	require.Nil(t, nodeClass.Spec.UserDataAppend, "UserDataAppend should be nil for standard bootstrap")

	// Wait for NodeClass to be ready
	suite.waitForNodeClassReady(t, nodeClass.Name)

	// Create NodePool
	nodePool := suite.createTestNodePool(t, testName, nodeClass.Name)

	// Add specific labels for this test
	nodePool.Spec.Template.ObjectMeta.Labels = map[string]string{
		"test-type": "standard-bootstrap-e2e",
	}

	// Update the NodePool
	err := suite.Client.Update(ctx, nodePool)
	require.NoError(t, err, "Failed to update NodePool")

	// Create test pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName + "-pod",
			Namespace: "default",
			Labels: map[string]string{
				"test-name": testName,
				"test-type": "standard-bootstrap-e2e",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"test-type": "standard-bootstrap-e2e",
			},
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
		},
	}

	// Create the test pod
	err = suite.Client.Create(ctx, pod)
	require.NoError(t, err, "Failed to create test pod")
	suite.AddCleanupResource(pod)

	t.Logf("Waiting for pod %s to be scheduled and running", pod.Name)

	// Wait for pod to be running
	suite.waitForPodRunning(t, pod.Name, "default", 10*time.Minute)

	// Get the node and verify it joined successfully
	err = suite.Client.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	require.NoError(t, err, "Failed to get pod")
	require.NotEmpty(t, pod.Spec.NodeName, "Pod should be scheduled to a node")

	node := &corev1.Node{}
	err = suite.Client.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node)
	require.NoError(t, err, "Failed to get node")

	// Verify node has correct labels indicating successful bootstrap
	require.Equal(t, "standard-bootstrap-e2e", node.Labels["test-type"], "Node should have correct test-type label")
	require.Equal(t, "true", node.Labels["karpenter.sh/initialized"], "Node should be initialized")
	require.Equal(t, nodeClass.Name, node.Labels["karpenter.ibm.sh/ibmnodeclass"], "Node should have correct nodeclass label")

	t.Logf("✅ Standard bootstrap E2E test completed successfully!")
}
