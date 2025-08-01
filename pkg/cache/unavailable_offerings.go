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
package cache

import (
	"sync"
	"time"
)

// UnavailableOfferings tracks offerings that are currently unavailable
type UnavailableOfferings struct {
	mu        sync.RWMutex
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
