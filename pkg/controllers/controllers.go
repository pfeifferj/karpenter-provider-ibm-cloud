package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// TODO: Implement NodePool reconciliation logic
	// This should watch for unschedulable pods and create NodeClaims
	return reconcile.Result{}, nil
}

// NodeClaimReconciler handles complete NodeClaim lifecycle
type NodeClaimReconciler struct {
	kubeClient    client.Client
	cloudProvider cloudprovider.CloudProvider
}

func (r *NodeClaimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// TODO: Implement NodeClaim reconciliation logic
	// This should handle the complete lifecycle from creation to deletion
	return reconcile.Result{}, nil
}
