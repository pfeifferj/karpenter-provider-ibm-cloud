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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/IBM/platform-services-go-sdk/globalcatalogv1"
	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestIAMTransportRecordsRateLimitResponse(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errorCode":"rate_limited","errorMessage":"retry later"}`))
	}))
	t.Cleanup(server.Close)
	client := NewIAMClient("test-key")
	auth := client.Authenticator.(*iamAuthenticator).auth
	auth.URL = server.URL
	require.Equal(t, 30*time.Second, auth.Client.Timeout)
	metric := metrics.ApiRequests.WithLabelValues("IAMToken", "429", "global")
	before := testutil.ToFloat64(metric)
	_, err := client.GetToken(context.Background())
	require.Error(t, err)
	require.Equal(t, 1, requests)
	require.Equal(t, before+1, testutil.ToFloat64(metric))
}

func TestCatalogTransportRecordsRateLimitResponseAfterTokenRefresh(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	t.Cleanup(server.Close)
	iam := &mockIAMClient{token: "first-token"}
	client := &GlobalCatalogClient{iamClient: iam}
	metric := metrics.ApiRequests.WithLabelValues("CatalogEntries", "429", "global")
	before := testutil.ToFloat64(metric)
	for _, token := range []string{"first-token", "refreshed-token"} {
		iam.token = token
		catalog, err := client.ensureClient(context.Background())
		require.NoError(t, err)
		sdk := catalog.(*globalcatalogv1.GlobalCatalogV1)
		require.NoError(t, sdk.Service.SetServiceURL(server.URL+"/entries"))
		_, err = client.GetInstanceType(context.Background(), "profile-id")
		require.Error(t, err)
	}
	require.Equal(t, 2, requests)
	require.Equal(t, before+2, testutil.ToFloat64(metric))
}
