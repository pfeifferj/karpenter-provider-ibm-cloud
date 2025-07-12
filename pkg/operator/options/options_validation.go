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
package options

import (
	"fmt"
)

// Validate checks if all required options are properly set
func (o *Options) Validate() error {
	var missingFields []string

	if o.APIKey == "" {
		missingFields = append(missingFields, "IBMCLOUD_API_KEY")
	}
	if o.Region == "" {
		missingFields = append(missingFields, "IBMCLOUD_REGION")
	}
	if o.Zone == "" {
		missingFields = append(missingFields, "IBMCLOUD_ZONE")
	}
	if o.ResourceGroupID == "" {
		missingFields = append(missingFields, "IBMCLOUD_RESOURCE_GROUP_ID")
	}

	if len(missingFields) > 0 {
		return fmt.Errorf("missing required environment variables: %v", missingFields)
	}

	return validateRegionZonePair(o.Region, o.Zone)
}

// validateRegionZonePair ensures the zone is valid for the given region
func validateRegionZonePair(region, zone string) error {
	// Map of valid zones for each region
	validZones := map[string][]string{
		"us-south": {"us-south-1", "us-south-2", "us-south-3"},
		"us-east":  {"us-east-1", "us-east-2", "us-east-3"},
		"eu-gb":    {"eu-gb-1", "eu-gb-2", "eu-gb-3"},
		"eu-de":    {"eu-de-1", "eu-de-2", "eu-de-3"},
		"jp-tok":   {"jp-tok-1", "jp-tok-2", "jp-tok-3"},
		"au-syd":   {"au-syd-1", "au-syd-2", "au-syd-3"},
		"ca-tor":   {"ca-tor-1", "ca-tor-2", "ca-tor-3"},
		"br-sao":   {"br-sao-1", "br-sao-2", "br-sao-3"},
	}

	zones, exists := validZones[region]
	if !exists {
		return fmt.Errorf("invalid region: %s", region)
	}

	for _, validZone := range zones {
		if zone == validZone {
			return nil
		}
	}

	return fmt.Errorf("invalid zone %s for region %s", zone, region)
}
