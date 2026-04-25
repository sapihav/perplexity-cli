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
	"time"

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

// chatFlags is the subset of ask/reason flags consumed by the shared runner.
// Reason adds its own --strip-thinking on top of these, but the wire-level
// request shape is identical, so we factor everything else out.
type chatFlags struct {
	model        string
	maxTokens    int
	system       string
	messagesFile string
}

// chatCallResult is the output of runChatCompletion: the API response (nil on
// dry-run) plus the assembled message list (useful for tests / verbose logs).
type chatCallResult struct {
	Resp     *client.Response
	Messages []client.Message
}

// runChatCompletion is the shared core of `ask` and `reason`. It resolves
// stdin, loads --messages, builds the request, and either dumps it (--dry-run)
// or sends it. On dry-run it writes the dump to stdout and returns (nil, nil).
// On success it returns the parsed response. Errors are already reported to
// stderr with the right exit code; the caller just propagates them.
func runChatCompletion(ctx context.Context, stdout, stderr io.Writer, query string, f *chatFlags) (*chatCallResult, error) {
	q, err := readStdinIfDash(query)
	if err != nil {
		errorOut(stderr, 1, err.Error())
		return nil, err
	}

	var prior []client.Message
	if f.messagesFile != "" {
		prior, err = loadMessagesFile(f.messagesFile)
		if err != nil {
			errorOut(stderr, 2, err.Error())
			return nil, err
		}
	}

	msgs, err := buildMessages(f.system, prior, q)
	if err != nil {
		errorOut(stderr, 2, err.Error())
		return nil, err
	}

	apiKey := requireAPIKey(stderr)
	if apiKey == "" {
		return nil, fmt.Errorf("missing PERPLEXITY_API_KEY")
	}

	logf(stderr, "model=%s max_tokens=%d turns=%d", f.model, f.maxTokens, len(msgs))

	c := client.New(apiKey, clientOptions()...)
	req := client.Request{Model: f.model, Messages: msgs, MaxTokens: f.maxTokens}

	if g.dryRun {
		dump, err := c.Dump(req)
		if err != nil {
			errorOut(stderr, 1, err.Error())
			return nil, err
		}
		fmt.Fprint(stdout, dump)
		return nil, nil
	}

	resp, err := c.Complete(ctx, req)
	if err != nil {
		return nil, handleClientError(stderr, err)
	}
	return &chatCallResult{Resp: resp, Messages: msgs}, nil
}

// firstChoiceContent returns the assistant content of the first choice, or ""
// when the response carries no choices. Centralizing this keeps both ask and
// reason resilient to malformed/empty upstream responses without panicking.
func firstChoiceContent(r *client.Response) string {
	if r == nil || len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

// nowSinceMs is a tiny helper so callers can keep `start := time.Now()` at the
// top and emit the envelope with a single call. Kept here (not in root.go) to
// stay close to the chat helpers that use it.
func nowSinceMs(start time.Time) int64 { return time.Since(start).Milliseconds() }

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
