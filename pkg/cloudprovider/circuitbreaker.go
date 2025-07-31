package cloudprovider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// CircuitBreakerState represents the state of the circuit breaker
type CircuitBreakerState string

const (
	// CircuitBreakerClosed - Normal operation, allowing requests
	CircuitBreakerClosed CircuitBreakerState = "CLOSED"
	// CircuitBreakerOpen - Failing fast, blocking requests
	CircuitBreakerOpen CircuitBreakerState = "OPEN"
	// CircuitBreakerHalfOpen - Testing if the issue is resolved
	CircuitBreakerHalfOpen CircuitBreakerState = "HALF_OPEN"
)

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening the circuit
	FailureThreshold int
	// FailureWindow is the time window for counting failures
	FailureWindow time.Duration
	// RecoveryTimeout is how long to wait before attempting recovery
	RecoveryTimeout time.Duration
	// HalfOpenMaxRequests is max requests allowed in half-open state
	HalfOpenMaxRequests int
	// RateLimitPerMinute is max instances created per minute
	RateLimitPerMinute int
	// MaxConcurrentInstances is max instances being created simultaneously
	MaxConcurrentInstances int
}

// DefaultCircuitBreakerConfig returns sensible defaults for IBM Cloud
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:       3,                // 3 consecutive failures
		FailureWindow:          5 * time.Minute,  // Within 5 minutes
		RecoveryTimeout:        15 * time.Minute, // Wait 15 minutes before retry
		HalfOpenMaxRequests:    2,                // Allow 2 test requests
		RateLimitPerMinute:     2,                // Max 2 instances/minute
		MaxConcurrentInstances: 5,                // Max 5 concurrent provisions
	}
}

// FailureRecord tracks individual failures
type FailureRecord struct {
	Timestamp time.Time
	Error     string
	NodeClass string
	Region    string
}

// CircuitBreaker implements circuit breaker pattern for instance provisioning
type CircuitBreaker struct {
	config   *CircuitBreakerConfig
	logger   logr.Logger
	mu       sync.RWMutex
	state    CircuitBreakerState
	failures []FailureRecord

	// State tracking
	lastStateChange     time.Time
	halfOpenRequests    int
	concurrentInstances int
	instancesThisMinute int
	lastMinuteReset     time.Time
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(config *CircuitBreakerConfig, logger logr.Logger) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		config:          config,
		logger:          logger,
		state:           CircuitBreakerClosed,
		failures:        make([]FailureRecord, 0),
		lastStateChange: time.Now(),
		lastMinuteReset: time.Now(),
	}
}

// CanProvision checks if provisioning is allowed based on circuit breaker state
func (cb *CircuitBreaker) CanProvision(ctx context.Context, nodeClass, region string, _ float64) error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Reset time-based counters
	cb.resetCountersIfNeeded()

	// Check circuit breaker state
	switch cb.state {
	case CircuitBreakerOpen:
		if time.Since(cb.lastStateChange) >= cb.config.RecoveryTimeout {
			cb.transitionToHalfOpen()
		} else {
			return &CircuitBreakerError{
				State:      cb.state,
				Message:    "Circuit breaker is OPEN - provisioning blocked due to recent failures",
				TimeToWait: cb.config.RecoveryTimeout - time.Since(cb.lastStateChange),
			}
		}

	case CircuitBreakerHalfOpen:
		if cb.halfOpenRequests >= cb.config.HalfOpenMaxRequests {
			return &CircuitBreakerError{
				State:   cb.state,
				Message: "Circuit breaker is HALF_OPEN - maximum test requests exceeded",
			}
		}
	}

	// Check rate limiting
	if cb.instancesThisMinute >= cb.config.RateLimitPerMinute {
		return &RateLimitError{
			Limit:       cb.config.RateLimitPerMinute,
			Current:     cb.instancesThisMinute,
			TimeToReset: time.Minute - time.Since(cb.lastMinuteReset),
		}
	}

	// Check concurrent instance limit
	if cb.concurrentInstances >= cb.config.MaxConcurrentInstances {
		return &ConcurrencyLimitError{
			Limit:   cb.config.MaxConcurrentInstances,
			Current: cb.concurrentInstances,
		}
	}

	// All checks passed - increment counters
	cb.instancesThisMinute++
	cb.concurrentInstances++

	if cb.state == CircuitBreakerHalfOpen {
		cb.halfOpenRequests++
	}

	cb.logger.Info("Provisioning allowed",
		"nodeClass", nodeClass,
		"region", region,
		"state", cb.state,
		"instancesThisMinute", cb.instancesThisMinute,
		"concurrentInstances", cb.concurrentInstances)

	return nil
}

// RecordSuccess records a successful provisioning operation
func (cb *CircuitBreaker) RecordSuccess(nodeClass, region string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.concurrentInstances > 0 {
		cb.concurrentInstances--
	}

	if cb.state == CircuitBreakerHalfOpen {
		// Successful operation in half-open state - close the circuit
		cb.transitionToClosed()
		cb.logger.Info("Circuit breaker transitioned to CLOSED after successful operation",
			"nodeClass", nodeClass, "region", region)
	}

	cb.logger.Info("Provisioning success recorded",
		"nodeClass", nodeClass,
		"region", region,
		"state", cb.state,
		"concurrentInstances", cb.concurrentInstances)
}

// RecordFailure records a failed provisioning operation
func (cb *CircuitBreaker) RecordFailure(nodeClass, region string, err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.concurrentInstances > 0 {
		cb.concurrentInstances--
	}

	// Add failure record
	failure := FailureRecord{
		Timestamp: time.Now(),
		Error:     err.Error(),
		NodeClass: nodeClass,
		Region:    region,
	}
	cb.failures = append(cb.failures, failure)

	// Clean old failures outside the window
	cb.cleanOldFailures()

	// Check if we should open the circuit
	recentFailures := cb.countRecentFailures()

	cb.logger.Info("Provisioning failure recorded",
		"nodeClass", nodeClass,
		"region", region,
		"error", err.Error(),
		"recentFailures", recentFailures,
		"threshold", cb.config.FailureThreshold,
		"state", cb.state)

	if recentFailures >= cb.config.FailureThreshold {
		if cb.state != CircuitBreakerOpen {
			cb.transitionToOpen()
			cb.logger.Error(fmt.Errorf("circuit breaker opened due to failure threshold exceeded"), "Circuit breaker OPENED",
				"failures", recentFailures,
				"threshold", cb.config.FailureThreshold,
				"nodeClass", nodeClass,
				"region", region)
		}
	} else if cb.state == CircuitBreakerHalfOpen {
		// Failure in half-open state - go back to open
		cb.transitionToOpen()
		cb.logger.Info("Circuit breaker returned to OPEN due to failure in HALF_OPEN state")
	}
}

// GetState returns the current circuit breaker state and statistics
func (cb *CircuitBreaker) GetState() (*CircuitBreakerStatus, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return &CircuitBreakerStatus{
		State:               cb.state,
		RecentFailures:      cb.countRecentFailures(),
		FailureThreshold:    cb.config.FailureThreshold,
		InstancesThisMinute: cb.instancesThisMinute,
		RateLimit:           cb.config.RateLimitPerMinute,
		ConcurrentInstances: cb.concurrentInstances,
		MaxConcurrent:       cb.config.MaxConcurrentInstances,
		LastStateChange:     cb.lastStateChange,
		TimeToRecovery:      cb.getTimeToRecovery(),
	}, nil
}

// Helper methods

func (cb *CircuitBreaker) resetCountersIfNeeded() {
	now := time.Now()

	// Reset minute counter
	if now.Sub(cb.lastMinuteReset) >= time.Minute {
		cb.instancesThisMinute = 0
		cb.lastMinuteReset = now
	}
}

func (cb *CircuitBreaker) transitionToClosed() {
	cb.state = CircuitBreakerClosed
	cb.lastStateChange = time.Now()
	cb.halfOpenRequests = 0
	// Keep failures for historical analysis but don't clear them
}

func (cb *CircuitBreaker) transitionToOpen() {
	cb.state = CircuitBreakerOpen
	cb.lastStateChange = time.Now()
	cb.halfOpenRequests = 0
}

func (cb *CircuitBreaker) transitionToHalfOpen() {
	cb.state = CircuitBreakerHalfOpen
	cb.lastStateChange = time.Now()
	cb.halfOpenRequests = 0
}

func (cb *CircuitBreaker) cleanOldFailures() {
	cutoff := time.Now().Add(-cb.config.FailureWindow)
	validFailures := make([]FailureRecord, 0, len(cb.failures))

	for _, failure := range cb.failures {
		if failure.Timestamp.After(cutoff) {
			validFailures = append(validFailures, failure)
		}
	}

	cb.failures = validFailures
}

func (cb *CircuitBreaker) countRecentFailures() int {
	cutoff := time.Now().Add(-cb.config.FailureWindow)
	count := 0
	for _, failure := range cb.failures {
		if failure.Timestamp.After(cutoff) {
			count++
		}
	}
	return count
}

func (cb *CircuitBreaker) getTimeToRecovery() time.Duration {
	if cb.state != CircuitBreakerOpen {
		return 0
	}
	elapsed := time.Since(cb.lastStateChange)
	if elapsed >= cb.config.RecoveryTimeout {
		return 0
	}
	return cb.config.RecoveryTimeout - elapsed
}

// Error types

// CircuitBreakerError is returned when the circuit breaker blocks a request
type CircuitBreakerError struct {
	State      CircuitBreakerState
	Message    string
	TimeToWait time.Duration
}

func (e *CircuitBreakerError) Error() string {
	if e.TimeToWait > 0 {
		return fmt.Sprintf("circuit breaker %s: %s (retry in %v)", e.State, e.Message, e.TimeToWait)
	}
	return fmt.Sprintf("circuit breaker %s: %s", e.State, e.Message)
}

// RateLimitError is returned when rate limit is exceeded
type RateLimitError struct {
	Limit       int
	Current     int
	TimeToReset time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: %d/%d instances this minute (reset in %v)",
		e.Current, e.Limit, e.TimeToReset)
}

// ConcurrencyLimitError is returned when too many concurrent operations
type ConcurrencyLimitError struct {
	Limit   int
	Current int
}

func (e *ConcurrencyLimitError) Error() string {
	return fmt.Sprintf("concurrency limit exceeded: %d/%d concurrent instances", e.Current, e.Limit)
}

// CircuitBreakerStatus holds the current status of the circuit breaker
type CircuitBreakerStatus struct {
	State               CircuitBreakerState `json:"state"`
	RecentFailures      int                 `json:"recentFailures"`
	FailureThreshold    int                 `json:"failureThreshold"`
	InstancesThisMinute int                 `json:"instancesThisMinute"`
	RateLimit           int                 `json:"rateLimit"`
	ConcurrentInstances int                 `json:"concurrentInstances"`
	MaxConcurrent       int                 `json:"maxConcurrent"`
	LastStateChange     time.Time           `json:"lastStateChange"`
	TimeToRecovery      time.Duration       `json:"timeToRecovery"`
}
