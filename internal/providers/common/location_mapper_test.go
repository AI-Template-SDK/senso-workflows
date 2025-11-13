package common_test

import (
	"testing"

	"github.com/AI-Template-SDK/senso-workflows/internal/models"
	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

func TestMapLocationToCountry(t *testing.T) {
	tests := []struct {
		name     string
		location *models.Location
		expected string
	}{
		{
			name:     "nil location defaults to US",
			location: nil,
			expected: "US",
		},
		{
			name: "US maps to US",
			location: &models.Location{
				Country: "US",
			},
			expected: "US",
		},
		{
			name: "lowercase us maps to US",
			location: &models.Location{
				Country: "us",
			},
			expected: "US",
		},
		{
			name: "CA maps to CA",
			location: &models.Location{
				Country: "CA",
			},
			expected: "CA",
		},
		{
			name: "GB maps to GB",
			location: &models.Location{
				Country: "GB",
			},
			expected: "GB",
		},
		{
			name: "UK maps to GB",
			location: &models.Location{
				Country: "UK",
			},
			expected: "GB",
		},
		{
			name: "lowercase uk maps to GB",
			location: &models.Location{
				Country: "uk",
			},
			expected: "GB",
		},
		{
			name: "AU maps to AU",
			location: &models.Location{
				Country: "AU",
			},
			expected: "AU",
		},
		{
			name: "DE maps to DE",
			location: &models.Location{
				Country: "DE",
			},
			expected: "DE",
		},
		{
			name: "FR maps to FR",
			location: &models.Location{
				Country: "FR",
			},
			expected: "FR",
		},
		{
			name: "unknown country defaults to US",
			location: &models.Location{
				Country: "ZZ",
			},
			expected: "US",
		},
		{
			name: "empty country defaults to US",
			location: &models.Location{
				Country: "",
			},
			expected: "US",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := common.MapLocationToCountry(tt.location)
			if result != tt.expected {
				t.Errorf("MapLocationToCountry(%v) = %s, want %s", tt.location, result, tt.expected)
			}
		})
	}
}

func TestMapLocationToCountryAllSupportedCountries(t *testing.T) {
	supportedCountries := []string{
		"US", "CA", "GB", "UK", "AU", "DE", "FR", "IT", "ES", "NL", "JP", "KR", "IN", "BR", "MX",
	}

	for _, country := range supportedCountries {
		t.Run(country, func(t *testing.T) {
			location := &models.Location{
				Country: country,
			}
			result := common.MapLocationToCountry(location)

			// Result should not be empty
			if result == "" {
				t.Errorf("MapLocationToCountry(%s) returned empty string", country)
			}

			// UK should map to GB, all others should map to themselves (uppercase)
			if country == "UK" {
				if result != "GB" {
					t.Errorf("UK should map to GB, got %s", result)
				}
			}
		})
	}
}
