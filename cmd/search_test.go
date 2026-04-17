package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunSearch_MissingAPIKey_ExitsCode2(t *testing.T) {
	// Ensure env is clean for this test.
	t.Setenv("PERPLEXITY_API_KEY", "")

	var stdout, stderr bytes.Buffer
	f := &searchFlags{model: "sonar", maxRetries: 0}
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
