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

// SearchRequest is the POST /search request body. Field names mirror the
// Perplexity wire contract exactly; omitempty keeps optional filters out of
// the JSON when unset so the API sees a minimal request.
type SearchRequest struct {
	Query          string   `json:"query"`
	MaxResults     int      `json:"max_results,omitempty"`
	Country        string   `json:"country,omitempty"`
	Language       []string `json:"search_language_filter,omitempty"`
	DomainFilter   []string `json:"search_domain_filter,omitempty"`
	RecencyFilter  string   `json:"search_recency_filter,omitempty"`
	SearchAfter    string   `json:"search_after_date,omitempty"`
	SearchBefore   string   `json:"search_before_date,omitempty"`
}

// SearchResponse is the POST /search response envelope. Unknown fields are
// ignored. The API returns `date` + `last_updated`; we surface `date` to
// consumers as `published_date` in the command-level output shape.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// SearchResult is one hit from POST /search on the wire.
type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Date        string `json:"date"`
	LastUpdated string `json:"last_updated"`
}

// AsyncRequest is the body for POST /async/chat/completions. The Perplexity
// async endpoint nests the chat-completions request under "request"; the
// outer object only carries that field today, but is kept distinct from
// Request so future async-only knobs (webhooks, callbacks) can be added
// without disturbing the synchronous chat path.
type AsyncRequest struct {
	Request AsyncChatRequest `json:"request"`
}

// AsyncChatRequest mirrors the per-job chat-completions payload sent to
// /async/chat/completions. ReasoningEffort is sonar-deep-research-only and
// omitted on the wire when empty.
type AsyncChatRequest struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	MaxTokens       int       `json:"max_tokens,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
}

// AsyncJob is the response shape for both the submit (POST) and poll (GET)
// endpoints. On submit the server returns id+status+model+created_at; on
// poll the response object fills in once status=COMPLETED. failed_at and
// error_message land on FAILED.
type AsyncJob struct {
	ID           string    `json:"id"`
	Model        string    `json:"model"`
	Status       string    `json:"status"`
	CreatedAt    int64     `json:"created_at"`
	StartedAt    int64     `json:"started_at,omitempty"`
	CompletedAt  int64     `json:"completed_at,omitempty"`
	FailedAt     int64     `json:"failed_at,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Response     *Response `json:"response,omitempty"`
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
