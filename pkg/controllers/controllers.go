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
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"

	// Core Karpenter Controllers
	corecontrollers "sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/cloudprovider/metrics"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/interruption"
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

	// Add metrics decoration to cloud provider (following AWS pattern)
	decoratedCloudProvider := metrics.Decorate(cloudProvider)

	// Create cluster state manager (required by core controllers)
	clusterState := state.NewCluster(clk, kubeClient, decoratedCloudProvider)

	// Register core Karpenter controllers (replaces simplified NodePool/NodeClaim controllers)
	coreControllerList := corecontrollers.NewControllers(
		ctx,
		mgr,
		clk,
		kubeClient,
		recorder,
		decoratedCloudProvider,
		clusterState,
	)

	// Register each core controller
	for _, ctrl := range coreControllerList {
		if err := ctrl.Register(ctx, mgr); err != nil {
			return fmt.Errorf("registering core controller: %w", err)
		}
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
