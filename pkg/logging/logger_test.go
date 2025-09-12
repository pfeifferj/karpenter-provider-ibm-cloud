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

package logging

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger("test-component")
	assert.NotNil(t, logger)
	assert.Equal(t, "test-component", logger.GetComponent())
}

func TestFromContext(t *testing.T) {
	ctx := context.Background()
	logger := FromContext(ctx, "test-component")
	assert.NotNil(t, logger)
	assert.Equal(t, "test-component", logger.GetComponent())
}

func TestWithName(t *testing.T) {
	logger := NewLogger("base")
	childLogger := logger.WithName("child")
	assert.Equal(t, "base.child", childLogger.GetComponent())
}

func TestWithValues(t *testing.T) {
	logger := NewLogger("test")
	loggerWithValues := logger.WithValues("key", "value")
	assert.NotNil(t, loggerWithValues)
	assert.Equal(t, "test", loggerWithValues.GetComponent())
}

func TestComponentLoggers(t *testing.T) {
	instanceLogger := InstanceLogger()
	assert.NotNil(t, instanceLogger)
	assert.Contains(t, instanceLogger.GetComponent(), "instance")

	pricingLogger := PricingLogger()
	assert.NotNil(t, pricingLogger)
	assert.Contains(t, pricingLogger.GetComponent(), "pricing")

	subnetLogger := SubnetLogger()
	assert.NotNil(t, subnetLogger)
	assert.Contains(t, subnetLogger.GetComponent(), "subnet")
}

func TestLoggingMethods(t *testing.T) {
	logger := NewLogger("test")

	// These should not panic
	logger.Info("test info message", "key", "value")
	logger.Debug("test debug message", "key", "value")
	logger.Warn("test warning message", "key", "value")
	logger.Error(nil, "test error message", "key", "value")
}

func TestLogLevelFiltering(t *testing.T) {
	// Save original LOG_LEVEL
	originalLogLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		_ = os.Setenv("LOG_LEVEL", originalLogLevel)
	}()

	tests := []struct {
		logLevel       string
		shouldLogDebug bool
		shouldLogInfo  bool
		shouldLogWarn  bool
		shouldLogError bool
	}{
		{"debug", true, true, true, true},
		{"info", false, true, true, true},
		{"warn", false, false, true, true},
		{"error", false, false, false, true},
		{"", false, true, true, true},        // default to info
		{"invalid", false, true, true, true}, // fallback to info
	}

	for _, tt := range tests {
		t.Run("log_level_"+tt.logLevel, func(t *testing.T) {
			_ = os.Setenv("LOG_LEVEL", tt.logLevel)
			logger := NewLogger("test")

			assert.Equal(t, tt.shouldLogDebug, logger.shouldLog("debug"))
			assert.Equal(t, tt.shouldLogInfo, logger.shouldLog("info"))
			assert.Equal(t, tt.shouldLogWarn, logger.shouldLog("warn"))
			assert.Equal(t, tt.shouldLogError, logger.shouldLog("error"))
		})
	}
}
