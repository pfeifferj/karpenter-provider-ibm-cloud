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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/require"
)

type invalidSerializablePrototype struct {
	*vpcv1.InstancePrototypeInstanceByImage
}

func TestListInstancesByNameKeepsFilterAcrossPages(t *testing.T) {
	requests := 0
	vpc, _ := createRequestFixture(t, func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/instances", request.URL.Path)
		require.Equal(t, "karpenter-checkpoint", request.URL.Query().Get("name"))
		require.Equal(t, "100", request.URL.Query().Get("limit"))
		body := `{"instances":[{"id":"matching-instance","name":"karpenter-checkpoint"}]}`
		if requests == 1 {
			require.Empty(t, request.URL.Query().Get("start"))
			body = `{"instances":[],"next":{"href":"https://test.iaas.cloud.ibm.com/v1/instances?start=next-page"}}`
		} else {
			require.Equal(t, "next-page", request.URL.Query().Get("start"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	instances, err := vpc.ListInstancesByName(context.Background(), "karpenter-checkpoint")
	require.NoError(t, err)
	require.Equal(t, 2, requests)
	require.Len(t, instances, 1)
	require.Equal(t, "matching-instance", *instances[0].ID)
}

func (*invalidSerializablePrototype) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("fixture JSON encoding failure")
}

func createRequestFixture(t *testing.T, transport identityTransport) (*VPCClient, vpcv1.InstancePrototypeIntf) {
	t.Helper()
	sdk, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{URL: "https://test.iaas.cloud.ibm.com/v1", Authenticator: &core.NoAuthAuthenticator{}})
	require.NoError(t, err)
	sdk.Service.SetHTTPClient(&http.Client{Transport: transport})
	prototype, err := sdk.NewInstancePrototypeInstanceByImageInstanceByImageInstanceByNetworkAttachment(
		&vpcv1.ImageIdentityByID{ID: core.StringPtr("image")}, &vpcv1.ZoneIdentityByName{Name: core.StringPtr("us-south-1")},
		&vpcv1.InstanceNetworkAttachmentPrototype{VirtualNetworkInterface: &vpcv1.InstanceNetworkAttachmentPrototypeVirtualNetworkInterfaceVirtualNetworkInterfacePrototypeInstanceNetworkAttachmentContext{Subnet: &vpcv1.SubnetIdentityByID{ID: core.StringPtr("subnet")}}},
	)
	require.NoError(t, err)
	prototype.Profile = &vpcv1.InstanceProfileIdentityByName{Name: core.StringPtr("bx2-2x8")}
	return NewVPCClientWithMock(sdk), prototype
}

func TestCreateRequestPreservesActualRemoteStatusAndKeepsTransportFailureAmbiguous(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusServiceUnavailable, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests := 0
			vpc, prototype := createRequestFixture(t, func(request *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/v1/instances", request.URL.Path)
				payload := map[string]any{}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
				require.Equal(t, map[string]any{"id": "image"}, payload["image"])
				require.Contains(t, payload, "primary_network_attachment")
				require.NotContains(t, payload, "primary_network_interface")
				if status == 0 {
					return nil, fmt.Errorf("response lost after write")
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"errors":[{"code":"validation_failed","message":"Expected only one oneOf fields to be set: got 0"}]}`))}, nil
			})
			_, err := vpc.CreateInstance(context.Background(), prototype)
			require.Error(t, err)
			require.Equal(t, 1, requests)
			require.False(t, IsCreateInstanceNotSent(err))
			require.Equal(t, status, ParseError(err).StatusCode)
		})
	}
}

func TestCreateRequestPreflightFailureNeverCallsSDKTransport(t *testing.T) {
	requests := 0
	vpc, _ := createRequestFixture(t, func(*http.Request) (*http.Response, error) {
		requests++
		return nil, fmt.Errorf("unexpected HTTP request")
	})
	for _, prototype := range []vpcv1.InstancePrototypeIntf{nil, &invalidSerializablePrototype{InstancePrototypeInstanceByImage: &vpcv1.InstancePrototypeInstanceByImage{Image: &vpcv1.ImageIdentityByID{ID: core.StringPtr("image")}, Zone: &vpcv1.ZoneIdentityByName{Name: core.StringPtr("us-south-1")}}}} {
		require.True(t, IsCreateInstanceNotSent(vpc.ValidateCreateInstance(prototype)))
		_, err := vpc.CreateInstance(context.Background(), prototype)
		require.Error(t, err)
		require.True(t, IsCreateInstanceNotSent(err))
	}
	require.Zero(t, requests)
}
