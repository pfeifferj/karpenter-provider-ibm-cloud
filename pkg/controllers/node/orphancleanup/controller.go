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

package orphancleanup

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/globaltaggingv1"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/awslabs/operatorpkg/reconciler"
	"github.com/awslabs/operatorpkg/singleton"
	"github.com/go-logr/logr"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/operator/injection"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/httpclient"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/ownership"
)

type GlobalTaggingAPI interface {
	ListTagsWithContext(context.Context, *globaltaggingv1.ListTagsOptions) (*globaltaggingv1.TagList, *core.DetailedResponse, error)
}

type vpcClientProvider interface {
	GetVPCClient(context.Context) (*ibm.VPCClient, error)
}

type Controller struct {
	kubeClient    client.Client
	apiReader     client.Reader
	ibmClient     vpcClientProvider
	globalTagging GlobalTaggingAPI
	orphanTimeout time.Duration
}

const (
	DefaultOrphanTimeout = 10 * time.Minute
	OrphanCheckInterval  = 5 * time.Minute
	MinimumOrphanTimeout = 5 * time.Minute
)

func NewController(kubeClient client.Client, ibmClient *ibm.Client, readers ...client.Reader) *Controller {
	reader := client.Reader(kubeClient)
	if len(readers) > 0 && readers[0] != nil {
		reader = readers[0]
	}
	controller := &Controller{kubeClient: kubeClient, apiReader: reader, orphanTimeout: getOrphanTimeoutFromEnv()}
	if ibmClient != nil {
		controller.ibmClient = ibmClient
	}
	if isOrphanCleanupEnabled() {
		if apiKey := os.Getenv("IBMCLOUD_API_KEY"); apiKey != "" {
			tagging, err := globaltaggingv1.NewGlobalTaggingV1(&globaltaggingv1.GlobalTaggingV1Options{
				Authenticator: ibm.NewIAMAuthenticator(apiKey),
			})
			if err == nil {
				tagging.Service.SetHTTPClient(httpclient.InstrumentHTTPClient(tagging.Service.GetHTTPClient(), "global"))
				controller.globalTagging = tagging
			}
		}
	}
	return controller
}

func (c *Controller) reader() client.Reader {
	if c.apiReader != nil {
		return c.apiReader
	}
	return c.kubeClient
}

func getOrphanTimeoutFromEnv() time.Duration {
	if value := os.Getenv("KARPENTER_ORPHAN_TIMEOUT_MINUTES"); value != "" {
		if minutes, err := strconv.Atoi(value); err == nil && minutes > 0 {
			timeout := time.Duration(minutes) * time.Minute
			if timeout < MinimumOrphanTimeout {
				return MinimumOrphanTimeout
			}
			return timeout
		}
	}
	return DefaultOrphanTimeout
}

func isOrphanCleanupEnabled() bool {
	return os.Getenv("KARPENTER_ENABLE_ORPHAN_CLEANUP") == "true"
}

func (c *Controller) Reconcile(ctx context.Context) (reconciler.Result, error) {
	ctx = injection.WithControllerName(ctx, "node.orphancleanup.ibm")
	result := reconciler.Result{RequeueAfter: OrphanCheckInterval}
	if !isOrphanCleanupEnabled() || c.ibmClient == nil || c.globalTagging == nil {
		return result, nil
	}
	if _, err := ownership.ClusterUID(ctx, c.reader()); err != nil {
		return reconciler.Result{}, err
	}
	instances, err := c.getAllVPCInstances(ctx)
	if err != nil {
		return reconciler.Result{}, err
	}
	nodes := &corev1.NodeList{}
	if err := c.reader().List(ctx, nodes); err != nil {
		return reconciler.Result{}, err
	}
	claims := &karpv1.NodeClaimList{}
	if err := c.reader().List(ctx, claims); err != nil {
		return reconciler.Result{}, err
	}
	expected := map[string]bool{}
	for _, node := range nodes.Items {
		expected[c.extractInstanceIDFromProviderID(node.Spec.ProviderID)] = true
	}
	for _, claim := range claims.Items {
		expected[c.extractInstanceIDFromProviderID(claim.Status.ProviderID)] = true
	}
	var errs []error
	for id, instance := range instances {
		if expected[id] || instance.CreatedAt == nil || instance.CreatedAt.IsZero() || time.Since(time.Time(*instance.CreatedAt)) < c.orphanTimeout {
			continue
		}
		tags, err := c.instanceTags(ctx, instance.CRN)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		owned, err := c.ownsTags(ctx, tags)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !owned {
			continue
		}
		if err := c.processOrphanedInstance(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return result, multierr.Combine(errs...)
}

func (c *Controller) extractInstanceIDFromProviderID(providerID string) string {
	if !strings.HasPrefix(providerID, "ibm:///") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(providerID, "ibm:///"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[1]
}

func (c *Controller) getAllVPCInstances(ctx context.Context) (map[string]vpcv1.Instance, error) {
	vpcClient, err := c.ibmClient.GetVPCClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}
	instances, err := vpcClient.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]vpcv1.Instance, len(instances))
	for _, instance := range instances {
		if instance.ID != nil && *instance.ID != "" {
			result[*instance.ID] = instance
		}
	}
	return result, nil
}

func (c *Controller) instanceTags(ctx context.Context, crn *string) (map[string]string, error) {
	if crn == nil || *crn == "" || c.globalTagging == nil {
		return nil, nil
	}
	tags := map[string]string{}
	const limit int64 = 100
	for offset := int64(0); ; {
		results, _, err := c.globalTagging.ListTagsWithContext(ctx, &globaltaggingv1.ListTagsOptions{
			AttachedTo: crn,
			Providers:  []string{"ghost"},
			TagType:    core.StringPtr("user"),
			Offset:     core.Int64Ptr(offset),
			Limit:      core.Int64Ptr(limit),
		})
		if err != nil {
			return nil, fmt.Errorf("listing resource ownership tags: %w", err)
		}
		if results == nil {
			return nil, nil
		}
		for _, tag := range results.Items {
			if tag.Name == nil {
				continue
			}
			parts := strings.SplitN(*tag.Name, ":", 2)
			if len(parts) != 2 {
				continue
			}
			if existing, ok := tags[parts[0]]; ok && existing != parts[1] && ownership.ReservedTag(parts[0]) {
				return nil, fmt.Errorf("conflicting ownership values for %s", parts[0])
			}
			tags[parts[0]] = parts[1]
		}
		offset += int64(len(results.Items))
		if results.TotalCount != nil {
			if offset >= *results.TotalCount {
				return tags, nil
			}
			if len(results.Items) == 0 {
				return nil, fmt.Errorf("incomplete ownership tag listing")
			}
		} else if len(results.Items) < int(limit) {
			return tags, nil
		}
	}
}

func (c *Controller) ownsTags(ctx context.Context, tags map[string]string) (bool, error) {
	if tags[ownership.ManagedTag] != "true" || tags[ownership.ProviderTag] != "vpc" || tags[ownership.ClaimUIDTag] == "" || tags[ownership.NodeClassUIDTag] == "" {
		return false, nil
	}
	clusterUID, err := ownership.ClusterUID(ctx, c.reader())
	if err != nil {
		return false, err
	}
	return tags[ownership.ClusterUIDTag] == clusterUID, nil
}

func (c *Controller) hasKarpenterTags(ctx context.Context, crn *string, instanceID string, logger logr.Logger) bool {
	tags, err := c.instanceTags(ctx, crn)
	if err != nil {
		logger.Error(err, "Cannot prove instance ownership", "instance-id", instanceID)
		return false
	}
	owned, err := c.ownsTags(ctx, tags)
	if err != nil {
		logger.Error(err, "Cannot determine cluster identity", "instance-id", instanceID)
	}
	return owned && err == nil
}

func (c *Controller) processOrphanedInstance(ctx context.Context, instanceID string) error {
	vpcClient, err := c.ibmClient.GetVPCClient(ctx)
	if err != nil {
		return err
	}
	instance, err := vpcClient.GetInstance(ctx, instanceID)
	if ibm.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("instance lookup returned no instance")
	}
	if instance.ID == nil || *instance.ID != instanceID || instance.CreatedAt == nil || instance.CreatedAt.IsZero() || time.Since(time.Time(*instance.CreatedAt)) < c.orphanTimeout {
		return nil
	}
	tags, err := c.instanceTags(ctx, instance.CRN)
	if err != nil {
		return err
	}
	owned, err := c.ownsTags(ctx, tags)
	if err != nil || !owned {
		return err
	}
	claims := &karpv1.NodeClaimList{}
	if err := c.reader().List(ctx, claims); err != nil {
		return err
	}
	for _, claim := range claims.Items {
		if string(claim.UID) == tags[ownership.ClaimUIDTag] || c.extractInstanceIDFromProviderID(claim.Status.ProviderID) == instanceID {
			return nil
		}
	}
	nodes := &corev1.NodeList{}
	if err := c.reader().List(ctx, nodes); err != nil {
		return err
	}
	for _, node := range nodes.Items {
		if c.extractInstanceIDFromProviderID(node.Spec.ProviderID) == instanceID {
			return nil
		}
	}
	if err := vpcClient.DeleteInstance(ctx, instanceID); err != nil && !ibm.IsNotFound(err) {
		return err
	}
	log.FromContext(ctx).Info("Deleted orphaned instance", "instance-id", instanceID)
	return nil
}

func (c *Controller) processOrphanedNode(ctx context.Context, candidate corev1.Node) error {
	if !c.isNodeManagedByKarpenter(candidate) || !c.isNodeOrphanedLongEnough(candidate) {
		return nil
	}
	id := c.extractInstanceIDFromProviderID(candidate.Spec.ProviderID)
	if id == "" || c.ibmClient == nil {
		return nil
	}
	vpcClient, err := c.ibmClient.GetVPCClient(ctx)
	if err != nil {
		return err
	}
	if _, err := vpcClient.GetInstance(ctx, id); !ibm.IsNotFound(err) {
		return err
	}
	node := &corev1.Node{}
	if err := c.reader().Get(ctx, client.ObjectKeyFromObject(&candidate), node); err != nil {
		return client.IgnoreNotFound(err)
	}
	if node.UID != candidate.UID || node.Spec.ProviderID != candidate.Spec.ProviderID || !node.DeletionTimestamp.IsZero() || !c.isNodeManagedByKarpenter(*node) || !c.isNodeOrphanedLongEnough(*node) {
		return nil
	}
	if _, err := vpcClient.GetInstance(ctx, id); !ibm.IsNotFound(err) {
		return err
	}
	uid, version := node.UID, node.ResourceVersion
	return client.IgnoreNotFound(c.kubeClient.Delete(ctx, node, &client.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version},
	}))
}

func (c *Controller) isNodeManagedByKarpenter(node corev1.Node) bool {
	_, hasPool := node.Labels[karpv1.NodePoolLabelKey]
	_, hasClass := node.Labels["karpenter-ibm.sh/ibmnodeclass"]
	return hasPool || hasClass
}

func (c *Controller) isNodeOrphanedLongEnough(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status != corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() && time.Since(condition.LastTransitionTime.Time) >= c.orphanTimeout
		}
	}
	return !node.CreationTimestamp.IsZero() && time.Since(node.CreationTimestamp.Time) >= c.orphanTimeout
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("node.orphancleanup.ibm").
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
