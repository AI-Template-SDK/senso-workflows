package chatgpt

// ChatGPT-specific API request structures

// Request is the payload structure for ChatGPT batch jobs via BrightData
type Request struct {
	Input []Input `json:"input"`
}

// Input represents a single query in a batch request
type Input struct {
	URL              string `json:"url"`
	Prompt           string `json:"prompt"`
	Country          string `json:"country"`
	WebSearch        bool   `json:"web_search"`
	Index            int    `json:"index"`
	AdditionalPrompt string `json:"additional_prompt"`
}

// Result represents the response for a single query from BrightData
type Result struct {
	URL                string      `json:"url"`
	Prompt             string      `json:"prompt"`
	Citations          interface{} `json:"citations"`
	Country            string      `json:"country"`
	AnswerTextMarkdown string      `json:"answer_text_markdown"`
	WebSearchTriggered bool        `json:"web_search_triggered"`
	Index              int         `json:"index"`
	Error              string      `json:"error,omitempty"`
	Input              *InputEcho  `json:"input,omitempty"` // Echoed back on errors
}

// InputEcho is the echoed input for error results
type InputEcho struct {
	URL              string `json:"url"`
	Prompt           string `json:"prompt"`
	Country          string `json:"country"`
	Index            int    `json:"index"`
	WebSearch        bool   `json:"web_search"`
	AdditionalPrompt string `json:"additional_prompt"`
}
