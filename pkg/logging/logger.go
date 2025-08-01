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

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Logger provides structured logging for IBM Cloud provider components
type Logger struct {
	logger    logr.Logger
	component string
}

// NewLogger creates a new logger for the specified component
func NewLogger(component string) *Logger {
	return &Logger{
		logger:    log.Log.WithName(component),
		component: component,
	}
}

// FromContext creates a logger from context
func FromContext(ctx context.Context, component string) *Logger {
	return &Logger{
		logger:    log.FromContext(ctx).WithName(component),
		component: component,
	}
}

// Info logs an info message with structured key-value pairs
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Info(msg, keysAndValues...)
}

// Error logs an error message with structured key-value pairs
func (l *Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.logger.Error(err, msg, keysAndValues...)
}

// Debug logs a debug message (only shown if debug logging is enabled)
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.V(1).Info(msg, keysAndValues...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	// Warnings are logged as info with a warning prefix
	l.logger.Info("WARNING: "+msg, keysAndValues...)
}

// WithValues returns a new logger with additional key-value pairs
func (l *Logger) WithValues(keysAndValues ...interface{}) *Logger {
	return &Logger{
		logger:    l.logger.WithValues(keysAndValues...),
		component: l.component,
	}
}

// WithName returns a new logger with an additional name segment
func (l *Logger) WithName(name string) *Logger {
	return &Logger{
		logger:    l.logger.WithName(name),
		component: l.component + "." + name,
	}
}

// GetComponent returns the component name
func (l *Logger) GetComponent() string {
	return l.component
}

// Global component loggers for common use cases
var (
	// ProviderLogger is the base logger for provider components
	ProviderLogger = NewLogger("provider")
	// ControllerLogger is the base logger for controller components
	ControllerLogger = NewLogger("controller")
	// ClientLogger is the base logger for client components
	ClientLogger = NewLogger("client")
	// CacheLogger is the base logger for cache components
	CacheLogger = NewLogger("cache")
)

// Component-specific loggers
func InstanceLogger() *Logger {
	return ProviderLogger.WithName("instance")
}

func PricingLogger() *Logger {
	return ProviderLogger.WithName("pricing")
}

func SubnetLogger() *Logger {
	return ProviderLogger.WithName("subnet")
}

func InstanceTypeLogger() *Logger {
	return ProviderLogger.WithName("instancetype")
}

func BootstrapLogger() *Logger {
	return ProviderLogger.WithName("bootstrap")
}

func VPCLogger() *Logger {
	return ClientLogger.WithName("vpc")
}

func IAMLogger() *Logger {
	return ClientLogger.WithName("iam")
}

func IKSLogger() *Logger {
	return ClientLogger.WithName("iks")
}
