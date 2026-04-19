package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
	"github.com/spf13/cobra"
)

// searchOutput is what we put in envelope.result on success: answer + citations,
// plus the resolved model for convenience.
type searchOutput struct {
	Answer    string   `json:"answer"`
	Model     string   `json:"model"`
	Citations []string `json:"citations"`
}

type searchFlags struct {
	model string
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
	c.Flags().StringVar(&f.model, "model", "sonar", "Perplexity model to use (sonar | sonar-pro)")
	return c
}

func runSearch(ctx context.Context, stdout, stderr io.Writer, query string, f *searchFlags) error {
	start := time.Now()
	query, err := readStdinIfDash(query)
	if err != nil {
		errorOut(stderr, 1, err.Error())
		return err
	}

	apiKey := requireAPIKey(stderr)
	if apiKey == "" {
		return fmt.Errorf("missing PERPLEXITY_API_KEY")
	}

	logf(stderr, "model=%s max_retries=%d rate_limit=%.2f/s", f.model, g.maxRetries, g.rateLimit)

	c := client.New(apiKey, clientOptions()...)
	req := client.Request{
		Model:    f.model,
		Messages: []client.Message{{Role: "user", Content: query}},
	}

	if g.dryRun {
		dump, err := c.Dump(req)
		if err != nil {
			errorOut(stderr, 1, err.Error())
			return err
		}
		fmt.Fprint(stdout, dump)
		return nil
	}

	resp, err := c.Complete(ctx, req)
	if err != nil {
		return handleClientError(stderr, err)
	}

	out := searchOutput{Model: resp.Model, Citations: resp.Citations}
	if out.Citations == nil {
		out.Citations = []string{}
	}
	if len(resp.Choices) > 0 {
		out.Answer = resp.Choices[0].Message.Content
	}
	return writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "search",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        out,
	})
}
