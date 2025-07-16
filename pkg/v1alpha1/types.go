/*
Copyright 2024 The Kubernetes Authors.

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

package v1alpha1

import (
	v1alpha1 "github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Group is the API group for IBM Cloud provider
	Group = "karpenter.ibm.sh"

	// AnnotationIBMNodeClassHash is the hash of the IBMNodeClass spec
	AnnotationIBMNodeClassHash = Group + "/nodeclass-hash"
	// AnnotationIBMNodeClassHashVersion is the version of the hash algorithm
	AnnotationIBMNodeClassHashVersion = Group + "/nodeclass-hash-version"

	// Labels that can be selected on and are propagated to the node
	InstanceSizeLabelKey   = Group + "/instance-size"
	InstanceFamilyLabelKey = Group + "/instance-family"
	InstanceMemoryLabelKey = Group + "/instance-memory"
	InstanceCPULabelKey    = Group + "/instance-cpu"

	// Internal labels that are propagated to the node
	IbmLabelKey          = "ibm.x-k8s.io/node"
	IbmLabelValue        = "fake"
	NodeViewerLabelKey   = "eks-node-viewer/instance-price"
	IbmPartitionLabelKey = "ibm-partition"
)

// IBMNodeClassSpec defines the desired state of IBMNodeClass
// +k8s:deepcopy-gen=true
type IBMNodeClassSpec struct {
	// Region is the IBM Cloud region where nodes will be provisioned
	// +kubebuilder:validation:Required
	Region string `json:"region"`

	// Zones is the list of zones within the region for node placement
	// +optional
	Zones []string `json:"zones,omitempty"`

	// VPC is the VPC ID where nodes will be created
	// +kubebuilder:validation:Required
	VPC string `json:"vpc"`

	// Subnet is the subnet ID where nodes will be created
	// +kubebuilder:validation:Required
	Subnet string `json:"subnet"`

	// InstanceProfile is the instance profile to use for nodes
	// +kubebuilder:validation:Required
	InstanceProfile string `json:"instanceProfile"`

	// Image is the ID of the image to use for nodes
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ResourceGroup is the resource group for the provisioned resources
	// +optional
	ResourceGroup string `json:"resourceGroup,omitempty"`

	// SecurityGroups are the security group IDs to attach to nodes
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// Tags to apply to provisioned resources
	// +optional
	Tags map[string]string `json:"tags,omitempty"`
}

// IBMNodeClass is the Schema for the IBMNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories=karpenter
// +kubebuilder:resource:shortName=ibmnc;ibmncs
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion
// +k8s:deepcopy-gen=true
type IBMNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec IBMNodeClassSpec `json:"spec"`

	// +optional
	Status v1alpha1.IBMNodeClassStatus `json:"status,omitempty"`
}

// IBMNodeClassList contains a list of IBMNodeClass
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=true
type IBMNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IBMNodeClass `json:"items"`
}
