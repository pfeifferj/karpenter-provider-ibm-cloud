package controllers

import (
	"context"
	"fmt"

	"github.com/awslabs/operatorpkg/controller"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"

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

func NewControllers(ctx context.Context, mgr manager.Manager, clk clock.Clock,
	kubeClient client.Client, recorder events.Recorder,
	unavailableOfferings *cache.UnavailableOfferings,
	cloudProvider cloudprovider.CloudProvider,
	instanceProvider instance.Provider, instanceTypeProvider instancetype.Provider,
	_ interface{}) []controller.Controller {

	controllers := make([]controller.Controller, 0)

	// Create event recorder adapter
	recorderAdapter := &RecorderAdapter{recorder}

	// Add nodeclass hash controller
	if hashCtrl, err := nodeclasshash.NewController(kubeClient); err == nil {
		controllers = append(controllers, hashCtrl)
	}

	// Add nodeclass status controller
	if statusCtrl, err := nodeclaasstatus.NewController(kubeClient); err == nil {
		controllers = append(controllers, statusCtrl)
	}

	// Add nodeclass termination controller
	if terminationCtrl, err := nodeclasstermination.NewController(kubeClient, recorderAdapter); err == nil {
		controllers = append(controllers, terminationCtrl)
	}

	// Add pricing controller
	if pricingCtrl, err := controllerspricing.NewController(nil); err == nil {
		controllers = append(controllers, pricingCtrl)
	}

	// Add garbage collection controller
	controllers = append(controllers, nodeclaimgarbagecollection.NewController(kubeClient, cloudProvider))

	// Add tagging controller
	controllers = append(controllers, nodeclaimtagging.NewController(kubeClient, instanceProvider))

	// Add instance type controller
	if instanceTypeCtrl, err := providersinstancetype.NewController(); err == nil {
		controllers = append(controllers, instanceTypeCtrl)
	}

	// Add interruption controller if enabled
	if options.FromContext(ctx).Interruption {
		controllers = append(controllers, interruption.NewController(kubeClient, recorderAdapter, unavailableOfferings))
	}

	return controllers
}
