package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
)

func TestRunSearch_HappyPath_WritesEnvelope(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"sonar","choices":[{"message":{"content":"Paris."}}],"citations":["https://w"]}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &searchFlags{model: "sonar"}
	if err := runSearch(context.Background(), &stdout, &stderr, "capital of france?", f); err != nil {
		t.Fatalf("runSearch: %v\n%s", err, stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout.String())
	}
	if env["command"] != "search" {
		t.Errorf("command = %v, want search", env["command"])
	}
	result := env["result"].(map[string]any)
	if result["answer"] != "Paris." {
		t.Errorf("answer = %v", result["answer"])
	}
}

func TestRunSearch_MissingAPIKey_ExitsCode2(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "")

	var stdout, stderr bytes.Buffer
	f := &searchFlags{model: "sonar"}
	err := runSearch(context.Background(), &stdout, &stderr, "hello", f)
	if err == nil {
		t.Fatal("expected error when PERPLEXITY_API_KEY is unset")
	}
	if ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", ExitCode())
	}
	msg := stderr.String()
	if !strings.Contains(msg, "PERPLEXITY_API_KEY") {
		t.Errorf("stderr missing env var name: %q", msg)
	}
	if !strings.Contains(msg, "perplexity.ai/settings/api") {
		t.Errorf("stderr missing docs link: %q", msg)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on config error, got %q", stdout.String())
	}
}

// TestWriteJSON_EnvelopeShape asserts the stable stdout envelope: schema_version,
// provider, command, elapsed_ms, result. This is the contract downstream agents
// depend on — break it and you break every consumer.
func TestWriteJSON_EnvelopeShape(t *testing.T) {
	env := envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "search",
		ElapsedMs:     42,
		Result: searchOutput{
			Answer:    "Paris.",
			Model:     "sonar",
			Citations: []string{"https://example.com/a"},
		},
	}
	var buf bytes.Buffer
	if err := writeJSON(&buf, env); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"schema_version", "provider", "command", "elapsed_ms", "result"} {
		if _, ok := got[key]; !ok {
			t.Errorf("envelope missing key %q; got %v", key, got)
		}
	}
	if got["schema_version"] != "1" {
		t.Errorf("schema_version = %v, want \"1\"", got["schema_version"])
	}
	if got["provider"] != "perplexity" {
		t.Errorf("provider = %v, want perplexity", got["provider"])
	}
	if got["command"] != "search" {
		t.Errorf("command = %v, want search", got["command"])
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", got["result"])
	}
	if result["answer"] != "Paris." {
		t.Errorf("result.answer = %v, want Paris.", result["answer"])
	}
	if result["model"] != "sonar" {
		t.Errorf("result.model = %v, want sonar", result["model"])
	}
}
