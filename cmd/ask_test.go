package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
)

// resetGlobals returns global flag state to defaults between tests so flags
// set by one test do not leak into the next.
func resetGlobals(t *testing.T) {
	t.Helper()
	*g = globals{timeoutSec: 60, maxRetries: 3, rateLimit: 0}
	exitCode = 0
}

func TestAsk_DryRun_RedactsAuthAndSkipsNetwork(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "pplx-super-secret")
	g.dryRun = true

	var stdout, stderr bytes.Buffer
	f := &askFlags{model: "sonar-pro"}
	if err := runAsk(context.Background(), &stdout, &stderr, "hello", f); err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	s := stdout.String()
	if !strings.Contains(s, "POST https://api.perplexity.ai/chat/completions") {
		t.Errorf("dry-run missing endpoint line: %q", s)
	}
	if !strings.Contains(s, "Authorization: Bearer ***REDACTED***") {
		t.Errorf("dry-run did not redact Authorization: %q", s)
	}
	if strings.Contains(s, "pplx-super-secret") {
		t.Fatalf("dry-run leaked the API key: %q", s)
	}
	if !strings.Contains(s, `"model": "sonar-pro"`) {
		t.Errorf("dry-run missing model: %q", s)
	}
}

func TestAsk_DryRun_IncludesSystemAndPriorTurns(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	g.dryRun = true

	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	_ = os.WriteFile(path, []byte(`[{"role":"user","content":"earlier"}]`), 0o600)

	var stdout, stderr bytes.Buffer
	f := &askFlags{model: "sonar-pro", system: "be terse", messagesFile: "@" + path, maxTokens: 128}
	if err := runAsk(context.Background(), &stdout, &stderr, "now", f); err != nil {
		t.Fatalf("runAsk: %v", err)
	}
	s := stdout.String()
	for _, want := range []string{`"role": "system"`, `"content": "be terse"`, `"content": "earlier"`, `"content": "now"`, `"max_tokens": 128`} {
		if !strings.Contains(s, want) {
			t.Errorf("dry-run missing %q in:\n%s", want, s)
		}
	}
}

func TestAsk_MissingQueryAndNoMessages_Fails(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "k")
	var stdout, stderr bytes.Buffer
	f := &askFlags{model: "sonar-pro"}
	err := runAsk(context.Background(), &stdout, &stderr, "", f)
	if err == nil {
		t.Fatal("expected error when no query and no messages")
	}
	if ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", ExitCode())
	}
	if !strings.Contains(stderr.String(), "no messages") {
		t.Errorf("stderr missing hint: %q", stderr.String())
	}
}

func TestAsk_LoadsMessagesFile_ArrayForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "msgs.json")
	content := `[{"role":"user","content":"prior"},{"role":"assistant","content":"ok"}]`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, err := loadMessagesFile("@" + path)
	if err != nil {
		t.Fatalf("loadMessagesFile: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Content != "prior" || msgs[1].Role != "assistant" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestAsk_LoadsMessagesFile_ObjectForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "msgs.json")
	content := `{"messages":[{"role":"user","content":"prior"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	msgs, err := loadMessagesFile("@" + path)
	if err != nil {
		t.Fatalf("loadMessagesFile: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "prior" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestAsk_MessagesFile_MissingAt_Errors(t *testing.T) {
	_, err := loadMessagesFile("/not/an/at/path")
	if err == nil || !strings.Contains(err.Error(), "@") {
		t.Fatalf("want error about @ prefix, got %v", err)
	}
}

func TestAsk_JSONErrors_EmitsStructured(t *testing.T) {
	resetGlobals(t)
	g.jsonErrors = true
	t.Setenv("PERPLEXITY_API_KEY", "")
	var stdout, stderr bytes.Buffer
	f := &askFlags{model: "sonar-pro"}
	_ = runAsk(context.Background(), &stdout, &stderr, "q", f)
	var got map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("stderr is not JSON: %v\n%s", err, stderr.String())
	}
	errObj, _ := got["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("missing error object: %v", got)
	}
	if errObj["code"].(float64) != 2 {
		t.Errorf("code = %v, want 2", errObj["code"])
	}
	if !strings.Contains(errObj["message"].(string), "PERPLEXITY_API_KEY") {
		t.Errorf("message missing env var: %v", errObj["message"])
	}
}

func TestRunAsk_HappyPath_WritesEnvelope(t *testing.T) {
	resetGlobals(t)
	srv := mockChatServer(t, `{"model":"sonar-pro","choices":[{"message":{"role":"assistant","content":"42"}}],"citations":["https://example.com/a"]}`)
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &askFlags{model: "sonar-pro"}
	if err := runAsk(context.Background(), &stdout, &stderr, "meaning?", f); err != nil {
		t.Fatalf("runAsk: %v\n%s", err, stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout.String())
	}
	if env["command"] != "ask" {
		t.Errorf("command = %v, want ask", env["command"])
	}
	r := env["result"].(map[string]any)
	if r["answer"] != "42" {
		t.Errorf("answer = %v, want 42", r["answer"])
	}
	cits := r["citations"].([]any)
	if len(cits) != 1 || cits[0] != "https://example.com/a" {
		t.Errorf("citations = %v", cits)
	}
}

func TestRunAsk_APIError_ExitCode1(t *testing.T) {
	resetGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{client.WithEndpoint(srv.URL), client.WithBackoff(func(int) time.Duration { return 0 })}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &askFlags{model: "sonar-pro"}
	err := runAsk(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
	if !strings.Contains(stderr.String(), "401") {
		t.Errorf("stderr missing 401: %q", stderr.String())
	}
}

// mockChatServer returns an httptest.Server that responds with the given body.
func mockChatServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func TestBuildMessages_SystemPrependedAndQueryAppended(t *testing.T) {
	msgs, err := buildMessages("be concise", nil, "hello")
	if err != nil {
		t.Fatalf("buildMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Errorf("unexpected order: %+v", msgs)
	}
	if msgs[0].Content != "be concise" || msgs[1].Content != "hello" {
		t.Errorf("content mismatch: %+v", msgs)
	}
}
