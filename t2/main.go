package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
)

// Simple struct to match the JSON structure
type Response struct {
	Response string `json:"response"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run main.go <json_file>")
		fmt.Println("Example: go run main.go ../samples/q1.json")
		os.Exit(1)
	}

	filename := os.Args[1]

	// Read the JSON file
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file %s: %v\n", filename, err)
		os.Exit(1)
	}

	// Parse JSON
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== TESTING REGEX ON: %s ===\n", filename)
	fmt.Printf("Response length: %d characters\n\n", len(resp.Response))

	// The current regex from the workflow
	pattern := `https?://www\.[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|https?://[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|www\.[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]\.[^\s,.)}\]]{2,}|[a-zA-Z0-9-]+\.[a-zA-Z]{2,6}\.[^\s,.)}\]]{2,}`

	fmt.Printf("Pattern: %s\n\n", pattern)

	regex, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Printf("❌ ERROR compiling regex: %v\n", err)
		os.Exit(1)
	}

	matches := regex.FindAllString(resp.Response, -1)
	fmt.Printf("Found %d total matches\n", len(matches))

	// Remove duplicates
	seen := make(map[string]bool)
	uniqueMatches := []string{}
	for _, match := range matches {
		if !seen[match] {
			seen[match] = true
			uniqueMatches = append(uniqueMatches, match)
		}
	}

	fmt.Printf("Found %d unique URLs:\n", len(uniqueMatches))
	if len(uniqueMatches) == 0 {
		fmt.Printf("  (no matches found)\n")
	} else {
		for i, match := range uniqueMatches {
			fmt.Printf("  %d. %s\n", i+1, match)
		}
	}

	// Show a snippet of the response for context
	fmt.Println("\n--- RESPONSE PREVIEW (first 300 chars) ---")
	preview := resp.Response
	if len(preview) > 300 {
		preview = preview[:300] + "..."
	}
	fmt.Println(preview)
}
