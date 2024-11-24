package operator

import (
	"context"

	"github.com/awslabs/operatorpkg/controller"
	"github.com/samber/lo"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/karpenter/pkg/events"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cache"
	instancetypecontroller "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/providers/instancetype"
	interruptioncontroller "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/controllers/interruption"
	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instancetype"
)

type Operator struct {
	kubeClient          client.Client
	kubeClientSet       *kubernetes.Clientset
	instanceTypeProvider instancetype.Provider
	unavailableOfferings *cache.UnavailableOfferings
	recorder            events.Recorder
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

	recorder := events.NewRecorder(kubeClient, "karpenter-ibm-cloud")
	unavailableOfferings := cache.NewUnavailableOfferings()
	instanceTypeProvider := instancetype.NewProvider()

	return &Operator{
		kubeClient:           kubeClient,
		kubeClientSet:        kubeClientSet,
		instanceTypeProvider: instanceTypeProvider,
		unavailableOfferings: unavailableOfferings,
		recorder:             recorder,
	}, nil
}

func (o *Operator) WithControllers() []controller.Controller {
	return []controller.Controller{
		instancetypecontroller.NewController(o.instanceTypeProvider),
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
