package common

import "encoding/json"

// IsStatusResponse checks if the response body is a status object rather than results
func IsStatusResponse(bodyBytes []byte) (bool, string, string) {
	var statusResp StatusResponse

	if err := json.Unmarshal(bodyBytes, &statusResp); err != nil {
		return false, "", ""
	}

	// If it has a status field, it's a status response
	if statusResp.Status != "" {
		return true, statusResp.Status, statusResp.Message
	}

	return false, "", ""
}

// Min returns the smaller of two integers
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
