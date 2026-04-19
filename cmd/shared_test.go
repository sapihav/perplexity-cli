package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapihav/perplexity-cli/internal/client"
)

func TestHandleClientError_MapsAPIErrorToExit1(t *testing.T) {
	resetGlobals(t)
	var stderr bytes.Buffer
	err := handleClientError(&stderr, &client.APIError{Status: 401, Body: "unauthorized"})
	if err == nil {
		t.Fatal("want error preserved")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
	if !strings.Contains(stderr.String(), "401") {
		t.Errorf("stderr missing status: %q", stderr.String())
	}
}

func TestHandleClientError_MapsNetworkToExit3(t *testing.T) {
	resetGlobals(t)
	var stderr bytes.Buffer
	netErr := &url.Error{Op: "Get", URL: "http://x", Err: errors.New("connection refused")}
	_ = handleClientError(&stderr, netErr)
	if ExitCode() != 3 {
		t.Errorf("exit = %d, want 3", ExitCode())
	}
}

func TestHandleClientError_DeadlineExceededIsNetwork(t *testing.T) {
	resetGlobals(t)
	var stderr bytes.Buffer
	_ = handleClientError(&stderr, context.DeadlineExceeded)
	if ExitCode() != 3 {
		t.Errorf("exit = %d, want 3", ExitCode())
	}
}

func TestHandleClientError_GenericIsExit1(t *testing.T) {
	resetGlobals(t)
	var stderr bytes.Buffer
	_ = handleClientError(&stderr, errors.New("decode failed"))
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
}

// Sanity: plain net.Error path goes through as network.
type fakeNetErr struct{}

func (fakeNetErr) Error() string   { return "net fail" }
func (fakeNetErr) Timeout() bool   { return false }
func (fakeNetErr) Temporary() bool { return false }

var _ net.Error = fakeNetErr{}

func TestHandleClientError_NetErrorIsExit3(t *testing.T) {
	resetGlobals(t)
	var stderr bytes.Buffer
	_ = handleClientError(&stderr, fakeNetErr{})
	if ExitCode() != 3 {
		t.Errorf("exit = %d, want 3", ExitCode())
	}
}

func TestReadStdinIfDash_PassThrough(t *testing.T) {
	got, err := readStdinIfDash("plain")
	if err != nil || got != "plain" {
		t.Errorf("got %q,%v", got, err)
	}
}

func TestReadStdinIfDash_ReadsStdin(t *testing.T) {
	r, w, _ := os.Pipe()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()
	go func() {
		_, _ = w.WriteString("from stdin\n")
		_ = w.Close()
	}()
	got, err := readStdinIfDash("-")
	if err != nil {
		t.Fatalf("readStdinIfDash: %v", err)
	}
	if got != "from stdin" {
		t.Errorf("got %q, want 'from stdin'", got)
	}
}

func TestLoadMessagesFile_BadPath(t *testing.T) {
	_, err := loadMessagesFile("@/nope/does/not/exist.json")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadMessagesFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	_ = os.WriteFile(path, []byte(`{"messages":[]}`), 0o600)
	_, err := loadMessagesFile("@" + path)
	if err == nil || !strings.Contains(err.Error(), "no messages") {
		t.Errorf("want 'no messages' error, got %v", err)
	}
}

func TestWriteJSON_PrettyIndents(t *testing.T) {
	resetGlobals(t)
	g.pretty = true
	var buf bytes.Buffer
	if err := writeJSON(&buf, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if !strings.Contains(buf.String(), "  \"a\"") {
		t.Errorf("pretty output not indented: %q", buf.String())
	}
}

func TestWriteJSON_ToFile(t *testing.T) {
	resetGlobals(t)
	dir := t.TempDir()
	g.out = filepath.Join(dir, "out.json")
	if err := writeJSON(&bytes.Buffer{}, map[string]any{"x": 1}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	b, err := os.ReadFile(g.out)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("file not valid JSON: %v", err)
	}
	if got["x"].(float64) != 1 {
		t.Errorf("x = %v, want 1", got["x"])
	}
}

func TestClientOptions_UserAgentEnvOverride(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_USER_AGENT", "env-agent/2.0")
	opts := clientOptions()
	c := client.New("k", opts...)
	out, err := c.Dump(client.Request{Model: "sonar", Messages: []client.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "env-agent/2.0") {
		t.Errorf("UA env override not applied: %q", out)
	}
}

func TestClientOptions_FlagBeatsEnv(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_USER_AGENT", "env-agent/2.0")
	g.userAgent = "flag-agent/3.0"
	c := client.New("k", clientOptions()...)
	out, _ := c.Dump(client.Request{Model: "sonar", Messages: []client.Message{{Role: "user", Content: "hi"}}})
	if !strings.Contains(out, "flag-agent/3.0") {
		t.Errorf("UA flag did not beat env: %q", out)
	}
	if strings.Contains(out, "env-agent/2.0") {
		t.Errorf("env UA leaked when flag set: %q", out)
	}
}

func TestRunSearch_DryRun_NoNetwork(t *testing.T) {
	resetGlobals(t)
	g.dryRun = true
	t.Setenv("PERPLEXITY_API_KEY", "k-123")

	var stdout, stderr bytes.Buffer
	f := &searchFlags{maxResults: 10}
	if err := runSearch(context.Background(), &stdout, &stderr, "q", f); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	s := stdout.String()
	if strings.Contains(s, "k-123") {
		t.Error("dry-run leaked api key")
	}
	if !strings.Contains(s, "Bearer ***REDACTED***") {
		t.Errorf("dry-run missing redaction: %q", s)
	}
	if !strings.Contains(s, "/search") {
		t.Errorf("dry-run should target /search endpoint: %q", s)
	}
}
