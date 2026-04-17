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
		// Echo the model back to prove the request body round-tripped.
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
	// initial + 2 retries = 3 calls total
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

func TestComplete_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := New("k", WithEndpoint(srv.URL), WithBackoff(zeroBackoff))
	_, err := c.Complete(ctx, Request{Model: "sonar", Messages: []Message{{Role: "user", Content: "q"}}})
	if err == nil {
		t.Fatal("expected error from cancelled context")
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
