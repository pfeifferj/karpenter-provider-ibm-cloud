package cache

import (
	"sync"
	"time"
)

// UnavailableOfferings tracks offerings that are currently unavailable
type UnavailableOfferings struct {
	mu       sync.RWMutex
	offerings map[string]time.Time
}

// NewUnavailableOfferings creates a new UnavailableOfferings cache
func NewUnavailableOfferings() *UnavailableOfferings {
	return &UnavailableOfferings{
		offerings: make(map[string]time.Time),
	}
}

// Add marks an offering as unavailable
func (u *UnavailableOfferings) Add(offeringID string, expiry time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.offerings[offeringID] = expiry
}

// Remove removes an offering from the unavailable list
func (u *UnavailableOfferings) Remove(offeringID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.offerings, offeringID)
}

// IsUnavailable checks if an offering is marked as unavailable
func (u *UnavailableOfferings) IsUnavailable(offeringID string) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	expiry, exists := u.offerings[offeringID]
	if !exists {
		return false
	}
	if time.Now().After(expiry) {
		delete(u.offerings, offeringID)
		return false
	}
	return true
}

// Cleanup removes expired entries
func (u *UnavailableOfferings) Cleanup() {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	for id, expiry := range u.offerings {
		if now.After(expiry) {
			delete(u.offerings, id)
		}
	}
}
