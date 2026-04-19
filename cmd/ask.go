package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
	"github.com/spf13/cobra"
)

// askOutput mirrors searchOutput but is declared separately so future ask-only
// fields (e.g. usage stats) can evolve without disturbing the search contract.
type askOutput struct {
	Answer    string   `json:"answer"`
	Model     string   `json:"model"`
	Citations []string `json:"citations"`
}

type askFlags struct {
	model        string
	maxTokens    int
	system       string
	messagesFile string
}

func newAskCmd() *cobra.Command {
	f := &askFlags{}
	c := &cobra.Command{
		Use:   "ask <query>",
		Short: "Conversational Perplexity query (chat/completions) — MCP perplexity_ask equivalent",
		Long: `ask wraps POST /chat/completions with the sonar-pro model by default.
Pass a question as an argument, or "-" to read the query from stdin.
Use --messages @file.json for multi-turn dialogs; --system sets a system prompt.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) == 1 {
				q = args[0]
			}
			return runAsk(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), q, f)
		},
	}
	c.Flags().StringVar(&f.model, "model", "sonar-pro", "Perplexity model (sonar | sonar-pro)")
	c.Flags().IntVar(&f.maxTokens, "max-tokens", 0, "Cap response tokens (0 = server default)")
	c.Flags().StringVar(&f.system, "system", "", "Optional system prompt prepended to the message list")
	c.Flags().StringVar(&f.messagesFile, "messages", "", "Load prior turns from @path/to/file.json")
	return c
}

func runAsk(ctx context.Context, stdout, stderr io.Writer, query string, f *askFlags) error {
	start := time.Now()
	query, err := readStdinIfDash(query)
	if err != nil {
		errorOut(stderr, 1, err.Error())
		return err
	}

	var prior []client.Message
	if f.messagesFile != "" {
		prior, err = loadMessagesFile(f.messagesFile)
		if err != nil {
			errorOut(stderr, 2, err.Error())
			return err
		}
	}

	msgs, err := buildMessages(f.system, prior, query)
	if err != nil {
		errorOut(stderr, 2, err.Error())
		return err
	}

	apiKey := requireAPIKey(stderr)
	if apiKey == "" {
		return fmt.Errorf("missing PERPLEXITY_API_KEY")
	}

	logf(stderr, "model=%s max_tokens=%d turns=%d", f.model, f.maxTokens, len(msgs))

	c := client.New(apiKey, clientOptions()...)
	req := client.Request{Model: f.model, Messages: msgs, MaxTokens: f.maxTokens}

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

	out := askOutput{Model: resp.Model, Citations: resp.Citations}
	if out.Citations == nil {
		out.Citations = []string{}
	}
	if len(resp.Choices) > 0 {
		out.Answer = resp.Choices[0].Message.Content
	}
	return writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "ask",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        out,
	})
}
