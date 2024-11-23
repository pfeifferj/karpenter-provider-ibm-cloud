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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

type IBMCloudInstanceProvider struct {
	client *ibm.Client
}

func NewProvider() (*IBMCloudInstanceProvider, error) {
	client, err := ibm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating IBM Cloud client: %w", err)
	}
	return &IBMCloudInstanceProvider{
		client: client,
	}, nil
}

func (p *IBMCloudInstanceProvider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*corev1.Node, error) {
	_, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// TODO: Use VPC client to create instance
	// For now returning a mock node
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nodeClaim.Name,
			Labels:      nodeClaim.Labels,
			Annotations: nodeClaim.Annotations,
		},
		Spec: corev1.NodeSpec{
			ProviderID: nodeClaim.Status.ProviderID,
		},
	}
	return node, nil
}

func (p *IBMCloudInstanceProvider) Delete(ctx context.Context, node *corev1.Node) error {
	_, err := p.client.GetVPCClient()
	if err != nil {
		return fmt.Errorf("getting VPC client: %w", err)
	}

	// TODO: Use VPC client to delete instance
	return nil
}

func (p *IBMCloudInstanceProvider) GetInstance(ctx context.Context, node *corev1.Node) (*Instance, error) {
	_, err := p.client.GetVPCClient()
	if err != nil {
		return nil, fmt.Errorf("getting VPC client: %w", err)
	}

	// TODO: Use VPC client to get instance details
	// For now returning a mock instance
	return &Instance{
		ID:           "mock-id",
		Type:         "mock-type",
		Zone:         "mock-zone",
		Region:       p.client.GetRegion(),
		CapacityType: "on-demand",
		ImageID:      "mock-image",
		Tags:         make(map[string]string),
	}, nil
}
