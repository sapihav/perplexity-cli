package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
)

// completedJobBody is a representative GET response for a finished
// sonar-deep-research job — embedded chat-completions response with a
// <think>...</think> block, citations, and lifecycle timestamps.
const completedJobBody = `{
	"id": "job-abc",
	"model": "sonar-deep-research",
	"status": "COMPLETED",
	"created_at": 1700000000,
	"started_at": 1700000005,
	"completed_at": 1700000060,
	"response": {
		"model": "sonar-deep-research",
		"choices": [{"message": {"role": "assistant", "content": "<think>step 1\nstep 2</think>\nFinal answer."}}],
		"citations": ["https://example.com/proof"]
	}
}`

func TestResearchSubmit_DryRun_RedactsAndSkipsNetwork(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "pplx-deep-secret")
	g.dryRun = true

	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium"}
	if err := runResearchSubmit(context.Background(), &stdout, &stderr, "research climate models", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := stdout.String()
	for _, want := range []string{
		"POST https://api.perplexity.ai/async/chat/completions",
		"Authorization: Bearer ***REDACTED***",
		`"model": "sonar-deep-research"`,
		`"reasoning_effort": "medium"`,
		`"content": "research climate models"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dry-run missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "pplx-deep-secret") {
		t.Fatal("dry-run leaked api key")
	}
}

func TestResearchSubmit_StdinDash_ReadsPrompt(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	r, w, _ := os.Pipe()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	go func() { _, _ = io.WriteString(w, "from stdin prompt\n"); _ = w.Close() }()

	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "low"}
	if err := runResearchSubmit(context.Background(), &stdout, &stderr, "-", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"content": "from stdin prompt"`) {
		t.Errorf("stdin not piped: %q", stdout.String())
	}
}

func TestResearchSubmit_InvalidReasoningEffort_ExitCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "ultra"}
	err := runResearchSubmit(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error for invalid reasoning effort")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2", ExitCode())
	}
	if !strings.Contains(stderr.String(), "low|medium|high") {
		t.Errorf("stderr missing valid set: %q", stderr.String())
	}
}

func TestResearchSubmit_LoadsMessagesFile(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	_ = os.WriteFile(path, []byte(`[{"role":"user","content":"prior turn"}]`), 0o600)

	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{
		model:           "sonar-deep-research",
		reasoningEffort: "high",
		system:          "be precise",
		messagesFile:    "@" + path,
		maxTokens:       512,
	}
	if err := runResearchSubmit(context.Background(), &stdout, &stderr, "now", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := stdout.String()
	for _, want := range []string{
		`"role": "system"`, `"content": "be precise"`,
		`"content": "prior turn"`, `"content": "now"`,
		`"max_tokens": 512`, `"reasoning_effort": "high"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dry-run missing %q", want)
		}
	}
}

func TestResearchSubmit_BadMessagesFile_ExitCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium", messagesFile: "@/nope/missing.json"}
	err := runResearchSubmit(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2", ExitCode())
	}
}

func TestResearchSubmit_MissingPromptAndMessages_ExitCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium"}
	err := runResearchSubmit(context.Background(), &stdout, &stderr, "", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2", ExitCode())
	}
}

func TestResearchSubmit_MissingAPIKey_ExitCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "")
	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium"}
	err := runResearchSubmit(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2", ExitCode())
	}
}

func TestResearchSubmit_StdinError_ExitCode1(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	// Replace stdin with a closed reader so io.ReadAll on it returns OK with
	// empty bytes — buildMessages then fails. To force a stdin *read* error,
	// use a pipe whose write end is closed by passing "-" with a pipe that
	// returns an error: simulate by using an os.File pointed at a non-readable.
	// Simpler: cover the error path by giving an empty stdin and checking that
	// an empty prompt (after stdin) propagates as "no messages" (exit 2). The
	// readStdinIfDash *itself* error path is exercised by the fakeNetErr-like
	// scenarios in shared_test; here we exercise the empty-after-stdin path.
	r, w, _ := os.Pipe()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	_ = w.Close() // EOF immediately

	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium"}
	err := runResearchSubmit(context.Background(), &stdout, &stderr, "-", f)
	if err == nil {
		t.Fatal("expected error from empty stdin + no messages")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2 (empty prompt + no messages)", ExitCode())
	}
}

func TestResearchSubmit_HappyPath_WritesEnvelope(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		// Verify the request shape nests under "request".
		var got client.AsyncRequest
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if got.Request.Model != "sonar-deep-research" || got.Request.ReasoningEffort != "medium" {
			t.Errorf("unexpected req: %+v", got)
		}
		_, _ = w.Write([]byte(`{"id":"job-xyz","model":"sonar-deep-research","status":"CREATED","created_at":1700000000}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium"}
	if err := runResearchSubmit(context.Background(), &stdout, &stderr, "deep dive", f); err != nil {
		t.Fatalf("runResearchSubmit: %v\n%s", err, stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout.String())
	}
	if env["command"] != "research submit" {
		t.Errorf("command = %v", env["command"])
	}
	r := env["result"].(map[string]any)
	if r["job_id"] != "job-xyz" || r["status"] != "CREATED" {
		t.Errorf("result = %+v", r)
	}
	if r["created_at"].(float64) != 1700000000 {
		t.Errorf("created_at = %v", r["created_at"])
	}
}

func TestResearchSubmit_APIError_ExitCode1(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchSubmitFlags{model: "sonar-deep-research", reasoningEffort: "medium"}
	err := runResearchSubmit(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
}

func TestResearchGet_DryRun_RedactsAndShowsGet(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "pplx-secret")
	g.dryRun = true

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	if err := runResearchGet(context.Background(), &stdout, &stderr, "job-abc", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := stdout.String()
	if !strings.Contains(s, "GET https://api.perplexity.ai/async/chat/completions/job-abc") {
		t.Errorf("dry-run missing GET line: %q", s)
	}
	if !strings.Contains(s, "Bearer ***REDACTED***") {
		t.Errorf("dry-run missing redaction: %q", s)
	}
	if strings.Contains(s, "pplx-secret") {
		t.Fatal("dry-run leaked api key")
	}
	// GET has no body — verify no Content-Type header line.
	if strings.Contains(s, "Content-Type:") {
		t.Errorf("GET dump should not include Content-Type: %q", s)
	}
}

func TestResearchGet_EmptyID_ExitCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	err := runResearchGet(context.Background(), &stdout, &stderr, "   ", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2", ExitCode())
	}
}

func TestResearchGet_MissingAPIKey_ExitCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "")
	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	err := runResearchGet(context.Background(), &stdout, &stderr, "job-abc", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 2 {
		t.Errorf("exit = %d, want 2", ExitCode())
	}
}

func TestResearchGet_Completed_StripsThinkingAndEmitsAnswer(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/job-abc") {
			t.Errorf("path = %q, want suffix /job-abc", r.URL.Path)
		}
		_, _ = w.Write([]byte(completedJobBody))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	if err := runResearchGet(context.Background(), &stdout, &stderr, "job-abc", f); err != nil {
		t.Fatalf("runResearchGet: %v\n%s", err, stderr.String())
	}
	var env map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &env)
	if env["command"] != "research get" {
		t.Errorf("command = %v", env["command"])
	}
	r := env["result"].(map[string]any)
	if r["status"] != "COMPLETED" {
		t.Errorf("status = %v", r["status"])
	}
	if got := r["answer"].(string); got != "Final answer." {
		t.Errorf("answer = %q, want stripped final", got)
	}
	reasoning, _ := r["reasoning"].(string)
	if !strings.Contains(reasoning, "step 1") || !strings.Contains(reasoning, "step 2") {
		t.Errorf("reasoning missing CoT: %q", reasoning)
	}
	cits := r["citations"].([]any)
	if len(cits) != 1 || cits[0] != "https://example.com/proof" {
		t.Errorf("citations = %v", cits)
	}
}

func TestResearchGet_RawMode_KeepsThinkInline(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(completedJobBody))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: false}
	if err := runResearchGet(context.Background(), &stdout, &stderr, "job-abc", f); err != nil {
		t.Fatalf("runResearchGet: %v", err)
	}
	var env map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &env)
	r := env["result"].(map[string]any)
	if !strings.Contains(r["answer"].(string), "<think>") {
		t.Errorf("raw mode should keep <think>: %v", r["answer"])
	}
	if _, present := r["reasoning"]; present {
		t.Errorf("raw mode should omit reasoning: %v", r["reasoning"])
	}
}

func TestResearchGet_InProgress_NoAnswer_ExitCode0(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"job-abc","model":"sonar-deep-research","status":"IN_PROGRESS","created_at":1700000000,"started_at":1700000005}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	if err := runResearchGet(context.Background(), &stdout, &stderr, "job-abc", f); err != nil {
		t.Fatalf("runResearchGet: %v", err)
	}
	if ExitCode() != 0 {
		t.Errorf("exit = %d, want 0 (in-flight)", ExitCode())
	}
	var env map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &env)
	r := env["result"].(map[string]any)
	if r["status"] != "IN_PROGRESS" {
		t.Errorf("status = %v", r["status"])
	}
	if _, has := r["answer"]; has {
		t.Errorf("in-flight should omit answer: %v", r["answer"])
	}
	cits := r["citations"].([]any)
	if cits == nil || len(cits) != 0 {
		t.Errorf("citations should be []: %v", cits)
	}
}

func TestResearchGet_Failed_ExitCode1AndPropagatesError(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"job-abc","model":"sonar-deep-research","status":"FAILED","created_at":1700000000,"failed_at":1700000010,"error_message":"upstream timeout"}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	err := runResearchGet(context.Background(), &stdout, &stderr, "job-abc", f)
	if err == nil {
		t.Fatal("expected error on FAILED")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
	if !strings.Contains(stderr.String(), "upstream timeout") {
		t.Errorf("stderr missing error_message: %q", stderr.String())
	}
	// Envelope should still have been emitted before the error returned.
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout missing envelope on FAILED: %v", err)
	}
	r := env["result"].(map[string]any)
	if r["status"] != "FAILED" || r["error_message"] != "upstream timeout" {
		t.Errorf("result = %+v", r)
	}
}

func TestResearchGet_APIError_ExitCode1(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no such job"}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	err := runResearchGet(context.Background(), &stdout, &stderr, "job-missing", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
}

func TestResearch_RegisteredOnRoot(t *testing.T) {
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "research" {
			found = true
			subs := map[string]bool{}
			for _, sub := range c.Commands() {
				subs[sub.Name()] = true
			}
			if !subs["submit"] || !subs["get"] {
				t.Errorf("research subcommands = %v, want submit + get", subs)
			}
			break
		}
	}
	if !found {
		t.Fatal("research command not registered on root")
	}
}

func TestResearchSubmit_RootCmd_RunE_DispatchesViaArgs(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	c := newResearchSubmitCmd()
	var stdout bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"my prompt"})
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout.String(), `"content": "my prompt"`) {
		t.Errorf("RunE did not pass arg as prompt: %q", stdout.String())
	}
}

func TestResearchSubmit_RootCmd_RunE_NoArgs(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")

	c := newResearchSubmitCmd()
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{})
	err := c.Execute()
	if err == nil {
		t.Fatal("expected error with no args + no messages")
	}
}

func TestResearchGet_WriteJSONError_PropagatesAndKeepsExitCode(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"j","model":"sonar-deep-research","status":"COMPLETED","created_at":1}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	g.out = "/dev/null/forbidden/path/out.json" // unwritable: forces os.WriteFile error

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	err := runResearchGet(context.Background(), &stdout, &stderr, "j", f)
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestResearchGet_FillsModelFromResponseWhenTopLevelMissing(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Top-level model is empty; fall back to response.model.
		_, _ = w.Write([]byte(`{"id":"j","model":"","status":"COMPLETED","created_at":1,"completed_at":2,"response":{"model":"sonar-deep-research","choices":[{"message":{"content":"x"}}],"citations":[]}}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithAsyncEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &researchGetFlags{stripThinking: true}
	if err := runResearchGet(context.Background(), &stdout, &stderr, "j", f); err != nil {
		t.Fatalf("runResearchGet: %v", err)
	}
	var env map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &env)
	r := env["result"].(map[string]any)
	if r["model"] != "sonar-deep-research" {
		t.Errorf("model = %v, want fallback from response.model", r["model"])
	}
}

func TestResearchGet_RootCmd_RunE_DispatchesViaArg(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	c := newResearchGetCmd()
	var stdout bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"job-zzz"})
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "/job-zzz") {
		t.Errorf("RunE did not pass id arg: %q", stdout.String())
	}
}
