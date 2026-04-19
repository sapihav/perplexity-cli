package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/sapihav/perplexity-cli/internal/client"
)

// requireAPIKey returns the key or an empty string after printing a friendly
// error to stderr (exit code 2). It centralizes the env-only auth contract
// shared by `search` and `ask`.
func requireAPIKey(stderr io.Writer) string {
	key := os.Getenv("PERPLEXITY_API_KEY")
	if key != "" {
		return key
	}
	errorOut(stderr, 2, "PERPLEXITY_API_KEY is not set — create one at https://www.perplexity.ai/settings/api")
	return ""
}

// readStdinIfDash resolves "-" to a trimmed stdin read. All other strings pass
// through untouched. Used to honor the workspace standard "-" means stdin.
func readStdinIfDash(raw string) (string, error) {
	if raw != "-" {
		return raw, nil
	}
	b, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// loadMessagesFile parses a JSON file prefixed with "@" into []client.Message.
// Accepts either a bare array `[{"role":..,"content":..}]` or an object with
// a top-level "messages" field, matching OpenAI-compat conventions.
func loadMessagesFile(arg string) ([]client.Message, error) {
	if !strings.HasPrefix(arg, "@") {
		return nil, fmt.Errorf("--messages expects @path/to/file.json, got %q", arg)
	}
	path := strings.TrimPrefix(arg, "@")
	b, err := os.ReadFile(path) // #nosec G304 -- user-chosen path by design
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Try array first.
	var arr []client.Message
	if err := json.Unmarshal(b, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}
	// Fall back to {"messages":[...]}.
	var obj struct {
		Messages []client.Message `json:"messages"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(obj.Messages) == 0 {
		return nil, fmt.Errorf("%s: no messages found", path)
	}
	return obj.Messages, nil
}

// buildMessages assembles a messages slice from optional system prompt,
// optional pre-existing turns (from --messages), and the current user query.
// An empty query is allowed only when prior turns are supplied.
func buildMessages(system string, prior []client.Message, query string) ([]client.Message, error) {
	var msgs []client.Message
	if system != "" {
		msgs = append(msgs, client.Message{Role: "system", Content: system})
	}
	msgs = append(msgs, prior...)
	if query != "" {
		msgs = append(msgs, client.Message{Role: "user", Content: query})
	}
	if len(msgs) == 0 {
		return nil, errors.New("no messages: provide a query or --messages")
	}
	return msgs, nil
}

// handleClientError maps a client error to our exit-code taxonomy and prints
// a user-facing line (respecting --json-errors). Returns the error unchanged.
func handleClientError(stderr io.Writer, err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		errorOut(stderr, 1, fmt.Sprintf("perplexity API returned HTTP %d: %s", apiErr.Status, truncate(apiErr.Body, 500)))
		return err
	}
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		errorOut(stderr, 3, fmt.Sprintf("network failure: %v", err))
		return err
	}
	errorOut(stderr, 1, err.Error())
	return err
}
