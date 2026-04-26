package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
	"github.com/spf13/cobra"
)

// researchSubmitOutput is the payload under envelope.result for `research submit`.
// Mirrors the upstream submit response: id+status+model+created_at. The answer
// is fetched later via `research get`.
type researchSubmitOutput struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
}

// researchGetOutput is the payload under envelope.result for `research get`.
// Optional fields use omitempty so the JSON shape adapts to job state:
// in-flight jobs omit answer/reasoning/completed_at; FAILED jobs surface
// error_message + failed_at; raw mode (--strip-thinking=false) keeps the
// upstream content unchanged in `answer` and omits `reasoning`.
type researchGetOutput struct {
	JobID        string   `json:"job_id"`
	Status       string   `json:"status"`
	Model        string   `json:"model"`
	CreatedAt    int64    `json:"created_at"`
	StartedAt    int64    `json:"started_at,omitempty"`
	CompletedAt  int64    `json:"completed_at,omitempty"`
	FailedAt     int64    `json:"failed_at,omitempty"`
	Answer       string   `json:"answer,omitempty"`
	Reasoning    string   `json:"reasoning,omitempty"`
	ErrorMessage string   `json:"error_message,omitempty"`
	Citations    []string `json:"citations"`
}

// researchSubmitFlags is the submit-side flag bag. Mirrors chatFlags but adds
// reasoning_effort, which is sonar-deep-research-only.
type researchSubmitFlags struct {
	model           string
	maxTokens       int
	system          string
	messagesFile    string
	reasoningEffort string
}

// researchGetFlags toggles whether the upstream <think> block is split into
// the dedicated reasoning field (default) or kept inline in answer.
type researchGetFlags struct {
	stripThinking bool
}

// validReasoningEffort is the API-accepted set; we validate client-side so a
// typo errors out before burning a paid request.
var validReasoningEffort = map[string]bool{"low": true, "medium": true, "high": true}

func newResearchCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "research",
		Short: "Async deep-research jobs (sonar-deep-research) — MCP perplexity_research equivalent",
		Long: `research wraps the Perplexity async chat-completions API.
Submit a job with "research submit", then poll its state with "research get <id>".
Use this for long-running deep-research runs that exceed sync HTTP timeouts.`,
	}
	c.AddCommand(newResearchSubmitCmd())
	c.AddCommand(newResearchGetCmd())
	return c
}

func newResearchSubmitCmd() *cobra.Command {
	f := &researchSubmitFlags{}
	c := &cobra.Command{
		Use:   "submit <prompt>",
		Short: "Enqueue a sonar-deep-research job (POST /async/chat/completions)",
		Long: `submit POSTs to /async/chat/completions and returns immediately with
{job_id, status, model, created_at}. Pass "-" to read the prompt from stdin.
Use --messages @file.json for multi-turn dialogs; --system sets a system prompt.
Poll the result later with "perplexity research get <job_id>".`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) == 1 {
				q = args[0]
			}
			return runResearchSubmit(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), q, f)
		},
	}
	c.Flags().StringVar(&f.model, "model", "sonar-deep-research", "Async-eligible Perplexity model")
	c.Flags().IntVar(&f.maxTokens, "max-tokens", 0, "Cap response tokens (0 = server default)")
	c.Flags().StringVar(&f.system, "system", "", "Optional system prompt prepended to the message list")
	c.Flags().StringVar(&f.messagesFile, "messages", "", "Load prior turns from @path/to/file.json")
	c.Flags().StringVar(&f.reasoningEffort, "reasoning-effort", "medium", "Depth/cost knob: low | medium | high")
	return c
}

func newResearchGetCmd() *cobra.Command {
	f := &researchGetFlags{}
	c := &cobra.Command{
		Use:   "get <job_id>",
		Short: "Fetch a deep-research job by id (GET /async/chat/completions/{id})",
		Long: `get returns the current state of a research job: status (CREATED, IN_PROGRESS,
COMPLETED, FAILED) plus answer/reasoning/citations once COMPLETED. Exits 0 on
COMPLETED or in-flight (CREATED/IN_PROGRESS); 1 on FAILED so scripts can branch.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResearchGet(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	c.Flags().BoolVar(&f.stripThinking, "strip-thinking", true, "Strip <think>...</think> blocks from result.answer (and surface them under result.reasoning)")
	return c
}

func runResearchSubmit(ctx context.Context, stdout, stderr io.Writer, query string, f *researchSubmitFlags) error {
	start := time.Now()
	q, err := readStdinIfDash(query)
	if err != nil {
		errorOut(stderr, 1, err.Error())
		return err
	}

	if !validReasoningEffort[f.reasoningEffort] {
		errorOut(stderr, 2, fmt.Sprintf("--reasoning-effort must be low|medium|high, got %q", f.reasoningEffort))
		return fmt.Errorf("invalid reasoning effort")
	}

	var prior []client.Message
	if f.messagesFile != "" {
		prior, err = loadMessagesFile(f.messagesFile)
		if err != nil {
			errorOut(stderr, 2, err.Error())
			return err
		}
	}

	msgs, err := buildMessages(f.system, prior, q)
	if err != nil {
		errorOut(stderr, 2, err.Error())
		return err
	}

	apiKey := requireAPIKey(stderr)
	if apiKey == "" {
		return fmt.Errorf("missing PERPLEXITY_API_KEY")
	}

	logf(stderr, "model=%s reasoning_effort=%s turns=%d", f.model, f.reasoningEffort, len(msgs))

	c := client.New(apiKey, clientOptions()...)
	req := client.AsyncRequest{Request: client.AsyncChatRequest{
		Model:           f.model,
		Messages:        msgs,
		MaxTokens:       f.maxTokens,
		ReasoningEffort: f.reasoningEffort,
	}}

	if g.dryRun {
		dump, err := c.DumpAsyncSubmit(req)
		if err != nil {
			errorOut(stderr, 1, err.Error())
			return err
		}
		fmt.Fprint(stdout, dump)
		return nil
	}

	job, err := c.AsyncSubmit(ctx, req)
	if err != nil {
		return handleClientError(stderr, err)
	}
	return writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "research submit",
		ElapsedMs:     nowSinceMs(start),
		Result: researchSubmitOutput{
			JobID:     job.ID,
			Status:    job.Status,
			Model:     job.Model,
			CreatedAt: job.CreatedAt,
		},
	})
}

func runResearchGet(ctx context.Context, stdout, stderr io.Writer, id string, f *researchGetFlags) error {
	start := time.Now()
	id = strings.TrimSpace(id)
	if id == "" {
		errorOut(stderr, 2, "job_id is required")
		return fmt.Errorf("missing job_id")
	}

	apiKey := requireAPIKey(stderr)
	if apiKey == "" {
		return fmt.Errorf("missing PERPLEXITY_API_KEY")
	}

	logf(stderr, "fetching job=%s", id)

	c := client.New(apiKey, clientOptions()...)

	if g.dryRun {
		fmt.Fprint(stdout, c.DumpAsyncGet(id))
		return nil
	}

	job, err := c.AsyncGet(ctx, id)
	if err != nil {
		return handleClientError(stderr, err)
	}

	out := researchGetOutput{
		JobID:        job.ID,
		Status:       job.Status,
		Model:        job.Model,
		CreatedAt:    job.CreatedAt,
		StartedAt:    job.StartedAt,
		CompletedAt:  job.CompletedAt,
		FailedAt:     job.FailedAt,
		ErrorMessage: job.ErrorMessage,
		Citations:    []string{},
	}
	if job.Response != nil {
		raw := firstChoiceContent(job.Response)
		out.Answer, out.Reasoning = splitThinking(raw, f.stripThinking)
		if job.Response.Citations != nil {
			out.Citations = job.Response.Citations
		}
		if out.Model == "" {
			out.Model = job.Response.Model
		}
	}

	if strings.EqualFold(job.Status, "FAILED") {
		errorOut(stderr, 1, fmt.Sprintf("research job %s FAILED: %s", id, job.ErrorMessage))
	}

	if err := writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "research get",
		ElapsedMs:     nowSinceMs(start),
		Result:        out,
	}); err != nil {
		return err
	}
	if strings.EqualFold(job.Status, "FAILED") {
		return fmt.Errorf("job failed")
	}
	return nil
}
