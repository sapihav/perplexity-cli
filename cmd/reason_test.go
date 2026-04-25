package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
)

// reasonResponseBody is a representative sonar-reasoning-pro response: a
// <think>...</think> block followed by the final answer in the same content.
const reasonResponseBody = `{
	"model": "sonar-reasoning-pro",
	"choices": [{"message": {"role": "assistant", "content": "<think>step 1\nstep 2</think>\nThe answer is 42."}}],
	"citations": ["https://example.com/proof"]
}`

func TestReason_DefaultModelIsSonarReasoningPro(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	c := newReasonCmd()
	flag := c.Flags().Lookup("model")
	if flag == nil {
		t.Fatal("--model flag missing on reason cmd")
	}
	if flag.DefValue != "sonar-reasoning-pro" {
		t.Errorf("default model = %q, want sonar-reasoning-pro", flag.DefValue)
	}
}

func TestReason_DryRun_UsesDefaultModel(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "pplx-secret")
	g.dryRun = true

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning-pro"}, stripThinking: true}
	if err := runReason(context.Background(), &stdout, &stderr, "why is the sky blue?", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := stdout.String()
	if !strings.Contains(s, `"model": "sonar-reasoning-pro"`) {
		t.Errorf("dry-run missing default model: %q", s)
	}
	if strings.Contains(s, "pplx-secret") {
		t.Fatal("dry-run leaked api key")
	}
	if !strings.Contains(s, "Bearer ***REDACTED***") {
		t.Errorf("dry-run missing redaction: %q", s)
	}
}

func TestReason_DryRun_AcceptsModelOverride(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning"}, stripThinking: true}
	if err := runReason(context.Background(), &stdout, &stderr, "q", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"model": "sonar-reasoning"`) {
		t.Errorf("override model not respected: %q", stdout.String())
	}
}

func TestReason_RequiresQueryOrMessages(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning-pro"}}
	err := runReason(context.Background(), &stdout, &stderr, "", f)
	if err == nil {
		t.Fatal("expected error when no query and no messages")
	}
	if ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", ExitCode())
	}
}

func TestReason_StdinDash_ReadsQuery(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	r, w, _ := os.Pipe()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	go func() {
		_, _ = w.WriteString("why is the sky blue?\n")
		_ = w.Close()
	}()

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning-pro"}, stripThinking: true}
	if err := runReason(context.Background(), &stdout, &stderr, "-", f); err != nil {
		t.Fatalf("runReason: %v", err)
	}
	if !strings.Contains(stdout.String(), `"content": "why is the sky blue?"`) {
		t.Errorf("stdin not piped into request: %q", stdout.String())
	}
}

func TestReason_HappyPath_StripsThinkingByDefault(t *testing.T) {
	resetGlobals(t)
	srv := mockChatServer(t, reasonResponseBody)
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning-pro"}, stripThinking: true}
	if err := runReason(context.Background(), &stdout, &stderr, "meaning?", f); err != nil {
		t.Fatalf("runReason: %v\n%s", err, stderr.String())
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout.String())
	}
	if env["command"] != "reason" {
		t.Errorf("command = %v, want reason", env["command"])
	}
	r := env["result"].(map[string]any)
	if got := r["answer"].(string); got != "The answer is 42." {
		t.Errorf("answer = %q, want stripped final answer", got)
	}
	thinking, _ := r["thinking"].(string)
	if !strings.Contains(thinking, "step 1") || !strings.Contains(thinking, "step 2") {
		t.Errorf("thinking missing CoT: %q", thinking)
	}
	cits := r["citations"].([]any)
	if len(cits) != 1 || cits[0] != "https://example.com/proof" {
		t.Errorf("citations = %v", cits)
	}
}

func TestReason_RawMode_NoStripping(t *testing.T) {
	resetGlobals(t)
	srv := mockChatServer(t, reasonResponseBody)
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning-pro"}, stripThinking: false}
	if err := runReason(context.Background(), &stdout, &stderr, "q", f); err != nil {
		t.Fatalf("runReason: %v\n%s", err, stderr.String())
	}
	var env map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &env)
	r := env["result"].(map[string]any)
	if !strings.Contains(r["answer"].(string), "<think>") {
		t.Errorf("raw mode should retain <think>: %v", r["answer"])
	}
	if _, present := r["thinking"]; present {
		t.Errorf("raw mode should omit thinking field; got %v", r["thinking"])
	}
}

func TestReason_APIError_ExitCode1(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &reasonFlags{chatFlags: chatFlags{model: "sonar-reasoning-pro"}}
	err := runReason(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
}

func TestSplitThinking_TableDriven(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		strip        bool
		wantAnswer   string
		wantThinking string
	}{
		{"strip removes think and trims", "<think>cot</think>\nfinal", true, "final", "cot"},
		{"strip removes thinking long form", "<thinking>longer cot</thinking>final", true, "final", "longer cot"},
		{"strip handles multiple blocks", "<think>a</think>x<think>b</think>y", true, "xy", "a\n\nb"},
		{"no strip preserves raw", "<think>cot</think>final", false, "<think>cot</think>final", ""},
		{"no think block returns content unchanged", "just an answer", true, "just an answer", ""},
		{"strip handles multiline cot", "<think>line1\nline2</think>\n\nthe answer", true, "the answer", "line1\nline2"},
		{"case insensitive tag", "<THINK>x</THINK>final", true, "final", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, th := splitThinking(tc.in, tc.strip)
			if a != tc.wantAnswer {
				t.Errorf("answer = %q, want %q", a, tc.wantAnswer)
			}
			if th != tc.wantThinking {
				t.Errorf("thinking = %q, want %q", th, tc.wantThinking)
			}
		})
	}
}

func TestReason_RegisteredOnRoot(t *testing.T) {
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "reason" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("reason subcommand not registered on root")
	}
}
