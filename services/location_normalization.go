package services

import (
	"strings"

	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

type normalizedLocation struct {
	CountryCode string
	CountryName string
	Region      *string
	City        *string
}

func normalizeLocation(location *workflowModels.Location) normalizedLocation {
	if location == nil {
		return normalizedLocation{
			CountryCode: "US",
			CountryName: "United States",
			Region:      nil,
			City:        nil,
		}
	}

	code := normalizeCountryCode(location.Country)
	region := cleanOptionalString(location.Region)
	city := cleanOptionalString(location.City)

	name := countryCodeToName(code)
	if name == "" {
		name = code
	}

	return normalizedLocation{
		CountryCode: code,
		CountryName: name,
		Region:      region,
		City:        city,
	}
}

func normalizeCountryCode(code string) string {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	if normalized == "" {
		normalized = "US"
	}
	if normalized == "UK" {
		normalized = "GB"
	}
	return normalized
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func formatLocationForPrompt(location *workflowModels.Location) string {
	normalized := normalizeLocation(location)
	parts := []string{}
	if normalized.City != nil && *normalized.City != "" {
		parts = append(parts, *normalized.City)
	}
	if normalized.Region != nil && *normalized.Region != "" {
		parts = append(parts, *normalized.Region)
	}
	if normalized.CountryName != "" {
		parts = append(parts, normalized.CountryName)
	}

	if len(parts) == 0 {
		return "United States"
	}

	return strings.Join(parts, ", ")
}

func countryCodeToName(code string) string {
	switch code {
	case "US":
		return "United States"
	case "CA":
		return "Canada"
	case "GB":
		return "United Kingdom"
	case "IE":
		return "Ireland"
	case "AU":
		return "Australia"
	case "NZ":
		return "New Zealand"
	case "DE":
		return "Germany"
	case "FR":
		return "France"
	case "ES":
		return "Spain"
	case "PT":
		return "Portugal"
	case "IT":
		return "Italy"
	case "NL":
		return "Netherlands"
	case "BE":
		return "Belgium"
	case "CH":
		return "Switzerland"
	case "AT":
		return "Austria"
	case "SE":
		return "Sweden"
	case "NO":
		return "Norway"
	case "DK":
		return "Denmark"
	case "FI":
		return "Finland"
	case "IS":
		return "Iceland"
	case "PL":
		return "Poland"
	case "CZ":
		return "Czech Republic"
	case "SK":
		return "Slovakia"
	case "HU":
		return "Hungary"
	case "RO":
		return "Romania"
	case "BG":
		return "Bulgaria"
	case "GR":
		return "Greece"
	case "TR":
		return "Turkey"
	case "RU":
		return "Russia"
	case "UA":
		return "Ukraine"
	case "IL":
		return "Israel"
	case "AE":
		return "United Arab Emirates"
	case "SA":
		return "Saudi Arabia"
	case "QA":
		return "Qatar"
	case "KW":
		return "Kuwait"
	case "OM":
		return "Oman"
	case "IN":
		return "India"
	case "PK":
		return "Pakistan"
	case "BD":
		return "Bangladesh"
	case "SG":
		return "Singapore"
	case "MY":
		return "Malaysia"
	case "TH":
		return "Thailand"
	case "VN":
		return "Vietnam"
	case "PH":
		return "Philippines"
	case "ID":
		return "Indonesia"
	case "JP":
		return "Japan"
	case "KR":
		return "South Korea"
	case "TW":
		return "Taiwan"
	case "CN":
		return "China"
	case "HK":
		return "Hong Kong"
	case "BR":
		return "Brazil"
	case "AR":
		return "Argentina"
	case "CL":
		return "Chile"
	case "CO":
		return "Colombia"
	case "PE":
		return "Peru"
	case "MX":
		return "Mexico"
	case "ZA":
		return "South Africa"
	case "EG":
		return "Egypt"
	case "NG":
		return "Nigeria"
	case "KE":
		return "Kenya"
	}
	return ""
}
