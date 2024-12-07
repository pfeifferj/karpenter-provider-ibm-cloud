package operator

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	instancetypecontroller "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/instancetype"
	interruptioncontroller "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/interruption"
)

type Operator struct {
	kubeClient          client.Client
	kubeClientSet       *kubernetes.Clientset
	unavailableOfferings *cache.UnavailableOfferings
	recorder            record.EventRecorder
}

func NewOperator(ctx context.Context, config *rest.Config) (*Operator, error) {
	kubeClientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	kubeClient, err := client.New(config, client.Options{})
	if err != nil {
		return nil, err
	}

	// Create event broadcaster
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClientSet.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "karpenter-ibm-cloud"})

	unavailableOfferings := cache.NewUnavailableOfferings()

	return &Operator{
		kubeClient:           kubeClient,
		kubeClientSet:        kubeClientSet,
		unavailableOfferings: unavailableOfferings,
		recorder:             recorder,
	}, nil
}

// GetClient returns the kubernetes client
func (o *Operator) GetClient() client.Client {
	return o.kubeClient
}

// GetEventRecorder returns the event recorder
func (o *Operator) GetEventRecorder() record.EventRecorder {
	return o.recorder
}

// GetUnavailableOfferings returns the unavailable offerings cache
func (o *Operator) GetUnavailableOfferings() *cache.UnavailableOfferings {
	return o.unavailableOfferings
}

func (o *Operator) WithControllers() []controller.Controller {
	instanceTypeCtrl, err := instancetypecontroller.NewController()
	if err != nil {
		// Since we can't return an error from this method, we'll panic
		// This is consistent with how controller-runtime handles initialization errors
		panic(err)
	}

	return []controller.Controller{
		instanceTypeCtrl,
		interruptioncontroller.NewController(o.kubeClient, o.recorder, o.unavailableOfferings),
	}
}

func (o *Operator) WithWebhooks() []manager.Runnable {
	return []manager.Runnable{}
}

func (o *Operator) WithCustomResourceDefinitions() []client.Object {
	return []client.Object{}
}

func (o *Operator) Cleanup(ctx context.Context) error {
	// Perform any necessary cleanup when the operator is shutting down
	return nil
}

func (o *Operator) Name() string {
	return "karpenter-ibm-cloud"
}

func (o *Operator) Ready() bool {
	return true
}

func (o *Operator) LivenessProbe() error {
	return nil
}
