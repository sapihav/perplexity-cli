// Package client is a thin wrapper over the Perplexity public HTTP APIs.
// It exposes Complete (POST /chat/completions) and Search (POST /search),
// both with retry-with-backoff on 429 and 5xx responses. No hidden state,
// no caches, no file I/O.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultChatEndpoint is the Perplexity chat-completions URL.
const DefaultChatEndpoint = "https://api.perplexity.ai/chat/completions"

// DefaultSearchEndpoint is the Perplexity standalone /search URL.
const DefaultSearchEndpoint = "https://api.perplexity.ai/search"

// DefaultAsyncEndpoint is the Perplexity async chat-completions URL. Submit
// jobs via POST <endpoint>; poll job state via GET <endpoint>/{id}.
const DefaultAsyncEndpoint = "https://api.perplexity.ai/async/chat/completions"

// DefaultEndpoint is the chat-completions URL. Kept as a named alias so
// existing callers (and tests) that constructed clients before the /search
// migration continue to compile. New code should refer to DefaultChatEndpoint.
const DefaultEndpoint = DefaultChatEndpoint

// DefaultUserAgent is sent unless callers override it via WithUserAgent.
const DefaultUserAgent = "perplexity-cli"

// Client calls the Perplexity API. Construct with New.
type Client struct {
	httpClient     *http.Client
	chatEndpoint   string
	searchEndpoint string
	asyncEndpoint  string
	apiKey         string
	userAgent      string
	maxRetries     int
	// backoff returns the duration to sleep before retry attempt n (1-indexed).
	// Exposed for tests; nil means use exponential default (250ms * 2^(n-1)).
	backoff func(attempt int) time.Duration
	// rateLimit is a channel that must receive before each HTTP call. nil = no limit.
	rateLimit <-chan time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (useful in tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithEndpoint overrides the chat-completions API URL (for tests pointing at
// httptest.Server). Retained as an alias for the pre-migration name.
func WithEndpoint(url string) Option {
	return func(c *Client) { c.chatEndpoint = url }
}

// WithSearchEndpoint overrides the /search API URL (for tests).
func WithSearchEndpoint(url string) Option {
	return func(c *Client) { c.searchEndpoint = url }
}

// WithAsyncEndpoint overrides the /async/chat/completions API URL (for tests).
func WithAsyncEndpoint(url string) Option {
	return func(c *Client) { c.asyncEndpoint = url }
}

// WithMaxRetries sets how many times a 429/5xx response is retried. 0 disables retries.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithBackoff overrides the backoff function. Mainly for tests to make them fast.
func WithBackoff(fn func(attempt int) time.Duration) Option {
	return func(c *Client) { c.backoff = fn }
}

// WithTimeout sets the per-request timeout on the underlying http.Client.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// WithUserAgent overrides the User-Agent header sent on every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithRateLimit throttles outgoing requests to n per second. 0 disables.
// A simple ticker-based gate; non-bursty, good enough for a single-shot CLI.
func WithRateLimit(perSec float64) Option {
	return func(c *Client) {
		if perSec <= 0 {
			return
		}
		interval := time.Duration(float64(time.Second) / perSec)
		c.rateLimit = time.Tick(interval)
	}
}

// New constructs a Client. apiKey must be non-empty; callers should check env
// before calling. A zero-value http.Client is used unless WithHTTPClient is passed.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient:     &http.Client{Timeout: 60 * time.Second},
		chatEndpoint:   DefaultChatEndpoint,
		searchEndpoint: DefaultSearchEndpoint,
		asyncEndpoint:  DefaultAsyncEndpoint,
		apiKey:         apiKey,
		userAgent:      DefaultUserAgent,
		maxRetries:     3,
	}
	for _, o := range opts {
		o(c)
	}
	if c.backoff == nil {
		c.backoff = defaultBackoff
	}
	return c
}

// defaultBackoff: 250ms, 500ms, 1s, 2s, ...
func defaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return 250 * time.Millisecond * (1 << (attempt - 1))
}

// Complete sends a chat-completions request and returns the parsed response.
// It retries on 429 and 5xx up to maxRetries times using exponential backoff.
// On final failure it returns *APIError (for HTTP errors) or a plain error (for
// network errors / json parse errors).
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := c.postJSON(ctx, c.chatEndpoint, body)
	if err != nil {
		return nil, err
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// Search sends a POST /search request and returns the parsed response. Same
// retry semantics as Complete; returns *APIError on final HTTP failure.
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal search request: %w", err)
	}
	raw, err := c.postJSON(ctx, c.searchEndpoint, body)
	if err != nil {
		return nil, err
	}
	var out SearchResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return &out, nil
}

// AsyncSubmit POSTs to /async/chat/completions to enqueue a deep-research job.
// Returns the AsyncJob with id+status+model+created_at; the answer is fetched
// later via AsyncGet. Same retry semantics as Complete.
func (c *Client) AsyncSubmit(ctx context.Context, req AsyncRequest) (*AsyncJob, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal async request: %w", err)
	}
	raw, err := c.postJSON(ctx, c.asyncEndpoint, body)
	if err != nil {
		return nil, err
	}
	var out AsyncJob
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode async submit response: %w", err)
	}
	return &out, nil
}

// AsyncGet GETs /async/chat/completions/{id} and returns the AsyncJob with
// its current status. Caller decides whether to poll again. Same retry
// semantics as Complete (429/5xx with backoff).
func (c *Client) AsyncGet(ctx context.Context, id string) (*AsyncJob, error) {
	if id == "" {
		return nil, errors.New("async get: id is required")
	}
	raw, err := c.getJSON(ctx, c.asyncEndpoint+"/"+id)
	if err != nil {
		return nil, err
	}
	var out AsyncJob
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode async get response: %w", err)
	}
	return &out, nil
}

// postJSON runs the shared retry loop against the given endpoint, returning
// the raw body on success or *APIError / network error on final failure.
func (c *Client) postJSON(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	return c.requestJSON(ctx, http.MethodPost, endpoint, body)
}

// getJSON is the GET counterpart to postJSON, sharing the same retry loop and
// error-mapping semantics. Body is omitted on the wire.
func (c *Client) getJSON(ctx context.Context, endpoint string) ([]byte, error) {
	return c.requestJSON(ctx, http.MethodGet, endpoint, nil)
}

// requestJSON is the shared retry+rate-limit core for both POST and GET. It
// returns the raw body on success or *APIError / network error on final
// failure.
func (c *Client) requestJSON(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff(attempt)):
			}
		}
		if c.rateLimit != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-c.rateLimit:
			}
		}

		resp, err := c.do(ctx, method, endpoint, body)
		if err != nil {
			lastErr = err
			if attempt == c.maxRetries {
				return nil, err
			}
			continue
		}

		if resp.status >= 500 || resp.status == http.StatusTooManyRequests {
			lastErr = &APIError{Status: resp.status, Body: string(resp.body)}
			if attempt == c.maxRetries {
				return nil, lastErr
			}
			continue
		}
		if resp.status >= 400 {
			return nil, &APIError{Status: resp.status, Body: string(resp.body)}
		}
		return resp.body, nil
	}

	if lastErr == nil {
		lastErr = errors.New("perplexity: no attempts made")
	}
	return nil, lastErr
}

type rawResponse struct {
	status int
	body   []byte
}

// do performs a single HTTP request to endpoint and reads the full body.
func (c *Client) do(ctx context.Context, method, endpoint string, body []byte) (*rawResponse, error) {
	httpReq, err := c.buildHTTPRequest(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return &rawResponse{status: resp.StatusCode, body: b}, nil
}

func (c *Client) buildHTTPRequest(ctx context.Context, method, endpoint string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if len(body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.userAgent)
	return httpReq, nil
}

// Dump returns a human-readable trace of the chat-completions request that
// Complete would send. The Authorization header is redacted; body is the
// serialized JSON. Intended for --dry-run / --verbose output. No network call.
func (c *Client) Dump(req Request) (string, error) {
	body, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	return c.renderDump(http.MethodPost, c.chatEndpoint, body), nil
}

// DumpSearch mirrors Dump for the /search endpoint.
func (c *Client) DumpSearch(req SearchRequest) (string, error) {
	body, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal search request: %w", err)
	}
	return c.renderDump(http.MethodPost, c.searchEndpoint, body), nil
}

// DumpAsyncSubmit mirrors Dump for POST /async/chat/completions (research submit).
func (c *Client) DumpAsyncSubmit(req AsyncRequest) (string, error) {
	body, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal async request: %w", err)
	}
	return c.renderDump(http.MethodPost, c.asyncEndpoint, body), nil
}

// DumpAsyncGet mirrors Dump for GET /async/chat/completions/{id} (research get).
// No body is sent on the wire; the dump shows only the request line + headers.
func (c *Client) DumpAsyncGet(id string) string {
	return c.renderDump(http.MethodGet, c.asyncEndpoint+"/"+id, nil)
}

func (c *Client) renderDump(method, endpoint string, body []byte) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %s\n", method, endpoint)
	fmt.Fprintf(&buf, "Authorization: Bearer ***REDACTED***\n")
	if len(body) > 0 {
		fmt.Fprintf(&buf, "Content-Type: application/json\n")
	}
	fmt.Fprintf(&buf, "Accept: application/json\n")
	fmt.Fprintf(&buf, "User-Agent: %s\n", c.userAgent)
	if len(body) > 0 {
		fmt.Fprintf(&buf, "\n%s\n", body)
	}
	return buf.String()
}
