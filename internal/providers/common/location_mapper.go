package common

import (
	"strings"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
)

// MapLocationToCountry maps a location to a BrightData country code
func MapLocationToCountry(location *models.Location) string {
	if location == nil {
		return "US" // Default to US
	}

	// Map location.Country to BrightData country codes
	countryMap := map[string]string{
		"US": "US",
		"CA": "CA",
		"GB": "GB",
		"UK": "GB", // Handle UK -> GB mapping
		"AU": "AU",
		"DE": "DE",
		"FR": "FR",
		"IT": "IT",
		"ES": "ES",
		"NL": "NL",
		"JP": "JP",
		"KR": "KR",
		"IN": "IN",
		"BR": "BR",
		"MX": "MX",
	}

	if country, exists := countryMap[strings.ToUpper(location.Country)]; exists {
		return country
	}

	// Fallback to US if country not found
	return "US"
}
