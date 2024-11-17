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

	"sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type Provider struct {
}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Create(ctx context.Context, node *v1alpha5.Machine) (*v1alpha5.Machine, error) {
	return node, nil
}

func (p *Provider) Delete(ctx context.Context, node *v1alpha5.Machine) error {
	return nil
}

func (p *Provider) List(ctx context.Context) ([]*v1alpha5.Machine, error) {
	return nil, nil
}

func (p *Provider) GetInstanceTypes(ctx context.Context) ([]*cloudprovider.InstanceType, error) {
	return nil, nil
}
