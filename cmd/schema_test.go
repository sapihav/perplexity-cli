package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

func TestSchema_EmitsCommandTree(t *testing.T) {
	resetGlobals(t)
	var buf bytes.Buffer
	// Find the pre-registered schema command under rootCmd so the tree it
	// describes matches what users actually see.
	var schema *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "schema" {
			schema = c
			break
		}
	}
	if schema == nil {
		t.Fatal("schema subcommand not registered")
	}
	schema.SetOut(&buf)
	if err := schema.RunE(schema, nil); err != nil {
		t.Fatalf("schema run: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("schema output is not JSON: %v\n%s", err, buf.String())
	}
	if got["provider"] != "perplexity" || got["schema_version"] != "1" || got["command"] != "schema" {
		t.Errorf("envelope fields wrong: %v", got)
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("result not object: %T", got["result"])
	}
	subs, _ := result["subcommands"].([]any)
	if len(subs) == 0 {
		t.Fatal("subcommands missing")
	}
	names := map[string]bool{}
	for _, s := range subs {
		if m, ok := s.(map[string]any); ok {
			names[m["name"].(string)] = true
		}
	}
	for _, want := range []string{"search", "ask", "schema", "version"} {
		if !names[want] {
			t.Errorf("missing subcommand %q in %v", want, names)
		}
	}
}
