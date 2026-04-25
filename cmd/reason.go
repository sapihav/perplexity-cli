package cmd

import (
	"context"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// reasonOutput is the payload under envelope.result for `reason`. `Thinking`
// is omitted when --strip-thinking=false (raw mode) or when the upstream
// response carries no <think>...</think> block.
type reasonOutput struct {
	Answer    string   `json:"answer"`
	Thinking  string   `json:"thinking,omitempty"`
	Model     string   `json:"model"`
	Citations []string `json:"citations"`
}

// reasonFlags extends chatFlags with the reason-only --strip-thinking switch.
// Embedding chatFlags lets us hand the inner struct straight to the shared
// runner without copying field-by-field.
type reasonFlags struct {
	chatFlags
	stripThinking bool
}

// thinkBlock matches both <think>...</think> and <thinking>...</thinking>
// because some Sonar reasoning responses have been observed to use the longer
// tag form. (?s) makes `.` span newlines; (?i) is case-insensitive on the tag
// name. Non-greedy `.*?` so multiple blocks are captured independently.
var thinkBlock = regexp.MustCompile(`(?is)<(think|thinking)>(.*?)</(think|thinking)>`)

func newReasonCmd() *cobra.Command {
	f := &reasonFlags{}
	c := &cobra.Command{
		Use:   "reason <query>",
		Short: "Step-by-step reasoning query (sonar-reasoning-pro) — MCP perplexity_reason equivalent",
		Long: `reason wraps POST /chat/completions with sonar-reasoning-pro.
The reasoning model emits a <think>...</think> block of chain-of-thought
followed by the final answer. By default the block is stripped from the
answer and surfaced separately under result.thinking. Pass
--strip-thinking=false to keep the raw content in result.answer.

Pass a question as an argument, or "-" to read the query from stdin.
Use --messages @file.json for multi-turn dialogs; --system sets a system prompt.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) == 1 {
				q = args[0]
			}
			return runReason(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), q, f)
		},
	}
	c.Flags().StringVar(&f.model, "model", "sonar-reasoning-pro", "Reasoning model (sonar-reasoning-pro | sonar-reasoning)")
	c.Flags().IntVar(&f.maxTokens, "max-tokens", 0, "Cap response tokens (0 = server default)")
	c.Flags().StringVar(&f.system, "system", "", "Optional system prompt prepended to the message list")
	c.Flags().StringVar(&f.messagesFile, "messages", "", "Load prior turns from @path/to/file.json")
	c.Flags().BoolVar(&f.stripThinking, "strip-thinking", true, "Strip <think>...</think> blocks from result.answer (and surface them under result.thinking)")
	return c
}

func runReason(ctx context.Context, stdout, stderr io.Writer, query string, f *reasonFlags) error {
	start := time.Now()
	res, err := runChatCompletion(ctx, stdout, stderr, query, &f.chatFlags)
	if err != nil {
		return err
	}
	if res == nil {
		// dry-run already wrote the dump
		return nil
	}

	raw := firstChoiceContent(res.Resp)
	answer, thinking := splitThinking(raw, f.stripThinking)

	citations := res.Resp.Citations
	if citations == nil {
		citations = []string{}
	}
	return writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "reason",
		ElapsedMs:     nowSinceMs(start),
		Result: reasonOutput{
			Answer:    answer,
			Thinking:  thinking,
			Model:     res.Resp.Model,
			Citations: citations,
		},
	})
}

// splitThinking returns (answer, thinking). When strip is false, the raw
// content is returned unchanged with thinking="". When strip is true, all
// <think>/<thinking> blocks are extracted (joined by a blank line) and the
// remainder is returned trimmed as the final answer.
func splitThinking(content string, strip bool) (string, string) {
	if !strip {
		return content, ""
	}
	matches := thinkBlock.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return content, ""
	}
	var parts []string
	for _, m := range matches {
		// m[2] is the inner content between the open/close tags.
		parts = append(parts, strings.TrimSpace(m[2]))
	}
	answer := strings.TrimSpace(thinkBlock.ReplaceAllString(content, ""))
	return answer, strings.Join(parts, "\n\n")
}
