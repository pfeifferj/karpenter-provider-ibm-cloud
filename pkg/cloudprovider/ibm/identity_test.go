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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type identityTransport func(*http.Request) (*http.Response, error)

func (f identityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func identityFixture(t *testing.T, transport identityTransport) *VPCClient {
	t.Helper()
	service, err := newCredentialIdentityService()
	require.NoError(t, err)
	service.GetHTTPClient().Transport = transport
	return &VPCClient{apiKey: "fixture-vpc-key", identityService: service}
}

func accountToken(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString([]byte("fixture-signature"))
}

func accountTokenResponse(token string) string {
	encoded, _ := json.Marshal(map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 3600})
	return string(encoded)
}

func TestResolveVPCAccountUsesExactCredentialAndCachesConcurrentReads(t *testing.T) {
	accountID := strings.Repeat("a", 32)
	t.Setenv("IBMCLOUD_API_KEY", "unrelated-key")
	t.Setenv("IBMCLOUD_TOKEN", accountToken(`{"account":{"bss":"`+strings.Repeat("b", 32)+`"}}`))
	t.Setenv("IBM_ACCOUNT_ID", strings.Repeat("b", 32))
	var requests atomic.Int32
	vpc := identityFixture(t, func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https", request.URL.Scheme)
		require.Equal(t, "iam.cloud.ibm.com", request.URL.Host)
		require.Equal(t, "/identity/token", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.Empty(t, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "fixture-vpc-key", request.PostForm.Get("apikey"))
		require.Equal(t, "urn:ibm:params:oauth:grant-type:apikey", request.PostForm.Get("grant_type"))
		require.Equal(t, "cloud_iam", request.PostForm.Get("response_type"))
		require.Empty(t, request.URL.RawQuery)
		_, bounded := request.Context().Deadline()
		require.True(t, bounded)
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(accountTokenResponse(accountToken(`{"sub_type":"ServiceId","account":{"bss":"` + accountID + `"}}`))))}, nil
	})
	require.Equal(t, 30*time.Second, vpc.identityService.GetHTTPClient().Timeout)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := vpc.ResolveAccountID(ctx)
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
		{http.StatusForbidden, `{"errors":[{"code":"forbidden","message":"secret-token"}]}`},
		{http.StatusTooManyRequests, `{"errors":[{"code":"rate_limit"}]}`},
		{http.StatusOK, `{}`},
		{http.StatusOK, `{"access_token":"secret-token"}`},
		{http.StatusOK, accountTokenResponse(accountToken(`{}`))},
		{http.StatusOK, accountTokenResponse(accountToken(`{"account":{"bss":"malformed"}}`))},
		{http.StatusOK, accountTokenResponse(accountToken(`{"account":{"bss":"` + strings.Repeat("A", 32) + `"}}`))},
		{http.StatusOK, accountTokenResponse(accountToken(`{"account":[]}`))},
		{http.StatusOK, accountTokenResponse("invalid.token.signature")},
		{http.StatusOK, accountTokenResponse(base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"account":{"bss":"`+strings.Repeat("a", 32)+`"}}`)) + ".")},
	} {
		vpc := identityFixture(t, func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: fixture.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fixture.body))}, nil
		})
		accountID, err := vpc.ResolveAccountID(context.Background())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-token")
		require.Empty(t, accountID)
		require.Empty(t, vpc.identityAccount)
	}
}

func TestResolveVPCAccountDoesNotFollowTokenRedirects(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			requests := 0
			vpc := identityFixture(t, func(request *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, "iam.cloud.ibm.com", request.URL.Host)
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://untrusted.example/token"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
			})
			accountID, err := vpc.ResolveAccountID(context.Background())
			require.Error(t, err)
			require.Empty(t, accountID)
			require.Equal(t, 1, requests)
		})
	}
}

func TestResolveVPCAccountHonorsRequestCancellation(t *testing.T) {
	vpc := identityFixture(t, func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	accountID, err := vpc.ResolveAccountID(ctx)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
	require.Empty(t, accountID)
	require.Empty(t, vpc.identityAccount)
}
