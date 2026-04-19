package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// zeroBackoff makes retries instant so tests stay fast.
func zeroBackoff(_ int) time.Duration { return 0 }

func TestComplete_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("User-Agent"); got != "perplexity-cli" {
			t.Errorf("User-Agent = %q, want perplexity-cli", got)
		}
		body, _ := io.ReadAll(r.Body)
		var got Request
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("could not parse request body: %v", err)
		}
		if got.Model != "sonar" || len(got.Messages) != 1 || got.Messages[0].Content != "hello" {
			t.Errorf("unexpected request body: %+v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "sonar",
			"choices": [{"message": {"role": "assistant", "content": "hi there"}}],
			"citations": ["https://example.com/a", "https://example.com/b"]
		}`))
	}))
	defer srv.Close()

	c := New("test-key", WithEndpoint(srv.URL), WithBackoff(zeroBackoff))
	resp, err := c.Complete(context.Background(), Request{
		Model:    "sonar",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}
	if resp.Model != "sonar" {
		t.Errorf("Model = %q, want sonar", resp.Model)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hi there" {
		t.Errorf("unexpected choices: %+v", resp.Choices)
	}
	if len(resp.Citations) != 2 {
		t.Errorf("Citations len = %d, want 2", len(resp.Citations))
	}
}

func TestComplete_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"sonar","choices":[{"message":{"role":"assistant","content":"ok"}}],"citations":[]}`))
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff))
	resp, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Choices[0].Message.Content)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (one 429 + one success)", got)
	}
}

func TestComplete_RetriesOn500ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"model":"sonar","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithMaxRetries(3))
	_, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestComplete_GivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`slow down`))
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithMaxRetries(2))
	_, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %T %v", err, err)
	}
	if apiErr.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", apiErr.Status)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
	if !strings.Contains(apiErr.Error(), "429") {
		t.Errorf("error message %q missing status code", apiErr.Error())
	}
}

func TestComplete_DoesNotRetryOn400(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithMaxRetries(3))
	_, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("want APIError status 400, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestComplete_401Unauthorized_NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithMaxRetries(3))
	_, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("want APIError status 401, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestComplete_Timeout_WrapsAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithMaxRetries(0), WithTimeout(20*time.Millisecond))
	_, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestComplete_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff))
	_, err := c.Complete(ctx, Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestWithUserAgent_OverridesHeader(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"model":"sonar","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithUserAgent("my-agent/1.2.3"))
	_, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotUA != "my-agent/1.2.3" {
		t.Errorf("UA = %q, want my-agent/1.2.3", gotUA)
	}
}

func TestDump_RedactsAuthorization(t *testing.T) {
	c := New("super-secret-key", WithEndpoint("https://example.test/chat/completions"))
	out, err := c.Dump(Request{Model: "sonar-pro", Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 64})
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if strings.Contains(out, "super-secret-key") {
		t.Errorf("Dump leaked API key: %q", out)
	}
	if !strings.Contains(out, "Authorization: Bearer ***REDACTED***") {
		t.Errorf("Dump missing redacted Authorization: %q", out)
	}
	if !strings.Contains(out, "POST https://example.test/chat/completions") {
		t.Errorf("Dump missing endpoint line: %q", out)
	}
	if !strings.Contains(out, `"max_tokens": 64`) {
		t.Errorf("Dump missing body field: %q", out)
	}
}

func TestWithRateLimit_EnforcesInterval(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"model":"sonar","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	// 50/s => 20ms interval. 3 requests must take at least ~40ms (2 gaps).
	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff), WithRateLimit(50))
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Complete(context.Background(), Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Errorf("rate limit too loose: 3 calls took %v, want >=30ms", elapsed)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestSearch_HappyPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req SearchRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request body not JSON: %v", err)
		}
		if req.Query != "hello" || req.MaxResults != 5 {
			t.Errorf("unexpected request: %+v", req)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"T","url":"https://example.com","snippet":"S","date":"2024-01-15","last_updated":"2024-02-01"}]}`))
	}))
	defer srv.Close()

	c := New("k", WithSearchEndpoint(srv.URL+"/search"), WithBackoff(zeroBackoff))
	resp, err := c.Search(context.Background(), SearchRequest{Query: "hello", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotPath != "/search" {
		t.Errorf("path = %q, want /search", gotPath)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "T" || resp.Results[0].Date != "2024-01-15" {
		t.Errorf("unexpected results: %+v", resp.Results)
	}
}

func TestSearch_RetriesOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := New("k", WithSearchEndpoint(srv.URL), WithBackoff(zeroBackoff))
	resp, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp == nil || calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestDumpSearch_RedactsAuthorizationAndTargetsSearch(t *testing.T) {
	c := New("super-secret-key", WithSearchEndpoint("https://example.test/search"))
	out, err := c.DumpSearch(SearchRequest{Query: "hi", MaxResults: 10, Country: "US"})
	if err != nil {
		t.Fatalf("DumpSearch: %v", err)
	}
	if strings.Contains(out, "super-secret-key") {
		t.Errorf("DumpSearch leaked API key: %q", out)
	}
	if !strings.Contains(out, "Authorization: Bearer ***REDACTED***") {
		t.Errorf("DumpSearch missing redacted Authorization: %q", out)
	}
	if !strings.Contains(out, "POST https://example.test/search") {
		t.Errorf("DumpSearch missing endpoint line: %q", out)
	}
	if !strings.Contains(out, `"query": "hi"`) || !strings.Contains(out, `"country": "US"`) {
		t.Errorf("DumpSearch missing body fields: %q", out)
	}
}

func TestDefaultBackoff_IsExponential(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 250 * time.Millisecond},
		{2, 500 * time.Millisecond},
		{3, 1000 * time.Millisecond},
		{4, 2000 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := defaultBackoff(tc.attempt); got != tc.want {
			t.Errorf("defaultBackoff(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}
