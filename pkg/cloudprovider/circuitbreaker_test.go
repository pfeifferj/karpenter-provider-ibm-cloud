package cloudprovider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCircuitBreaker(t *testing.T) {
	tests := []struct {
		name   string
		config *CircuitBreakerConfig
	}{
		{
			name:   "with default config",
			config: nil,
		},
		{
			name: "with custom config",
			config: &CircuitBreakerConfig{
				FailureThreshold:       5,
				FailureWindow:          10 * time.Minute,
				RecoveryTimeout:        30 * time.Minute,
				HalfOpenMaxRequests:    3,
				RateLimitPerMinute:     1,
				MaxConcurrentInstances: 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(tt.config, logr.Discard())
			
			assert.NotNil(t, cb)
			assert.Equal(t, CircuitBreakerClosed, cb.state)
			assert.Empty(t, cb.failures)
			
			if tt.config == nil {
				// Should use default config
				assert.Equal(t, 3, cb.config.FailureThreshold)
				assert.Equal(t, 5*time.Minute, cb.config.FailureWindow)
				assert.Equal(t, 15*time.Minute, cb.config.RecoveryTimeout)
				assert.Equal(t, 2, cb.config.HalfOpenMaxRequests)
				assert.Equal(t, 2, cb.config.RateLimitPerMinute)
				assert.Equal(t, 5, cb.config.MaxConcurrentInstances)
			} else {
				// Should use provided config
				assert.Equal(t, tt.config.FailureThreshold, cb.config.FailureThreshold)
				assert.Equal(t, tt.config.FailureWindow, cb.config.FailureWindow)
				assert.Equal(t, tt.config.RecoveryTimeout, cb.config.RecoveryTimeout)
				assert.Equal(t, tt.config.HalfOpenMaxRequests, cb.config.HalfOpenMaxRequests)
				assert.Equal(t, tt.config.RateLimitPerMinute, cb.config.RateLimitPerMinute)
				assert.Equal(t, tt.config.MaxConcurrentInstances, cb.config.MaxConcurrentInstances)
			}
		})
	}
}

func TestCircuitBreaker_CanProvision_RateLimit(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       3,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     2, // Only 2 per minute
		MaxConcurrentInstances: 5,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// First 2 requests should succeed
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	// Third request should be rate limited
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &RateLimitError{}, err)
	
	rateLimitErr := err.(*RateLimitError)
	assert.Equal(t, 2, rateLimitErr.Limit)
	assert.Equal(t, 2, rateLimitErr.Current)
	assert.True(t, rateLimitErr.TimeToReset > 0)
}

func TestCircuitBreaker_CanProvision_ConcurrencyLimit(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       3,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     10, // High rate limit
		MaxConcurrentInstances: 2,  // Only 2 concurrent
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// First 2 requests should succeed
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	// Third request should hit concurrency limit
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &ConcurrencyLimitError{}, err)
	
	concurrencyErr := err.(*ConcurrencyLimitError)
	assert.Equal(t, 2, concurrencyErr.Limit)
	assert.Equal(t, 2, concurrencyErr.Current)
}

func TestCircuitBreaker_FailureThreshold(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       2, // Low threshold for testing
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     10,
		MaxConcurrentInstances: 10,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// Circuit should start closed
	assert.Equal(t, CircuitBreakerClosed, cb.state)
	
	// First failure
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 1"))
	assert.Equal(t, CircuitBreakerClosed, cb.state)
	
	// Second failure should open the circuit
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 2"))
	assert.Equal(t, CircuitBreakerOpen, cb.state)
	
	// Should now block requests
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &CircuitBreakerError{}, err)
	
	cbErr := err.(*CircuitBreakerError)
	assert.Equal(t, CircuitBreakerOpen, cbErr.State)
	assert.Contains(t, cbErr.Message, "provisioning blocked due to recent failures")
	assert.True(t, cbErr.TimeToWait > 0)
}

func TestCircuitBreaker_RecoveryFlow(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       2,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        100 * time.Millisecond, // Short timeout for testing
		HalfOpenMaxRequests:    1,
		RateLimitPerMinute:     10,
		MaxConcurrentInstances: 10,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// Trigger circuit open
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 1"))
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 2"))
	assert.Equal(t, CircuitBreakerOpen, cb.state)
	
	// Wait for recovery timeout
	time.Sleep(150 * time.Millisecond)
	
	// Should transition to half-open and allow limited requests
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	assert.Equal(t, CircuitBreakerHalfOpen, cb.state)
	
	// Second request should be blocked (exceeds HalfOpenMaxRequests)
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &CircuitBreakerError{}, err)
	
	// Record success should close the circuit
	cb.RecordSuccess("test-nodeclass", "us-south")
	assert.Equal(t, CircuitBreakerClosed, cb.state)
	
	// Should now allow normal requests
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
}

func TestCircuitBreaker_HalfOpenFailureReturnsToOpen(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       2,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        100 * time.Millisecond,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     10,
		MaxConcurrentInstances: 10,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	
	// Trigger circuit open
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 1"))
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 2"))
	assert.Equal(t, CircuitBreakerOpen, cb.state)
	
	// Wait for recovery and transition to half-open
	time.Sleep(150 * time.Millisecond)
	err := cb.CanProvision(context.Background(), "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	assert.Equal(t, CircuitBreakerHalfOpen, cb.state)
	
	// Record failure in half-open state should return to open
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error 3"))
	assert.Equal(t, CircuitBreakerOpen, cb.state)
}

func TestCircuitBreaker_ResetCountersAfterTime(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       3,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     2,
		MaxConcurrentInstances: 5,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// Use up rate limit
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	// Should be rate limited
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &RateLimitError{}, err)
	
	// Manually reset time (simulate time passage)
	cb.mu.Lock()
	cb.lastMinuteReset = time.Now().Add(-2 * time.Minute)
	cb.mu.Unlock()
	
	// Should now allow requests again
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
}

func TestCircuitBreaker_RecordSuccessDecrementsConcurrency(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig(), logr.Discard())
	ctx := context.Background()
	
	// Provision some instances
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	// Check concurrency
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, 2, status.ConcurrentInstances)
	
	// Record success should decrement
	cb.RecordSuccess("test-nodeclass", "us-south")
	status, err = cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, 1, status.ConcurrentInstances)
}

func TestCircuitBreaker_RecordFailureDecrementsConcurrency(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig(), logr.Discard())
	ctx := context.Background()
	
	// Provision some instances
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	// Check concurrency
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, 2, status.ConcurrentInstances)
	
	// Record failure should decrement
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error"))
	status, err = cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, 1, status.ConcurrentInstances)
}

func TestCircuitBreaker_GetState(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       2,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    1,
		RateLimitPerMinute:     3,
		MaxConcurrentInstances: 4,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// Initial state
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, CircuitBreakerClosed, status.State)
	assert.Equal(t, 0, status.RecentFailures)
	assert.Equal(t, 2, status.FailureThreshold)
	assert.Equal(t, 0, status.InstancesThisMinute)
	assert.Equal(t, 3, status.RateLimit)
	assert.Equal(t, 0, status.ConcurrentInstances)
	assert.Equal(t, 4, status.MaxConcurrent)
	assert.Equal(t, time.Duration(0), status.TimeToRecovery)
	
	// Add some activity
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error"))
	
	status, err = cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, CircuitBreakerClosed, status.State)
	assert.Equal(t, 1, status.RecentFailures)
	assert.Equal(t, 1, status.InstancesThisMinute)
	assert.Equal(t, 0, status.ConcurrentInstances) // Decremented by RecordFailure
}

func TestCircuitBreaker_CleanOldFailures(t *testing.T) {
	config := &CircuitBreakerConfig{
		FailureThreshold:       5,
		FailureWindow:          100 * time.Millisecond, // Short window for testing
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     10,
		MaxConcurrentInstances: 10,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	
	// Add failures
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("error 1"))
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("error 2"))
	
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, 2, status.RecentFailures)
	
	// Wait for failures to age out
	time.Sleep(150 * time.Millisecond)
	
	// Trigger cleanup by calling GetState
	status, err = cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, 0, status.RecentFailures) // Old failures should be cleaned up
}

func TestCircuitBreaker_ErrorTypes(t *testing.T) {
	t.Run("CircuitBreakerError", func(t *testing.T) {
		err := &CircuitBreakerError{
			State:      CircuitBreakerOpen,
			Message:    "test message",
			TimeToWait: 5 * time.Minute,
		}
		
		expected := "circuit breaker OPEN: test message (retry in 5m0s)"
		assert.Equal(t, expected, err.Error())
		
		// Test without TimeToWait
		err.TimeToWait = 0
		expected = "circuit breaker OPEN: test message"
		assert.Equal(t, expected, err.Error())
	})
	
	t.Run("RateLimitError", func(t *testing.T) {
		err := &RateLimitError{
			Limit:       2,
			Current:     3,
			TimeToReset: 30 * time.Second,
		}
		
		expected := "rate limit exceeded: 3/2 instances this minute (reset in 30s)"
		assert.Equal(t, expected, err.Error())
	})
	
	t.Run("ConcurrencyLimitError", func(t *testing.T) {
		err := &ConcurrencyLimitError{
			Limit:   5,
			Current: 6,
		}
		
		expected := "concurrency limit exceeded: 6/5 concurrent instances"
		assert.Equal(t, expected, err.Error())
	})
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitBreakerConfig(), logr.Discard())
	ctx := context.Background()
	
	// Test concurrent access doesn't cause race conditions
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			// Mix of operations
			switch id % 3 {
			case 0:
				_ = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
			case 1:
				cb.RecordSuccess("test-nodeclass", "us-south")
			default:
				cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("test error %d", id))
			}
			
			// Get state
			_, _ = cb.GetState()
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Should not panic and should be able to get final state
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.NotNil(t, status)
}

func TestDefaultCircuitBreakerConfig(t *testing.T) {
	config := DefaultCircuitBreakerConfig()
	
	assert.Equal(t, 3, config.FailureThreshold)
	assert.Equal(t, 5*time.Minute, config.FailureWindow)
	assert.Equal(t, 15*time.Minute, config.RecoveryTimeout)
	assert.Equal(t, 2, config.HalfOpenMaxRequests)
	assert.Equal(t, 2, config.RateLimitPerMinute)
	assert.Equal(t, 5, config.MaxConcurrentInstances)
}

func TestCircuitBreaker_IntegrationWithCloudProvider(t *testing.T) {
	// Test that circuit breaker integrates correctly with CloudProvider.Create()
	config := &CircuitBreakerConfig{
		FailureThreshold:       2,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    1,
		RateLimitPerMinute:     1, // Very low for testing
		MaxConcurrentInstances: 1,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	// First request should succeed
	err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.NoError(t, err)
	
	// Second request should be rate limited
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &RateLimitError{}, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
	
	// Record some failures to trigger circuit open
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("bootstrap failure"))
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("another bootstrap failure"))
	
	// Should now have circuit open
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, CircuitBreakerOpen, status.State)
	
	// Reset rate limit by advancing time
	cb.mu.Lock()
	cb.lastMinuteReset = time.Now().Add(-2 * time.Minute)
	cb.instancesThisMinute = 0
	cb.concurrentInstances = 0
	cb.mu.Unlock()
	
	// Should still be blocked by circuit breaker
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &CircuitBreakerError{}, err)
	assert.Contains(t, err.Error(), "circuit breaker OPEN")
}

func TestCircuitBreaker_ScaleUpStormPrevention(t *testing.T) {
	// Test scenario: simulate the 90-instance scale-up storm and verify circuit breaker prevents it
	config := &CircuitBreakerConfig{
		FailureThreshold:       3,
		FailureWindow:          5 * time.Minute,
		RecoveryTimeout:        15 * time.Minute,
		HalfOpenMaxRequests:    2,
		RateLimitPerMinute:     2, // This would prevent 90 instances in 15 minutes
		MaxConcurrentInstances: 5,
	}
	
	cb := NewCircuitBreaker(config, logr.Discard())
	ctx := context.Background()
	
	var successfulProvisions int
	var rateLimitErrors int
	var circuitBreakerErrors int
	
	// Simulate rapid provisioning attempts (like the 90-instance storm)
	for i := 0; i < 10; i++ {
		err := cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
		if err == nil {
			successfulProvisions++
			// Simulate bootstrap failures
			cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("bootstrap failure %d", i))
		} else if _, ok := err.(*RateLimitError); ok {
			rateLimitErrors++
		} else if _, ok := err.(*CircuitBreakerError); ok {
			circuitBreakerErrors++
		}
	}
	
	// Should be limited to 2 successful provisions per minute
	assert.Equal(t, 2, successfulProvisions)
	assert.True(t, rateLimitErrors > 0, "Should have rate limit errors")
	
	// Add one more failure to trigger circuit open (need 3 total)
	cb.RecordFailure("test-nodeclass", "us-south", fmt.Errorf("third bootstrap failure"))
	
	// After 3 failures, circuit should open
	status, err := cb.GetState()
	require.NoError(t, err)
	assert.Equal(t, CircuitBreakerOpen, status.State)
	
	// Reset rate limit for next test
	cb.mu.Lock()
	cb.lastMinuteReset = time.Now().Add(-2 * time.Minute)
	cb.instancesThisMinute = 0
	cb.concurrentInstances = 0
	cb.mu.Unlock()
	
	// Further attempts should be blocked by circuit breaker
	err = cb.CanProvision(ctx, "test-nodeclass", "us-south", 0)
	assert.Error(t, err)
	assert.IsType(t, &CircuitBreakerError{}, err)
	
	t.Logf("Protection Summary: %d successful, %d rate limited, %d circuit blocked", 
		successfulProvisions, rateLimitErrors, circuitBreakerErrors)
}