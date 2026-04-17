package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"

	"github.com/sapihav/perplexity-cli/internal/client"
	"github.com/spf13/cobra"
)

// searchOutput is what we write to stdout on success: answer + citations,
// plus the resolved model for convenience. Kept intentionally small per M1.
type searchOutput struct {
	Answer    string   `json:"answer"`
	Model     string   `json:"model"`
	Citations []string `json:"citations"`
}

type searchFlags struct {
	model      string
	maxRetries int
	verbose    bool
	quiet      bool
	pretty     bool
	out        string
}

func newSearchCmd() *cobra.Command {
	f := &searchFlags{}
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Ask Perplexity Sonar a question and return the answer + citations as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	c.Flags().StringVar(&f.model, "model", "sonar", "Perplexity model to use")
	c.Flags().IntVar(&f.maxRetries, "max-retries", 3, "Retries on 429/5xx with exponential backoff")
	c.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "Verbose progress logging to stderr")
	c.Flags().BoolVarP(&f.quiet, "quiet", "q", false, "Suppress non-error stderr output")
	c.Flags().BoolVar(&f.pretty, "pretty", false, "Indent JSON output")
	c.Flags().StringVar(&f.out, "out", "", "Write JSON to this file instead of stdout")
	return c
}

func runSearch(ctx context.Context, stdout, stderr io.Writer, query string, f *searchFlags) error {
	apiKey := os.Getenv("PERPLEXITY_API_KEY")
	if apiKey == "" {
		setExit(2)
		fmt.Fprintln(stderr, "error: PERPLEXITY_API_KEY is not set")
		fmt.Fprintln(stderr, "       Create a key at https://www.perplexity.ai/settings/api and export it:")
		fmt.Fprintln(stderr, "         export PERPLEXITY_API_KEY=pplx-...")
		return errors.New("missing PERPLEXITY_API_KEY")
	}

	logf := func(format string, a ...any) {
		if f.verbose && !f.quiet {
			fmt.Fprintf(stderr, "perplexity: "+format+"\n", a...)
		}
	}
	logf("model=%s max_retries=%d", f.model, f.maxRetries)

	c := client.New(apiKey, client.WithMaxRetries(f.maxRetries))

	resp, err := c.Complete(ctx, client.Request{
		Model: f.model,
		Messages: []client.Message{
			{Role: "user", Content: query},
		},
	})
	if err != nil {
		return handleClientError(stderr, err)
	}

	out := searchOutput{
		Model:     resp.Model,
		Citations: resp.Citations,
	}
	if out.Citations == nil {
		out.Citations = []string{}
	}
	if len(resp.Choices) > 0 {
		out.Answer = resp.Choices[0].Message.Content
	}

	return writeJSON(stdout, out, f)
}

// handleClientError maps a client error to our exit-code taxonomy and prints
// a short human-readable line to stderr. It always returns the error unchanged
// so the caller can propagate it.
func handleClientError(stderr io.Writer, err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		setExit(1)
		fmt.Fprintf(stderr, "error: perplexity API returned HTTP %d\n", apiErr.Status)
		if apiErr.Body != "" {
			fmt.Fprintf(stderr, "       body: %s\n", truncate(apiErr.Body, 500))
		}
		return err
	}
	// Anything that looks network-ish is a network error.
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		setExit(3)
		fmt.Fprintf(stderr, "error: network failure: %v\n", err)
		return err
	}
	// Default: treat as API error (e.g., JSON decode).
	setExit(1)
	fmt.Fprintf(stderr, "error: %v\n", err)
	return err
}

func writeJSON(stdout io.Writer, v any, f *searchFlags) error {
	var data []byte
	var err error
	if f.pretty {
		data, err = json.MarshalIndent(v, "", "  ")
	} else {
		data, err = json.Marshal(v)
	}
	if err != nil {
		setExit(1)
		return fmt.Errorf("marshal output: %w", err)
	}
	data = append(data, '\n')

	if f.out != "" {
		// #nosec G306 -- 0o644 is fine for user-chosen output file.
		if err := os.WriteFile(f.out, data, 0o644); err != nil {
			setExit(1)
			return fmt.Errorf("write %s: %w", f.out, err)
		}
		return nil
	}
	if _, err := stdout.Write(data); err != nil {
		setExit(1)
		return err
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
