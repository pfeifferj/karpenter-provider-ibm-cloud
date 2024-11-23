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

package instancetype

import (
	"context"
	"fmt"

	"sigs.k8s.io/karpenter/pkg/cloudprovider"

	"github.com/pfeifferj/karpenter-provider-ibm-cloud/pkg/cloudprovider/ibm"
)

type Provider struct {
	client *ibm.Client
}

func NewProvider() (*Provider, error) {
	client, err := ibm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating IBM Cloud client: %w", err)
	}
	return &Provider{
		client: client,
	}, nil
}

func (p *Provider) Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error) {
	_, err := p.client.GetGlobalCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("getting Global Catalog client: %w", err)
	}

	// TODO: Use Global Catalog client to get instance type details
	return nil, fmt.Errorf("instance type not found: %s", name)
}

func (p *Provider) List(ctx context.Context) ([]*cloudprovider.InstanceType, error) {
	_, err := p.client.GetGlobalCatalogClient()
	if err != nil {
		return nil, fmt.Errorf("getting Global Catalog client: %w", err)
	}

	// TODO: Use Global Catalog client to list instance types
	return []*cloudprovider.InstanceType{}, nil
}

func (p *Provider) Create(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	// Instance types are predefined in IBM Cloud, so this is a no-op
	return nil
}

func (p *Provider) Delete(ctx context.Context, instanceType *cloudprovider.InstanceType) error {
	// Instance types are predefined in IBM Cloud, so this is a no-op
	return nil
}
