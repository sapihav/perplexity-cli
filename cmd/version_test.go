package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersion_EmitsJSON(t *testing.T) {
	resetGlobals(t)
	c := newVersionCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("version: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not json: %v\n%s", err, buf.String())
	}
	if got["provider"] != "perplexity" || got["command"] != "version" {
		t.Errorf("envelope wrong: %v", got)
	}
	if got["version"] == "" {
		t.Error("version empty")
	}
	if Version() == "" {
		t.Error("Version() empty")
	}
}

func TestLogf_VerboseWritesToStderr(t *testing.T) {
	resetGlobals(t)
	g.verbose = true
	var buf bytes.Buffer
	logf(&buf, "hello %s", "world")
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("logf did not emit: %q", buf.String())
	}
}

func TestLogf_QuietSuppresses(t *testing.T) {
	resetGlobals(t)
	g.verbose = true
	g.quiet = true
	var buf bytes.Buffer
	logf(&buf, "hidden")
	if buf.Len() != 0 {
		t.Errorf("quiet did not suppress: %q", buf.String())
	}
}

func TestTruncate_LongString(t *testing.T) {
	got := truncate(strings.Repeat("x", 20), 5)
	if got != "xxxxx..." {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q, want short", got)
	}
}
