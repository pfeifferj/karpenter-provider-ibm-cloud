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
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/stretchr/testify/require"
)

type identityTransport func(*http.Request) (*http.Response, error)

func (f identityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func identityFixture(t *testing.T, transport identityTransport) *VPCClient {
	t.Helper()
	service, err := core.NewBaseService(&core.ServiceOptions{URL: "https://iam.cloud.ibm.com", Authenticator: &core.NoAuthAuthenticator{}})
	require.NoError(t, err)
	service.SetHTTPClient(&http.Client{Transport: transport})
	return &VPCClient{apiKey: "fixture-vpc-key", identityService: service}
}

func TestResolveVPCAccountUsesExactCredentialAndCachesConcurrentReads(t *testing.T) {
	accountID := strings.Repeat("a", 32)
	var requests atomic.Int32
	vpc := identityFixture(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/apikeys/details", request.URL.Path)
		require.Equal(t, "fixture-vpc-key", request.Header.Get("IAM-ApiKey"))
		require.Empty(t, request.URL.RawQuery)
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"account_id":"` + accountID + `"}`))}, nil
	})
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := vpc.ResolveAccountID(context.Background())
			require.NoError(t, err)
			require.Equal(t, accountID, resolved)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, requests.Load())
}

func TestResolveVPCAccountRefusesUnavailableOrMalformedIdentity(t *testing.T) {
	for _, fixture := range []struct {
		status int
		body   string
	}{
		{http.StatusForbidden, `{"errors":[{"code":"forbidden"}]}`},
		{http.StatusTooManyRequests, `{"errors":[{"code":"rate_limit"}]}`},
		{http.StatusOK, `{}`},
		{http.StatusOK, `{"account_id":"malformed"}`},
	} {
		vpc := identityFixture(t, func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: fixture.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture.body))}, nil
		})
		accountID, err := vpc.ResolveAccountID(context.Background())
		require.Error(t, err)
		require.Empty(t, accountID)
		require.Empty(t, vpc.identityAccount)
	}
}
