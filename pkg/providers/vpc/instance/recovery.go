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

package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/IBM/vpc-go-sdk/vpcv1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/utils/ownership"
)

const (
	LaunchAnnotation      = "karpenter-ibm.sh/vpc-launch"
	LaunchFinalizer       = "karpenter-ibm.sh/vpc-launch"
	LaunchImageAnnotation = "karpenter-ibm.sh/launch-image-id"
)

type launchConfig struct {
	Name, ClusterUID, ClaimUID, ClassUID      string
	AccountID, ResourceGroup                  string
	Region, VPC, Profile, Zone, Subnet, Image string
	CapacityType, Hash, HashVersion           string
	SecurityGroups                            []string
	Submitted, Rejected                       bool
	Capacity, Allocatable                     corev1.ResourceList
}

func tagVolumeAttachments(boot *vpcv1.VolumeAttachmentPrototypeInstanceByImageContext, data []vpcv1.VolumeAttachmentPrototype, ownershipTags map[string]string) {
	tags := func(custom []string, retained bool) []string {
		result := make([]string, 0, len(custom)+len(ownershipTags)+1)
		for _, tag := range custom {
			key, _, _ := strings.Cut(tag, ":")
			if !ownership.ReservedTag(key) {
				result = append(result, tag)
			}
		}
		for key, value := range ownershipTags {
			result = append(result, key+":"+value)
		}
		if retained {
			result = append(result, "karpenter-ibm.sh/retain:true")
		}
		sort.Strings(result)
		return result
	}
	if boot != nil && boot.Volume != nil {
		boot.Volume.UserTags = tags(boot.Volume.UserTags, boot.DeleteVolumeOnInstanceDelete != nil && !*boot.DeleteVolumeOnInstanceDelete)
	}
	for _, attachment := range data {
		if volume, ok := attachment.Volume.(*vpcv1.VolumeAttachmentPrototypeVolumeVolumePrototypeInstanceContextVolumePrototypeInstanceContextVolumeByCapacity); ok {
			volume.UserTags = tags(volume.UserTags, attachment.DeleteVolumeOnInstanceDelete != nil && !*attachment.DeleteVolumeOnInstanceDelete)
		}
	}
}

func (p *VPCInstanceProvider) reader() client.Reader {
	if p.apiReader != nil {
		return p.apiReader
	}
	return p.kubeClient
}

func (p *VPCInstanceProvider) clientForRegion(ctx context.Context, region string) (*ibm.VPCClient, error) {
	vpc, err := p.vpcClientManager.GetVPCClient(ctx)
	if err != nil {
		return nil, err
	}
	return vpc.ForRegion(region)
}

func providerRegion(providerID string) string {
	parts := strings.Split(strings.TrimPrefix(providerID, "ibm:///"), "/")
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func decodeLaunch(value string) (*launchConfig, error) {
	config := &launchConfig{}
	if err := json.Unmarshal([]byte(value), config); err != nil {
		return nil, fmt.Errorf("invalid launch checkpoint: %w", err)
	}
	if config.ClaimUID == "" || config.ClusterUID == "" || config.Region == "" || config.Name != ownership.InstanceName(config.ClusterUID, config.ClaimUID) {
		return nil, fmt.Errorf("launch checkpoint has invalid ownership")
	}
	return config, nil
}

func (p *VPCInstanceProvider) ValidateLaunchTarget(ctx context.Context, claim *karpv1.NodeClaim) error {
	if claim == nil {
		return fmt.Errorf("NodeClaim launch checkpoint is required")
	}
	if claim.Annotations[LaunchAnnotation] == "" {
		vpc, err := p.clientForRegion(ctx, providerRegion(claim.Status.ProviderID))
		if err != nil {
			return err
		}
		return p.validateAccountTarget(ctx, vpc, claim.Annotations[ownership.AccountIDAnnotation])
	}
	config, err := decodeLaunch(claim.Annotations[LaunchAnnotation])
	if err != nil {
		return err
	}
	return p.validateLaunchTarget(ctx, claim, config)
}

func (p *VPCInstanceProvider) validateLaunchTarget(ctx context.Context, claim *karpv1.NodeClaim, config *launchConfig) error {
	if p.reader() == nil {
		return fmt.Errorf("kubernetes API reader is required for launch ownership")
	}
	clusterUID, err := ownership.ClusterUID(ctx, p.reader())
	if err != nil {
		return err
	}
	if config.ClaimUID != string(claim.UID) || config.ClusterUID != clusterUID {
		return fmt.Errorf("launch checkpoint belongs to another claim or cluster")
	}
	if claim.Status.ProviderID != "" && providerRegion(claim.Status.ProviderID) != config.Region {
		return fmt.Errorf("provider ID region does not match launch checkpoint")
	}
	vpc, err := p.clientForRegion(ctx, config.Region)
	if err != nil {
		return err
	}
	return p.validateAccountTarget(ctx, vpc, config.AccountID)
}

func (p *VPCInstanceProvider) resolveAccountID(ctx context.Context, vpc *ibm.VPCClient) (string, error) {
	var accountID string
	var err error
	if p.accountResolver != nil {
		accountID, err = p.accountResolver(ctx)
	} else {
		accountID, err = vpc.ResolveAccountID(ctx)
	}
	if err != nil {
		return "", err
	}
	if accountID == "" {
		return "", fmt.Errorf("VPC credential account could not be verified")
	}
	if configured := os.Getenv("IBM_ACCOUNT_ID"); configured != "" && configured != accountID {
		return "", fmt.Errorf("configured IBM account does not match the VPC credential account")
	}
	return accountID, nil
}

func (p *VPCInstanceProvider) validateAccountTarget(ctx context.Context, vpc *ibm.VPCClient, birthAccount string) error {
	if birthAccount == "" {
		return fmt.Errorf("VPC birth account is unproven; retaining allocation ownership")
	}
	accountID, err := p.resolveAccountID(ctx, vpc)
	if err != nil {
		return err
	}
	if accountID != birthAccount {
		return fmt.Errorf("VPC credential account differs from the immutable birth account; retaining ownership")
	}
	return nil
}

func verifyInstanceAccount(instance *vpcv1.Instance, accountID string) error {
	if instance.CRN == nil || *instance.CRN == "" {
		return nil
	}
	parts := strings.Split(*instance.CRN, ":")
	if len(parts) != 10 || parts[0] != "crn" || parts[6] != "a/"+accountID {
		return fmt.Errorf("instance CRN does not match the verified credential account")
	}
	return nil
}

func (p *VPCInstanceProvider) checkpointLaunch(ctx context.Context, claim *karpv1.NodeClaim, config *launchConfig) error {
	fresh := &karpv1.NodeClaim{}
	if err := p.reader().Get(ctx, client.ObjectKeyFromObject(claim), fresh); err != nil {
		return err
	}
	if fresh.UID != claim.UID || !fresh.DeletionTimestamp.IsZero() || fresh.Annotations[LaunchAnnotation] != "" {
		return fmt.Errorf("NodeClaim launch changed before submission")
	}
	if err := p.updateLaunch(ctx, fresh, config); err != nil {
		return err
	}
	claim.ResourceVersion = fresh.ResourceVersion
	claim.Annotations = fresh.Annotations
	claim.Finalizers = fresh.Finalizers
	return nil
}

func (p *VPCInstanceProvider) updateLaunch(ctx context.Context, claim *karpv1.NodeClaim, config *launchConfig) error {
	value, err := json.Marshal(config)
	if err != nil {
		return err
	}
	stored := claim.DeepCopy()
	if claim.Annotations == nil {
		claim.Annotations = map[string]string{}
	}
	claim.Annotations[LaunchAnnotation] = string(value)
	claim.Annotations[ownership.BackendAnnotation] = "vpc"
	claim.Annotations[ownership.RegionAnnotation] = config.Region
	controllerutil.AddFinalizer(claim, LaunchFinalizer)
	return p.kubeClient.Patch(ctx, claim, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{}))
}

func (p *VPCInstanceProvider) resetPreparedLaunch(ctx context.Context, claim *karpv1.NodeClaim) error {
	stored := claim.DeepCopy()
	delete(claim.Annotations, LaunchAnnotation)
	delete(claim.Annotations, ownership.BackendAnnotation)
	delete(claim.Annotations, ownership.RegionAnnotation)
	controllerutil.RemoveFinalizer(claim, LaunchFinalizer)
	return p.kubeClient.Patch(ctx, claim, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{}))
}

func rejectedCreate(err *ibm.IBMError) bool {
	return err != nil && err.StatusCode >= 400 && err.StatusCode < 500 && err.StatusCode != 408 && err.StatusCode != 409 && err.StatusCode != 429
}

func (p *VPCInstanceProvider) findLaunch(ctx context.Context, vpc *ibm.VPCClient, config *launchConfig) (*vpcv1.Instance, error) {
	instances, err := vpc.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	var found *vpcv1.Instance
	for _, instance := range instances {
		if instance.Name == nil || *instance.Name != config.Name {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple instances match launch identity")
		}
		if instance.ID == nil {
			return nil, fmt.Errorf("matching instance has no ID")
		}
		found, err = vpc.GetInstance(ctx, *instance.ID)
		if err != nil {
			return nil, err
		}
	}
	if found == nil {
		return nil, nil
	}
	if found.Name == nil || *found.Name != config.Name || found.Profile == nil || found.Profile.Name == nil || *found.Profile.Name != config.Profile || found.Zone == nil || found.Zone.Name == nil || *found.Zone.Name != config.Zone || found.VPC == nil || found.VPC.ID == nil || *found.VPC.ID != config.VPC || found.Image == nil || found.Image.ID == nil || *found.Image.ID != config.Image {
		return nil, fmt.Errorf("instance does not match its launch checkpoint")
	}
	subnet := ""
	if found.PrimaryNetworkAttachment != nil && found.PrimaryNetworkAttachment.Subnet != nil && found.PrimaryNetworkAttachment.Subnet.ID != nil {
		subnet = *found.PrimaryNetworkAttachment.Subnet.ID
	}
	if found.PrimaryNetworkInterface != nil && found.PrimaryNetworkInterface.Subnet != nil && found.PrimaryNetworkInterface.Subnet.ID != nil {
		subnet = *found.PrimaryNetworkInterface.Subnet.ID
	}
	if subnet != config.Subnet {
		return nil, fmt.Errorf("instance subnet does not match launch checkpoint")
	}
	if config.ResourceGroup != "" && (found.ResourceGroup == nil || found.ResourceGroup.ID == nil || *found.ResourceGroup.ID != config.ResourceGroup) {
		return nil, fmt.Errorf("instance resource group does not match launch checkpoint")
	}
	if err := verifyInstanceAccount(found, config.AccountID); err != nil {
		return nil, err
	}
	return found, nil
}

func (p *VPCInstanceProvider) recoverLaunch(ctx context.Context, claim *karpv1.NodeClaim, config *launchConfig) (*corev1.Node, error) {
	if err := p.validateLaunchTarget(ctx, claim, config); err != nil {
		return nil, err
	}
	vpc, err := p.clientForRegion(ctx, config.Region)
	if err != nil {
		return nil, err
	}
	instance, err := p.findLaunch(ctx, vpc, config)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("launch outcome for %s is not yet resolved", config.Name)
	}
	if instance.ID == nil || (instance.Status != nil && *instance.Status == vpcv1.InstanceStatusDeletingConst) {
		return nil, fmt.Errorf("launch instance is terminating")
	}
	if err := vpc.UpdateInstanceTags(ctx, *instance.ID, ownership.VPCTags(config.ClusterUID, config.ClaimUID, config.ClassUID)); err != nil {
		return nil, err
	}
	return config.node(claim, *instance.ID), nil
}

func (config *launchConfig) node(claim *karpv1.NodeClaim, instanceID string) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: claim.Name, Labels: map[string]string{
			ownership.ManagedTag: "true", corev1.LabelInstanceTypeStable: config.Profile, corev1.LabelTopologyZone: config.Zone, corev1.LabelTopologyRegion: config.Region, karpv1.CapacityTypeLabelKey: config.CapacityType, karpv1.NodePoolLabelKey: claim.Labels[karpv1.NodePoolLabelKey],
		}, Annotations: map[string]string{
			ownership.BackendAnnotation: "vpc", ownership.RegionAnnotation: config.Region,
			LaunchAnnotation: claim.Annotations[LaunchAnnotation], LaunchImageAnnotation: config.Image, v1alpha1.AnnotationIBMNodeClaimImageID: config.Image,
			v1alpha1.AnnotationIBMNodeClassHash: config.Hash, v1alpha1.AnnotationIBMNodeClassHashVersion: config.HashVersion,
			v1alpha1.AnnotationIBMNodeClaimSubnetID: config.Subnet, v1alpha1.AnnotationIBMNodeClaimSecurityGroups: strings.Join(config.SecurityGroups, ","),
		}},
		Spec:   corev1.NodeSpec{ProviderID: fmt.Sprintf("ibm:///%s/%s", config.Region, instanceID)},
		Status: corev1.NodeStatus{Capacity: config.Capacity.DeepCopy(), Allocatable: config.Allocatable.DeepCopy()},
	}
	if config.AccountID != "" {
		node.Annotations[ownership.AccountIDAnnotation] = config.AccountID
	}
	return node
}

// CleanupPending resolves a launch without a published provider ID before its finalizer can be removed.
func (p *VPCInstanceProvider) CleanupPending(ctx context.Context, claim *karpv1.NodeClaim) (bool, error) {
	config, err := decodeLaunch(claim.Annotations[LaunchAnnotation])
	if err != nil {
		return false, err
	}
	if targetErr := p.validateLaunchTarget(ctx, claim, config); targetErr != nil {
		return false, targetErr
	}
	vpc, err := p.clientForRegion(ctx, config.Region)
	if err != nil {
		return false, err
	}
	instance, err := p.findLaunch(ctx, vpc, config)
	if err != nil {
		return false, err
	}
	if instance == nil {
		return config.Rejected || !config.Submitted, nil
	}
	if instance.ID == nil {
		return false, fmt.Errorf("pending instance has no ID")
	}
	nodes := &corev1.NodeList{}
	if listErr := p.reader().List(ctx, nodes); listErr != nil {
		return false, listErr
	}
	providerID := fmt.Sprintf("ibm:///%s/%s", config.Region, *instance.ID)
	foundNode := false
	for _, node := range nodes.Items {
		if node.Spec.ProviderID != providerID {
			continue
		}
		foundNode = true
		if !controllerutil.ContainsFinalizer(&node, karpv1.TerminationFinalizer) {
			return false, fmt.Errorf("pending instance has a Node without graceful termination ownership")
		}
		if node.DeletionTimestamp.IsZero() {
			if deleteErr := p.kubeClient.Delete(ctx, &node, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &node.UID}}); client.IgnoreNotFound(deleteErr) != nil {
				return false, deleteErr
			}
		}
	}
	if foundNode {
		return false, nil
	}
	if deleteErr := vpc.DeleteInstance(ctx, *instance.ID); deleteErr != nil && !isIBMInstanceNotFoundError(deleteErr) {
		return false, deleteErr
	}
	_, err = vpc.GetInstance(ctx, *instance.ID)
	if isIBMInstanceNotFoundError(err) {
		return true, nil
	}
	return false, err
}
