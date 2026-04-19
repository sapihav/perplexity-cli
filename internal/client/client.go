// Package client is a thin wrapper over the Perplexity chat-completions
// endpoint. It exposes one call (Complete) plus retry-with-backoff on
// 429 and 5xx responses. No hidden state, no caches, no file I/O.
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

// DefaultEndpoint is the Perplexity chat-completions URL.
const DefaultEndpoint = "https://api.perplexity.ai/chat/completions"

// DefaultUserAgent is sent unless callers override it via WithUserAgent.
const DefaultUserAgent = "perplexity-cli"

// Client calls the Perplexity API. Construct with New.
type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	userAgent  string
	maxRetries int
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

// WithEndpoint overrides the API URL (useful in tests to point at httptest.Server).
func WithEndpoint(url string) Option {
	return func(c *Client) { c.endpoint = url }
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
		httpClient: &http.Client{Timeout: 60 * time.Second},
		endpoint:   DefaultEndpoint,
		apiKey:     apiKey,
		userAgent:  DefaultUserAgent,
		maxRetries: 3,
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

		resp, err := c.do(ctx, body)
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

		var out Response
		if err := json.Unmarshal(resp.body, &out); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &out, nil
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

// do performs a single HTTP request and reads the full body.
func (c *Client) do(ctx context.Context, body []byte) (*rawResponse, error) {
	httpReq, err := c.buildHTTPRequest(ctx, body)
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

func (c *Client) buildHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.userAgent)
	return httpReq, nil
}

// Dump returns a human-readable trace of the request that Complete would send.
// The Authorization header is redacted; body is the serialized JSON. Intended
// for --dry-run and --verbose output. No network call is made.
func (c *Client) Dump(req Request) (string, error) {
	body, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "POST %s\n", c.endpoint)
	fmt.Fprintf(&buf, "Authorization: Bearer ***REDACTED***\n")
	fmt.Fprintf(&buf, "Content-Type: application/json\n")
	fmt.Fprintf(&buf, "Accept: application/json\n")
	fmt.Fprintf(&buf, "User-Agent: %s\n", c.userAgent)
	fmt.Fprintf(&buf, "\n%s\n", body)
	return buf.String(), nil
}
