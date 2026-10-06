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
	"net/http"
	"strconv"
	"strings"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/metrics"
)

type metricsTransport struct {
	base   http.RoundTripper
	region string
}

func InstrumentHTTPClient(original *http.Client, region string) *http.Client {
	if original == nil {
		original = http.DefaultClient
	}
	copy := *original
	if _, ok := copy.Transport.(*metricsTransport); ok {
		return &copy
	}
	base := copy.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copy.Transport = &metricsTransport{base: base, region: region}
	return &copy
}

func (t *metricsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	status := "transport_error"
	if response != nil {
		status = strconv.Itoa(response.StatusCode)
	}
	region := t.region
	if region == "" {
		region = "global"
		for _, segment := range strings.Split(request.URL.Hostname(), ".") {
			if strings.HasPrefix(segment, "us-") || strings.HasPrefix(segment, "eu-") || strings.HasPrefix(segment, "jp-") || strings.HasPrefix(segment, "au-") || strings.HasPrefix(segment, "ca-") || strings.HasPrefix(segment, "br-") {
				region = segment
				break
			}
		}
	}
	metrics.ApiRequests.WithLabelValues(apiOperation(request), status, region).Inc()
	return response, err
}

func apiOperation(request *http.Request) string {
	if request.URL.Hostname() == "globalcatalog.cloud.ibm.com" {
		return "CatalogEntries"
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) == 0 {
		return "Other"
	}
	last := parts[len(parts)-1]
	switch last {
	case "getWorkers", "getWorker", "createWorkerPool", "deleteWorkerPool", "removeWorkerPool", "resizeWorkerPool", "removeWorker", "getWorkerPool", "getWorkerPools":
		return last
	case "token":
		return "IAMToken"
	case "entries":
		return "CatalogEntries"
	case "attach", "detach", "tags":
		return "GlobalTags"
	}
	for i, part := range parts {
		if part == "entries" {
			return "CatalogEntries"
		}
		if part != "instances" {
			continue
		}
		if i != len(parts)-1 {
			switch request.Method {
			case http.MethodGet:
				return "GetInstance"
			case http.MethodDelete:
				return "DeleteInstance"
			case http.MethodPatch:
				return "UpdateInstance"
			}
		}
		if request.Method == http.MethodPost {
			return "CreateInstance"
		}
		return "ListInstances"
	}
	return "Other"
}
