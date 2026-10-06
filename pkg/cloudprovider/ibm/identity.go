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
	"net/http"
	"time"

	"github.com/IBM/go-sdk-core/v5/core"

	"github.com/kubernetes-sigs/karpenter-provider-ibm-cloud/pkg/httpclient"
)

func (c *VPCClient) ResolveAccountID(ctx context.Context) (string, error) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if c.identityAccount != "" {
		return c.identityAccount, nil
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("VPC credential account cannot be verified without an API key")
	}
	if c.identityService == nil {
		service, err := core.NewBaseService(&core.ServiceOptions{
			URL: "https://iam.cloud.ibm.com", Authenticator: NewIAMAuthenticator(c.apiKey),
		})
		if err != nil {
			return "", fmt.Errorf("creating credential identity client: %w", err)
		}
		identityClient := service.GetHTTPClient()
		identityClient.Timeout = 30 * time.Second
		service.SetHTTPClient(httpclient.InstrumentHTTPClient(identityClient, "global"))
		c.identityService = service
	}
	builder := core.NewRequestBuilder(core.GET).WithContext(ctx)
	if _, err := builder.ResolveRequestURL(c.identityService.Options.URL, "/v1/apikeys/details", nil); err != nil {
		return "", err
	}
	builder.AddHeader("Accept", "application/json")
	builder.AddHeader(http.CanonicalHeaderKey("IAM-ApiKey"), c.apiKey)
	request, err := builder.Build()
	if err != nil {
		return "", err
	}
	var identity struct {
		AccountID string `json:"account_id"`
	}
	if _, err := c.identityService.Request(request, &identity); err != nil {
		return "", fmt.Errorf("verifying VPC credential account: %w", err)
	}
	if !ibmAccountIDPattern.MatchString(identity.AccountID) {
		return "", fmt.Errorf("credential identity response has no valid account ID")
	}
	c.identityAccount = identity.AccountID
	return c.identityAccount, nil
}
