package autoplacement

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
)

func TestUpdateCondition(t *testing.T) {
	// Create a fake client
	client := fake.NewClientBuilder().Build()

	// Create a test controller
	controller := &Controller{
		client: client,
		log:    zap.New(),
	}

	// Create a test nodeclass
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Status: v1alpha1.IBMNodeClassStatus{
			Conditions: []metav1.Condition{},
		},
	}

	// Test adding a new condition
	controller.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionTrue, "TestReason", "Test message")

	// Verify the condition was added correctly
	assert.Len(t, nodeClass.Status.Conditions, 1)
	condition := nodeClass.Status.Conditions[0]
	assert.Equal(t, ConditionTypeAutoPlacement, condition.Type)
	assert.Equal(t, metav1.ConditionTrue, condition.Status)
	assert.Equal(t, "TestReason", condition.Reason)
	assert.Equal(t, "Test message", condition.Message)
	assert.NotNil(t, condition.LastTransitionTime)

	// Test updating an existing condition
	oldTime := condition.LastTransitionTime
	time.Sleep(time.Millisecond) // Ensure time difference
	controller.updateCondition(nodeClass, ConditionTypeAutoPlacement, metav1.ConditionFalse, "NewReason", "New message")

	// Verify the condition was updated correctly
	assert.Len(t, nodeClass.Status.Conditions, 1)
	condition = nodeClass.Status.Conditions[0]
	assert.Equal(t, ConditionTypeAutoPlacement, condition.Type)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	assert.Equal(t, "NewReason", condition.Reason)
	assert.Equal(t, "New message", condition.Message)
	assert.NotEqual(t, oldTime, condition.LastTransitionTime)
}

func TestReconcile(t *testing.T) {
	// Create a fake client
	client := fake.NewClientBuilder().Build()

	// Create a test controller
	controller := &Controller{
		client: client,
		log:    zap.New(),
	}

	// Create a test nodeclass
	nodeClass := &v1alpha1.IBMNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclass",
		},
		Spec: v1alpha1.IBMNodeClassSpec{
			Region: "us-south",
			VPC:    "test-vpc",
			Image:  "test-image",
			InstanceRequirements: &v1alpha1.InstanceTypeRequirements{
				MinimumCPU:    1,
				MinimumMemory: 2,
			},
		},
	}

	// Create the nodeclass in the fake client
	err := client.Create(context.Background(), nodeClass)
	assert.NoError(t, err)

	// Test reconciliation
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: "test-nodeclass",
		},
	}

	// Perform reconciliation
	result, err := controller.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Get the updated nodeclass
	err = client.Get(context.Background(), req.NamespacedName, nodeClass)
	assert.NoError(t, err)

	// Verify conditions were set correctly
	assert.NotEmpty(t, nodeClass.Status.Conditions)
	var autoPlacementCondition *metav1.Condition
	for i := range nodeClass.Status.Conditions {
		if nodeClass.Status.Conditions[i].Type == ConditionTypeAutoPlacement {
			cond := nodeClass.Status.Conditions[i]
			autoPlacementCondition = &cond
			break
		}
	}
	assert.NotNil(t, autoPlacementCondition)
	assert.Equal(t, metav1.ConditionFalse, autoPlacementCondition.Status)
	assert.Equal(t, "InstanceTypeSelectionFailed", autoPlacementCondition.Reason)
}
