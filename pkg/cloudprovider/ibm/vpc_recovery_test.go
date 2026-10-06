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

package ibm

import (
	"context"
	"fmt"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/globaltaggingv1"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/require"
)

type paginationVPC struct {
	*mockVPCClient
	pages int
}

func (m *paginationVPC) ListInstancesWithContext(_ context.Context, options *vpcv1.ListInstancesOptions) (*vpcv1.InstanceCollection, *core.DetailedResponse, error) {
	m.pages++
	if options.Start == nil {
		return &vpcv1.InstanceCollection{Instances: []vpcv1.Instance{{ID: core.StringPtr("first")}}, Next: &vpcv1.PageLink{Href: core.StringPtr("https://example.com/instances?start=second")}}, nil, nil
	}
	return &vpcv1.InstanceCollection{Instances: []vpcv1.Instance{{ID: core.StringPtr(*options.Start)}}}, nil, nil
}

func TestListInstancesIncludesEveryPage(t *testing.T) {
	mock := &paginationVPC{mockVPCClient: &mockVPCClient{}}
	vpc := NewVPCClientWithMock(mock)
	instances, err := vpc.ListInstances(context.Background())
	require.NoError(t, err)
	require.Len(t, instances, 2)
	require.Equal(t, "second", *instances[1].ID)
	require.Equal(t, 2, mock.pages)
}

type tagger struct {
	options *globaltaggingv1.AttachTagOptions
	failed  bool
}

func (t *tagger) AttachTagWithContext(_ context.Context, options *globaltaggingv1.AttachTagOptions) (*globaltaggingv1.TagResults, *core.DetailedResponse, error) {
	t.options = options
	return &globaltaggingv1.TagResults{Results: []globaltaggingv1.TagResultsItem{{ResourceID: options.Resources[0].ResourceID, IsError: &t.failed}}}, nil, nil
}

func TestUpdateInstanceTagsUsesResourceCRN(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			tags := &tagger{failed: failed}
			vpc := NewVPCClientWithMock(&mockVPCClient{getInstanceResponse: &vpcv1.Instance{CRN: core.StringPtr("crn:instance")}}, tags)
			err := vpc.UpdateInstanceTags(context.Background(), "id", map[string]string{"env": "test"})
			require.Equal(t, failed, err != nil)
			require.Equal(t, "crn:instance", *tags.options.Resources[0].ResourceID)
			require.Equal(t, []string{"env:test"}, tags.options.TagNames)
			require.True(t, *tags.options.Update)
			require.Nil(t, tags.options.Replace)
		})
	}
}

func TestUpdateInstanceTagsRequiresInitializedTagger(t *testing.T) {
	vpc := NewVPCClientWithMock(&mockVPCClient{})
	require.Error(t, vpc.UpdateInstanceTags(context.Background(), "id", map[string]string{"env": "test"}))
	require.NoError(t, vpc.UpdateInstanceTags(context.Background(), "id", nil))
}

func TestForRegionPreservesPrivateEndpointAndSharesClients(t *testing.T) {
	client, err := NewVPCClient("https://us-south.private.iaas.cloud.ibm.com/v1", "iam", "test-key", "us-south", "")
	require.NoError(t, err)
	regional, err := client.ForRegion("eu-de")
	require.NoError(t, err)
	require.Equal(t, "https://eu-de.private.iaas.cloud.ibm.com/v1", regional.baseURL)
	repeated, err := client.ForRegion("eu-de")
	require.NoError(t, err)
	require.Same(t, regional, repeated)
	_, err = client.ForRegion("evil.example/")
	require.Error(t, err)
}

func TestForRegionPreservesCustomEndpointBoundary(t *testing.T) {
	client, err := NewVPCClient("https://vpc.internal.example/v1", "iam", "test-key", "us-south", "")
	require.NoError(t, err)
	_, err = client.ForRegion("eu-de")
	require.Error(t, err)
}
