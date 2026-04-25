package cmd

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// askOutput mirrors searchOutput but is declared separately so future ask-only
// fields (e.g. usage stats) can evolve without disturbing the search contract.
type askOutput struct {
	Answer    string   `json:"answer"`
	Model     string   `json:"model"`
	Citations []string `json:"citations"`
}

// askFlags is just chatFlags — kept as a type alias-ish wrapper so existing
// tests that build &askFlags{...} keep compiling without churn.
type askFlags = chatFlags

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
	res, err := runChatCompletion(ctx, stdout, stderr, query, f)
	if err != nil {
		return err
	}
	if res == nil {
		// dry-run: dump already written to stdout
		return nil
	}

	citations := res.Resp.Citations
	if citations == nil {
		citations = []string{}
	}
	out := askOutput{
		Answer:    firstChoiceContent(res.Resp),
		Model:     res.Resp.Model,
		Citations: citations,
	}
	return writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "ask",
		ElapsedMs:     nowSinceMs(start),
		Result:        out,
	})
}
