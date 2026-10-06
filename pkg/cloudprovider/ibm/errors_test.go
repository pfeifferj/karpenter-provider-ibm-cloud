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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIBMError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *IBMError
		expected string
	}{
		{
			name: "error with code",
			err: &IBMError{
				Code:       "instance_not_found",
				StatusCode: 404,
				Message:    "Instance not found",
			},
			expected: "IBM Cloud error (code: instance_not_found, status: 404): Instance not found",
		},
		{
			name: "error without code",
			err: &IBMError{
				StatusCode: 500,
				Message:    "Internal server error",
			},
			expected: "IBM Cloud error (status: 500): Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestIBMError_TypeChecks(t *testing.T) {
	tests := []struct {
		name   string
		err    *IBMError
		checks map[string]bool
	}{
		{
			name: "not found error",
			err: &IBMError{
				Type:       ErrorTypeNotFound,
				StatusCode: http.StatusNotFound,
			},
			checks: map[string]bool{
				"IsNotFound":     true,
				"IsClientError":  true,
				"IsServerError":  false,
				"IsUnauthorized": false,
			},
		},
		{
			name: "server error",
			err: &IBMError{
				Type:       ErrorTypeServerError,
				StatusCode: http.StatusInternalServerError,
				Retryable:  true,
			},
			checks: map[string]bool{
				"IsServerError": true,
				"IsClientError": false,
				"Retryable":     true,
			},
		},
		{
			name: "rate limit error",
			err: &IBMError{
				Type:       ErrorTypeRateLimit,
				StatusCode: http.StatusTooManyRequests,
				RetryAfter: 60,
				Retryable:  true,
			},
			checks: map[string]bool{
				"IsRateLimit":   true,
				"IsClientError": true,
				"Retryable":     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if val, ok := tt.checks["IsNotFound"]; ok {
				assert.Equal(t, val, tt.err.IsNotFound())
			}
			if val, ok := tt.checks["IsServerError"]; ok {
				assert.Equal(t, val, tt.err.IsServerError())
			}
			if val, ok := tt.checks["IsClientError"]; ok {
				assert.Equal(t, val, tt.err.IsClientError())
			}
			if val, ok := tt.checks["IsRateLimit"]; ok {
				assert.Equal(t, val, tt.err.IsRateLimit())
			}
			if val, ok := tt.checks["IsUnauthorized"]; ok {
				assert.Equal(t, val, tt.err.IsUnauthorized())
			}
			if val, ok := tt.checks["Retryable"]; ok {
				assert.Equal(t, val, tt.err.Retryable)
			}
		})
	}
}

func TestParseError_StringPatterns(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedType ErrorType
		expectedCode int
		retryable    bool
	}{
		{
			name:         "not found error",
			err:          errors.New("instance not found"),
			expectedType: ErrorTypeNotFound,
			expectedCode: http.StatusNotFound,
			retryable:    false,
		},
		{
			name:         "not_found error",
			err:          errors.New("resource_not_found"),
			expectedType: ErrorTypeNotFound,
			expectedCode: http.StatusNotFound,
			retryable:    false,
		},
		{
			name:         "404 error",
			err:          errors.New("Error: 404 - Resource not found"),
			expectedType: ErrorTypeNotFound,
			expectedCode: http.StatusNotFound,
			retryable:    false,
		},
		{
			name:         "unauthorized error",
			err:          errors.New("unauthorized access"),
			expectedType: ErrorTypeUnauthorized,
			expectedCode: http.StatusUnauthorized,
			retryable:    false,
		},
		{
			name:         "timeout error",
			err:          errors.New("request timeout"),
			expectedType: ErrorTypeTimeout,
			expectedCode: http.StatusRequestTimeout,
			retryable:    true,
		},
		{
			name:         "rate limit error",
			err:          errors.New("rate limit exceeded"),
			expectedType: ErrorTypeRateLimit,
			expectedCode: http.StatusTooManyRequests,
			retryable:    true,
		},
		{
			name:         "internal server error",
			err:          errors.New("internal server error occurred"),
			expectedType: ErrorTypeServerError,
			expectedCode: http.StatusInternalServerError,
			retryable:    true,
		},
		{
			name:         "500 error",
			err:          errors.New("Error 500: Server problem"),
			expectedType: ErrorTypeServerError,
			expectedCode: http.StatusInternalServerError,
			retryable:    true,
		},
		{
			name:         "validation error",
			err:          errors.New("validation failed for field 'name'"),
			expectedType: ErrorTypeValidation,
			expectedCode: http.StatusBadRequest,
			retryable:    false,
		},
		{
			name:         "conflict error",
			err:          errors.New("resource already exists"),
			expectedType: ErrorTypeConflict,
			expectedCode: http.StatusConflict,
			retryable:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ibmErr := ParseError(tt.err)
			assert.NotNil(t, ibmErr)
			assert.Equal(t, tt.expectedType, ibmErr.Type)
			assert.Equal(t, tt.expectedCode, ibmErr.StatusCode)
			assert.Equal(t, tt.retryable, ibmErr.Retryable)
			assert.Equal(t, tt.err.Error(), ibmErr.Message)
		})
	}
}

type errorResponseTransport func(*http.Request) (*http.Response, error)

func (f errorResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestParseError_WrappedSDKHTTPResponse(t *testing.T) {
	sdk, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{URL: "https://test.iaas.cloud.ibm.com/v1", Authenticator: &core.NoAuthAuthenticator{}})
	require.NoError(t, err)
	requests := 0
	sdk.Service.SetHTTPClient(&http.Client{Transport: errorResponseTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"errors":[{"code":"validation_failed","message":"Expected only one oneOf fields to be set: got 0","more_info":"https://cloud.ibm.com/apidocs/vpc"}]}`)),
			Request:    request,
		}, nil
	})})
	_, response, requestErr := sdk.GetInstanceWithContext(context.Background(), &vpcv1.GetInstanceOptions{ID: core.StringPtr("instance-id")})
	require.Error(t, requestErr)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, 1, requests)
	wrappedErr := fmt.Errorf("reconciling claim: %w", fmt.Errorf("getting instance: %w", requestErr))
	parsed := ParseError(wrappedErr)
	require.Equal(t, http.StatusBadRequest, parsed.StatusCode)
	require.Equal(t, ErrorTypeValidation, parsed.Type)
	require.Equal(t, "validation_failed", parsed.Code)
	require.Equal(t, "https://cloud.ibm.com/apidocs/vpc", parsed.MoreInfo)
	require.False(t, parsed.Retryable)
	require.ErrorIs(t, parsed, wrappedErr)
	var httpProblem *core.HTTPProblem
	require.ErrorAs(t, parsed, &httpProblem)
	require.Same(t, response, httpProblem.Response)
}

func TestParseError_SDKProblemWithoutResponse(t *testing.T) {
	for _, message := range []string{"invalid request", "404 not found", "request timeout", "Expected only one oneOf fields to be set: got 0"} {
		t.Run(message, func(t *testing.T) {
			var problem error = core.SDKErrorf(context.DeadlineExceeded, message, "request-error", core.NewProblemComponent("github.com/IBM/vpc-go-sdk/vpcv1", "test"))
			wrappedErr := fmt.Errorf("creating instance: %w", problem)
			parsed := ParseError(wrappedErr)
			require.Zero(t, parsed.StatusCode)
			require.Equal(t, ErrorTypeUnknown, parsed.Type)
			require.ErrorIs(t, parsed, context.DeadlineExceeded)
		})
	}
}

func TestParseError_TransportFailureAfterRequestRemainsUncertain(t *testing.T) {
	sdk, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{URL: "https://test.iaas.cloud.ibm.com/v1", Authenticator: &core.NoAuthAuthenticator{}})
	require.NoError(t, err)
	requests := 0
	transportErr := errors.New("validation response lost after request submission")
	sdk.Service.SetHTTPClient(&http.Client{Transport: errorResponseTransport(func(_ *http.Request) (*http.Response, error) {
		requests++
		return nil, transportErr
	})})
	_, response, requestErr := sdk.GetInstanceWithContext(context.Background(), &vpcv1.GetInstanceOptions{ID: core.StringPtr("instance-id")})
	require.Error(t, requestErr)
	require.Nil(t, response)
	require.Equal(t, 1, requests)
	parsed := ParseError(fmt.Errorf("getting instance: %w", requestErr))
	require.Zero(t, parsed.StatusCode)
	require.Equal(t, ErrorTypeUnknown, parsed.Type)
	require.ErrorIs(t, parsed, transportErr)
}

func TestParseError_HTTPProblemWithoutResponse(t *testing.T) {
	var problem error = &core.HTTPProblem{IBMProblem: core.IBMErrorf(nil, core.NewProblemComponent("test", "test"), "invalid request 404", "missing-response")}
	parsed := ParseError(fmt.Errorf("creating instance: %w", problem))
	require.Zero(t, parsed.StatusCode)
	require.Equal(t, ErrorTypeUnknown, parsed.Type)
}

func TestParseErrorResponse_ActualStatus(t *testing.T) {
	for _, tc := range []struct {
		status    int
		errType   ErrorType
		retryable bool
	}{
		{http.StatusBadRequest, ErrorTypeValidation, false},
		{http.StatusUnauthorized, ErrorTypeUnauthorized, false},
		{http.StatusForbidden, ErrorTypeForbidden, false},
		{http.StatusNotFound, ErrorTypeNotFound, false},
		{http.StatusRequestTimeout, ErrorTypeTimeout, true},
		{http.StatusConflict, ErrorTypeConflict, false},
		{http.StatusUnprocessableEntity, ErrorTypeValidation, false},
		{http.StatusTooManyRequests, ErrorTypeRateLimit, true},
		{http.StatusInternalServerError, ErrorTypeServerError, true},
		{http.StatusServiceUnavailable, ErrorTypeServerError, true},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			original := fmt.Errorf("operation: %w", errors.New("invalid object, formerly returned 404"))
			parsed := ParseErrorResponse(original, &core.DetailedResponse{
				StatusCode: tc.status,
				Headers:    http.Header{"Retry-After": []string{"37"}},
				Result:     map[string]interface{}{"code": "request_failed", "more_info": "https://cloud.ibm.com/apidocs/vpc"},
			})
			require.Equal(t, tc.status, parsed.StatusCode)
			require.Equal(t, tc.errType, parsed.Type)
			require.Equal(t, tc.retryable, parsed.Retryable)
			require.Equal(t, "request_failed", parsed.Code)
			require.Equal(t, "https://cloud.ibm.com/apidocs/vpc", parsed.MoreInfo)
			require.Equal(t, 37, parsed.RetryAfter)
			require.ErrorIs(t, parsed, original)
		})
	}
}

func TestParseErrorResponse_MissingDetails(t *testing.T) {
	for _, result := range []interface{}{nil, map[string]interface{}{"errors": []interface{}{}}, map[string]interface{}{"errors": "unexpected"}, map[string]interface{}{"errorCode": "failure"}} {
		parsed := ParseErrorResponse(errors.New("request failed"), &core.DetailedResponse{StatusCode: http.StatusBadGateway, Result: result})
		require.Equal(t, http.StatusBadGateway, parsed.StatusCode)
		require.Equal(t, ErrorTypeServerError, parsed.Type)
	}
	require.Nil(t, ParseErrorResponse(nil, &core.DetailedResponse{StatusCode: http.StatusBadRequest}))
	parsed := ParseErrorResponse(errors.New("transport failed"), nil)
	require.Zero(t, parsed.StatusCode)
}

func TestParseError_AlreadyIBMError(t *testing.T) {
	originalErr := &IBMError{
		Type:       ErrorTypeNotFound,
		StatusCode: http.StatusNotFound,
		Code:       "test_not_found",
		Message:    "Test not found",
	}

	parsedErr := ParseError(originalErr)
	assert.Equal(t, originalErr, parsedErr)
	assert.Same(t, originalErr, ParseError(fmt.Errorf("reconciling instance: %w", originalErr)))
}

func TestParseError_Nil(t *testing.T) {
	assert.Nil(t, ParseError(nil))
}

func TestHelperFunctions(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		checkFn  func(error) bool
		expected bool
	}{
		{
			name:     "IsNotFound with not found error",
			err:      errors.New("resource not found"),
			checkFn:  IsNotFound,
			expected: true,
		},
		{
			name:     "IsNotFound with other error",
			err:      errors.New("internal error"),
			checkFn:  IsNotFound,
			expected: false,
		},
		{
			name:     "IsRetryable with server error",
			err:      errors.New("500 internal server error"),
			checkFn:  IsRetryable,
			expected: true,
		},
		{
			name:     "IsRetryable with client error",
			err:      errors.New("400 bad request"),
			checkFn:  IsRetryable,
			expected: false,
		},
		{
			name:     "IsRateLimit with rate limit error",
			err:      errors.New("429 too many requests"),
			checkFn:  IsRateLimit,
			expected: true,
		},
		{
			name:     "IsTimeout with timeout error",
			err:      errors.New("request timeout occurred"),
			checkFn:  IsTimeout,
			expected: true,
		},
		{
			name:     "nil error",
			err:      nil,
			checkFn:  IsNotFound,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.checkFn(tt.err))
		})
	}
}

func TestIBMError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	ibmErr := &IBMError{
		Type:       ErrorTypeUnknown,
		StatusCode: 0,
		Message:    "wrapped error",
		wrapped:    originalErr,
	}

	assert.Equal(t, originalErr, ibmErr.Unwrap())
}

func TestParseError_ComplexPatterns(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedType ErrorType
		expectedCode int
	}{
		{
			name:         "502 bad gateway",
			err:          errors.New("Error: 502 Bad Gateway"),
			expectedType: ErrorTypeServerError,
			expectedCode: http.StatusBadGateway,
		},
		{
			name:         "503 service unavailable",
			err:          errors.New("503 Service Unavailable"),
			expectedType: ErrorTypeServerError,
			expectedCode: http.StatusServiceUnavailable,
		},
		{
			name:         "504 gateway timeout",
			err:          errors.New("Gateway timeout: 504"),
			expectedType: ErrorTypeServerError,
			expectedCode: http.StatusGatewayTimeout,
		},
		{
			name:         "permission denied",
			err:          errors.New("permission denied to access resource"),
			expectedType: ErrorTypeForbidden,
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "authentication failed",
			err:          errors.New("authentication failed"),
			expectedType: ErrorTypeUnauthorized,
			expectedCode: http.StatusUnauthorized,
		},
		{
			name:         "already exists",
			err:          errors.New("resource already exists"),
			expectedType: ErrorTypeConflict,
			expectedCode: http.StatusConflict,
		},
		{
			name:         "invalid request",
			err:          errors.New("invalid request parameters"),
			expectedType: ErrorTypeValidation,
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ibmErr := ParseError(tt.err)
			assert.NotNil(t, ibmErr)
			assert.Equal(t, tt.expectedType, ibmErr.Type)
			assert.Equal(t, tt.expectedCode, ibmErr.StatusCode)
		})
	}
}
