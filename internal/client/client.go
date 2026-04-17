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

// Client calls the Perplexity API. Construct with New.
type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	maxRetries int
	// backoff returns the duration to sleep before retry attempt n (1-indexed).
	// Exposed for tests; nil means use exponential default (250ms * 2^(n-1)).
	backoff func(attempt int) time.Duration
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

// New constructs a Client. apiKey must be non-empty; callers should check env
// before calling. A zero-value http.Client is used unless WithHTTPClient is passed.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		endpoint:   DefaultEndpoint,
		apiKey:     apiKey,
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
	// attempt 0 is the initial try; 1..maxRetries are retries.
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff(attempt)):
			}
		}

		resp, err := c.do(ctx, body)
		if err != nil {
			// Network-level error: retry if we still can.
			lastErr = err
			if attempt == c.maxRetries {
				return nil, err
			}
			continue
		}

		// Decide based on status.
		if resp.status >= 500 || resp.status == http.StatusTooManyRequests {
			lastErr = &APIError{Status: resp.status, Body: string(resp.body)}
			if attempt == c.maxRetries {
				return nil, lastErr
			}
			continue
		}
		if resp.status >= 400 {
			// 4xx other than 429: do not retry.
			return nil, &APIError{Status: resp.status, Body: string(resp.body)}
		}

		var out Response
		if err := json.Unmarshal(resp.body, &out); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &out, nil
	}

	// Unreachable, but keep the compiler happy.
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

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
