package controllers

//+kubebuilder:rbac:groups=karpenter.sh,resources=nodepools,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=karpenter.sh,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;create;delete;update;patch
//+kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=karpenter.ibm.sh,resources=ibmnodeclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=karpenter.ibm.sh,resources=ibmnodeclasses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;create;delete;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/interruption"
	nodeclaimgarbagecollection "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclaim/garbagecollection"
	nodeclaimtagging "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclaim/tagging"
	nodeclasshash "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/hash"
	nodeclaasstatus "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/status"
	nodeclasstermination "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/nodeclass/termination"
	providersinstancetype "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/instancetype"
	controllerspricing "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/pricing"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/operator/options"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
)

// RecorderAdapter adapts between events.Recorder and record.EventRecorder
type RecorderAdapter struct {
	events.Recorder
}

// Event implements record.EventRecorder
func (r *RecorderAdapter) Event(object runtime.Object, eventtype, reason, message string) {
	r.Publish(events.Event{
		InvolvedObject: object,
		Type:           eventtype,
		Reason:         reason,
		Message:        message,
	})
}

// Eventf implements record.EventRecorder
func (r *RecorderAdapter) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	r.Publish(events.Event{
		InvolvedObject: object,
		Type:           eventtype,
		Reason:         reason,
		Message:        fmt.Sprintf(messageFmt, args...),
	})
}

// AnnotatedEventf implements record.EventRecorder
func (r *RecorderAdapter) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	r.Publish(events.Event{
		InvolvedObject: object,
		Type:           eventtype,
		Reason:         reason,
		Message:        fmt.Sprintf(messageFmt, args...),
	})
}

func RegisterControllers(ctx context.Context, mgr manager.Manager, clk clock.Clock,
	kubeClient client.Client, recorder events.Recorder,
	unavailableOfferings *cache.UnavailableOfferings,
	cloudProvider cloudprovider.CloudProvider,
	instanceProvider instance.Provider, instanceTypeProvider instancetype.Provider) error {

	// Create event recorder adapter
	recorderAdapter := &RecorderAdapter{recorder}

	// Register core Karpenter controllers
	// NodePool controller - essential for detecting unschedulable pods and creating NodeClaims
	if err := controllerruntime.NewControllerManagedBy(mgr).
		Named("nodepool").
		For(&v1.NodePool{}).
		Owns(&v1.NodeClaim{}).
		Complete(&NodePoolReconciler{
			kubeClient:    kubeClient,
			cloudProvider: cloudProvider,
		}); err != nil {
		return fmt.Errorf("registering nodepool controller: %w", err)
	}

	// NodeClaim controller - handles complete NodeClaim lifecycle
	if err := controllerruntime.NewControllerManagedBy(mgr).
		Named("nodeclaim").
		For(&v1.NodeClaim{}).
		Complete(&NodeClaimReconciler{
			kubeClient:    kubeClient,
			cloudProvider: cloudProvider,
		}); err != nil {
		return fmt.Errorf("registering nodeclaim controller: %w", err)
	}

	// Register IBMNodeClass hash controller
	if hashCtrl, err := nodeclasshash.NewController(kubeClient); err != nil {
		return fmt.Errorf("creating nodeclass hash controller: %w", err)
	} else if err := hashCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering nodeclass hash controller: %w", err)
	}

	// Register IBMNodeClass status controller
	if statusCtrl, err := nodeclaasstatus.NewController(kubeClient); err != nil {
		return fmt.Errorf("creating nodeclass status controller: %w", err)
	} else if err := statusCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering nodeclass status controller: %w", err)
	}

	// Register IBMNodeClass termination controller
	if terminationCtrl, err := nodeclasstermination.NewController(kubeClient, recorderAdapter); err != nil {
		return fmt.Errorf("creating nodeclass termination controller: %w", err)
	} else if err := terminationCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering nodeclass termination controller: %w", err)
	}

	// Register pricing controller
	if pricingCtrl, err := controllerspricing.NewController(nil); err != nil {
		return fmt.Errorf("creating pricing controller: %w", err)
	} else if err := pricingCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering pricing controller: %w", err)
	}

	// Register garbage collection controller
	garbageCollectionCtrl := nodeclaimgarbagecollection.NewController(kubeClient, cloudProvider)
	if err := garbageCollectionCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering garbage collection controller: %w", err)
	}

	// Register tagging controller
	taggingCtrl := nodeclaimtagging.NewController(kubeClient, instanceProvider)
	if err := taggingCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering tagging controller: %w", err)
	}

	// Register instance type controller
	if instanceTypeCtrl, err := providersinstancetype.NewController(); err != nil {
		return fmt.Errorf("creating instance type controller: %w", err)
	} else if err := instanceTypeCtrl.Register(ctx, mgr); err != nil {
		return fmt.Errorf("registering instance type controller: %w", err)
	}

	// Register interruption controller if enabled
	if options.FromContext(ctx).Interruption {
		interruptionCtrl := interruption.NewController(kubeClient, recorderAdapter, unavailableOfferings)
		if err := interruptionCtrl.Register(ctx, mgr); err != nil {
			return fmt.Errorf("registering interruption controller: %w", err)
		}
	}

	return nil
}

// NodePoolReconciler handles NodePool lifecycle and creates NodeClaims for unschedulable pods
type NodePoolReconciler struct {
	kubeClient    client.Client
	cloudProvider cloudprovider.CloudProvider
}

func (r *NodePoolReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodepool", req.NamespacedName)
	logger.V(1).Info("Starting NodePool reconciliation")

	var nodePool v1.NodePool
	if err := r.kubeClient.Get(ctx, req.NamespacedName, &nodePool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Add finalizer for cleanup
	if !controllerutil.ContainsFinalizer(&nodePool, "karpenter.sh/finalizer") {
		logger.V(1).Info("Adding finalizer to NodePool")
		controllerutil.AddFinalizer(&nodePool, "karpenter.sh/finalizer")
		if err := r.kubeClient.Update(ctx, &nodePool); err != nil {
			return reconcile.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	// Handle deletion
	if !nodePool.DeletionTimestamp.IsZero() {
		logger.Info("Deleting NodePool")
		
		// Cleanup logic for NodePool resources
		if err := r.cleanupNodePoolResources(ctx, &nodePool); err != nil {
			return reconcile.Result{}, fmt.Errorf("cleaning up NodePool resources: %w", err)
		}
		
		controllerutil.RemoveFinalizer(&nodePool, "karpenter.sh/finalizer")
		if err := r.kubeClient.Update(ctx, &nodePool); err != nil {
			return reconcile.Result{}, fmt.Errorf("removing finalizer: %w", err)
		}
		return reconcile.Result{}, nil
	}

	// For now, the NodePool controller mainly manages the lifecycle of NodePools
	// The actual scheduling logic (creating NodeClaims for unschedulable pods) 
	// is typically handled by the core Karpenter provisioning controller
	logger.V(1).Info("NodePool reconciliation completed")
	return reconcile.Result{}, nil
}

// cleanupNodePoolResources handles cleanup of resources associated with a NodePool
func (r *NodePoolReconciler) cleanupNodePoolResources(ctx context.Context, nodePool *v1.NodePool) error {
	logger := log.FromContext(ctx).WithValues("nodepool", nodePool.Name)
	
	// Find all NodeClaims that belong to this NodePool
	// In Karpenter v1, NodeClaims are associated with NodePools via labels
	nodeClaims := &v1.NodeClaimList{}
	if err := r.kubeClient.List(ctx, nodeClaims, client.MatchingLabels{"karpenter.sh/nodepool": nodePool.Name}); err != nil {
		return fmt.Errorf("listing NodeClaims for NodePool %s: %w", nodePool.Name, err)
	}
	
	logger.Info("Found NodeClaims to cleanup", "count", len(nodeClaims.Items))
	
	// Delete each NodeClaim associated with this NodePool
	for _, nodeClaim := range nodeClaims.Items {
		logger.Info("Deleting NodeClaim", "nodeclaim", nodeClaim.Name)
		if err := r.kubeClient.Delete(ctx, &nodeClaim); err != nil {
			logger.Error(err, "Failed to delete NodeClaim", "nodeclaim", nodeClaim.Name)
			// Continue with other NodeClaims even if one fails
			continue
		}
	}
	
	// Wait for NodeClaims to be fully deleted before proceeding
	// This ensures that underlying cloud resources are cleaned up
	remaining := &v1.NodeClaimList{}
	if err := r.kubeClient.List(ctx, remaining, client.MatchingLabels{"karpenter.sh/nodepool": nodePool.Name}); err != nil {
		return fmt.Errorf("checking remaining NodeClaims: %w", err)
	}
	
	if len(remaining.Items) > 0 {
		logger.Info("NodeClaims still being deleted, will retry", "remaining", len(remaining.Items))
		return fmt.Errorf("waiting for %d NodeClaims to be deleted", len(remaining.Items))
	}
	
	logger.Info("Successfully cleaned up all NodePool resources")
	return nil
}

// NodeClaimReconciler handles complete NodeClaim lifecycle
type NodeClaimReconciler struct {
	kubeClient    client.Client
	cloudProvider cloudprovider.CloudProvider
}

func (r *NodeClaimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", req.NamespacedName)
	logger.V(1).Info("Starting NodeClaim reconciliation")

	var nodeClaim v1.NodeClaim
	if err := r.kubeClient.Get(ctx, req.NamespacedName, &nodeClaim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Processing NodeClaim",
		"name", nodeClaim.Name,
		"deletionTimestamp", nodeClaim.DeletionTimestamp,
		"providerID", nodeClaim.Status.ProviderID,
		"requirements", nodeClaim.Spec.Requirements)

	// Add finalizer for cleanup
	if !controllerutil.ContainsFinalizer(&nodeClaim, "karpenter.sh/finalizer") {
		logger.V(1).Info("Adding finalizer to NodeClaim")
		controllerutil.AddFinalizer(&nodeClaim, "karpenter.sh/finalizer")
		if err := r.kubeClient.Update(ctx, &nodeClaim); err != nil {
			return reconcile.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	// Handle NodeClaim provisioning or deletion
	if nodeClaim.DeletionTimestamp.IsZero() {
		if nodeClaim.Status.ProviderID == "" && r.cloudProvider != nil {
			logger.Info("Provisioning new node")
			// Provision new instance using cloud provider
			createdNodeClaim, err := r.cloudProvider.Create(ctx, &nodeClaim)
			if err != nil {
				logger.Error(err, "Failed to create node",
					"requirements", nodeClaim.Spec.Requirements,
					"labels", nodeClaim.Labels)
				return reconcile.Result{}, fmt.Errorf("creating node claim: %w", err)
			}
			logger.Info("Successfully created node",
				"providerID", createdNodeClaim.Status.ProviderID,
				"requirements", nodeClaim.Spec.Requirements)

			nodeClaim.Status = createdNodeClaim.Status
			if err := r.kubeClient.Status().Update(ctx, &nodeClaim); err != nil {
				logger.Error(err, "Failed to update NodeClaim status")
				return reconcile.Result{}, fmt.Errorf("updating status: %w", err)
			}
			logger.V(1).Info("Successfully updated NodeClaim status")
		} else {
			logger.V(1).Info("Node already exists", "providerID", nodeClaim.Status.ProviderID)
		}
	} else if r.cloudProvider != nil {
		logger.Info("Deleting node", "providerID", nodeClaim.Status.ProviderID)
		// Handle deletion
		if err := r.cloudProvider.Delete(ctx, &nodeClaim); err != nil {
			logger.Error(err, "Failed to delete node")
			return reconcile.Result{}, fmt.Errorf("deleting node claim: %w", err)
		}
		logger.Info("Successfully deleted node")

		controllerutil.RemoveFinalizer(&nodeClaim, "karpenter.sh/finalizer")
		if err := r.kubeClient.Update(ctx, &nodeClaim); err != nil {
			return reconcile.Result{}, fmt.Errorf("removing finalizer: %w", err)
		}
		logger.V(1).Info("Successfully removed finalizer")
	}

	logger.V(1).Info("Completed NodeClaim reconciliation")
	return reconcile.Result{}, nil
}
