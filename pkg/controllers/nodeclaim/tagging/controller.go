package tagging

import (
	"context"
	"strings"

	"github.com/awslabs/operatorpkg/singleton"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/providers/instance"
)

// Controller reconciles NodeClaim objects to ensure proper tagging of IBM Cloud instances
//+kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
type Controller struct {
	kubeClient       client.Client
	instanceProvider *instance.IBMCloudInstanceProvider
}

// NewController constructs a controller instance
func NewController(kubeClient client.Client, instanceProvider instance.Provider) *Controller {
	return &Controller{
		kubeClient:       kubeClient,
		instanceProvider: instanceProvider.(*instance.IBMCloudInstanceProvider),
	}
}

// Reconcile executes a control loop for the resource
func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	// List all NodeClaims
	nodeClaimList := &v1beta1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, nodeClaimList); err != nil {
		return reconcile.Result{}, err
	}

	for _, nodeClaim := range nodeClaimList.Items {
		// Skip if node name is not set
		if nodeClaim.Status.NodeName == "" {
			continue
		}

		// Get the node
		node := &v1.Node{}
		if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeClaim.Status.NodeName}, node); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			return reconcile.Result{}, err
		}

		// Extract instance ID from provider ID
		if node.Spec.ProviderID == "" {
			continue
		}
		instanceID := strings.TrimPrefix(node.Spec.ProviderID, "ibm://")

		// Build tags map
		tags := map[string]string{
			"karpenter.ibm.sh/nodeclaim": nodeClaim.Name,
			"karpenter.ibm.sh/nodepool":  nodeClaim.Labels["karpenter.sh/nodepool"],
		}

		// Add custom tags from nodeclaim requirements
		for _, req := range nodeClaim.Spec.Requirements {
			if req.Key == "karpenter.ibm.sh/tags" {
				for _, value := range req.Values {
					parts := strings.SplitN(value, "=", 2)
					if len(parts) == 2 {
						tags[parts[0]] = parts[1]
					}
				}
			}
		}

		// Update instance tags using the instance provider
		if err := c.instanceProvider.TagInstance(ctx, instanceID, tags); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// Name returns the name of the controller
func (c *Controller) Name() string {
	return "nodeclaim.tagging"
}

// Register registers the controller with the manager
func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return builder.ControllerManagedBy(m).
		For(&v1beta1.NodeClaim{}).
		Owns(&v1.Node{}).
		Complete(singleton.AsReconciler(c))
}
