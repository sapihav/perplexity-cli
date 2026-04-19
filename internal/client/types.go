package client

import "fmt"

// Request is the chat-completions request body we send to Perplexity.
// Only fields actively consumed by the CLI are exposed; unknowns are fine
// for Perplexity to ignore on the wire.
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

// Message is one turn in the chat-completions messages array.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response is the minimal subset of the Perplexity chat-completions response
// we care about. Unknown fields are ignored by encoding/json.
type Response struct {
	Model     string   `json:"model"`
	Choices   []Choice `json:"choices"`
	Citations []string `json:"citations"`
}

// Choice is one completion choice from the API.
type Choice struct {
	Message Message `json:"message"`
}

// APIError is returned when the upstream API responds with HTTP >= 400
// after retries are exhausted. Body is the raw response body (may be JSON).
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("perplexity api error: status %d: %s", e.Status, e.Body)
}
