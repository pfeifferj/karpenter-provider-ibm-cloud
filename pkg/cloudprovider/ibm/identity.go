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
	"fmt"
	"net/http"
	"strings"
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
		service, err := newCredentialIdentityService()
		if err != nil {
			return "", fmt.Errorf("creating credential identity client: %w", err)
		}
		c.identityService = service
	}
	builder := core.NewRequestBuilder(core.POST).WithContext(ctx)
	if _, err := builder.ResolveRequestURL(c.identityService.Options.URL, "/identity/token", nil); err != nil {
		return "", err
	}
	builder.AddHeader("Accept", "application/json")
	builder.AddHeader("Content-Type", "application/x-www-form-urlencoded")
	builder.AddFormData("grant_type", "", "", "urn:ibm:params:oauth:grant-type:apikey")
	builder.AddFormData("response_type", "", "", "cloud_iam")
	builder.AddFormData("apikey", "", "", c.apiKey)
	request, err := builder.Build()
	if err != nil {
		return "", err
	}
	var tokenResponse core.IamTokenServerResponse
	if response, requestErr := c.identityService.Request(request, &tokenResponse); requestErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("verifying VPC credential account: %w", ctx.Err())
		}
		if response != nil {
			return "", fmt.Errorf("verifying VPC credential account: IAM token request failed with HTTP status %d", response.StatusCode)
		}
		return "", fmt.Errorf("verifying VPC credential account: IAM token request failed")
	}
	accountID, err := accountFromIAMToken(tokenResponse.AccessToken)
	if err != nil {
		return "", err
	}
	c.identityAccount = accountID
	return c.identityAccount, nil
}

func newCredentialIdentityService() (*core.BaseService, error) {
	service, err := core.NewBaseService(&core.ServiceOptions{
		URL: "https://iam.cloud.ibm.com", Authenticator: &core.NoAuthAuthenticator{},
	})
	if err != nil {
		return nil, err
	}
	identityClient := service.GetHTTPClient()
	identityClient.Timeout = 30 * time.Second
	identityClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	service.SetHTTPClient(httpclient.InstrumentHTTPClient(identityClient, "global"))
	return service, nil
}

// The token is accepted only from the fixed IAM HTTPS endpoint using this client's API key.
func accountFromIAMToken(token string) (string, error) {
	segments := strings.Split(token, ".")
	if len(segments) != 3 || segments[0] == "" || segments[1] == "" || segments[2] == "" {
		return "", fmt.Errorf("credential identity response has an invalid access token")
	}
	header, err := base64.RawURLEncoding.DecodeString(segments[0])
	var tokenHeader struct {
		Algorithm string `json:"alg"`
	}
	if err != nil || json.Unmarshal(header, &tokenHeader) != nil || tokenHeader.Algorithm == "" || strings.EqualFold(tokenHeader.Algorithm, "none") {
		return "", fmt.Errorf("credential identity response has an invalid token header")
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return "", fmt.Errorf("credential identity response has an invalid token payload")
	}
	if _, err := base64.RawURLEncoding.DecodeString(segments[2]); err != nil {
		return "", fmt.Errorf("credential identity response has an invalid token signature")
	}
	var identity struct {
		Account struct {
			ID string `json:"bss"`
		} `json:"account"`
	}
	if err := json.Unmarshal(payload, &identity); err != nil || !ibmAccountIDPattern.MatchString(identity.Account.ID) {
		return "", fmt.Errorf("credential identity response has no valid account ID")
	}
	return identity.Account.ID, nil
}
