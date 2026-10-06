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

package httpclient

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPMetricsRecordRealStatusWithBoundedOperation(t *testing.T) {
	metric := metrics.ApiRequests.WithLabelValues("GetInstance", "429", "test-region")
	before := testutil.ToFloat64(metric)
	original := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("rate limited")), Request: r}, nil
	})}
	instrumented := InstrumentHTTPClient(InstrumentHTTPClient(original, "test-region"), "test-region")
	for _, id := range []string{"first-id", "another-id"} {
		response, err := instrumented.Get("https://api.ibm.com/v1/instances/" + id)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	require.Equal(t, before+2, testutil.ToFloat64(metric))
	require.IsType(t, roundTripFunc(nil), original.Transport)
}
