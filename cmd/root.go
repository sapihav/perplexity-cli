// Package cmd wires the cobra command tree.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
	"github.com/spf13/cobra"
)

// exitCode is set by subcommands before returning an error to signal which
// process exit code main() should use:
// 0 success, 1 API error, 2 user/config error, 3 network error.
var exitCode int

// ExitCode returns the exit code the last-run command requested.
func ExitCode() int { return exitCode }

func setExit(code int) { exitCode = code }

// globals holds workspace-standard flags shared across every subcommand.
// Populated by cobra PersistentFlags in init() and read by RunE bodies.
type globals struct {
	verbose    bool
	quiet      bool
	pretty     bool
	out        string
	dryRun     bool
	timeoutSec int
	jsonErrors bool
	rateLimit  float64
	maxRetries int
	userAgent  string
}

var g = &globals{}

// rootCmd is the top-level `perplexity` command.
var rootCmd = &cobra.Command{
	Use:           "perplexity",
	Short:         "Thin CLI wrapper for the Perplexity Sonar API",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	p := rootCmd.PersistentFlags()
	p.BoolVarP(&g.verbose, "verbose", "v", false, "Verbose progress logging to stderr")
	p.BoolVarP(&g.quiet, "quiet", "q", false, "Suppress non-error stderr output")
	p.BoolVar(&g.pretty, "pretty", false, "Indent JSON output")
	p.StringVar(&g.out, "out", "", "Write JSON to this file instead of stdout")
	p.BoolVar(&g.dryRun, "dry-run", false, "Print the planned request (Authorization redacted) and exit without calling the API")
	p.IntVar(&g.timeoutSec, "timeout", 60, "Per-request timeout in seconds")
	p.BoolVar(&g.jsonErrors, "json-errors", false, "Emit errors as {error:{message,code}} JSON on stderr")
	p.Float64Var(&g.rateLimit, "rate-limit", 1, "Client-side rate limit in requests per second (0 disables)")
	p.IntVar(&g.maxRetries, "max-retries", 3, "Retries on 429/5xx with exponential backoff")
	p.StringVar(&g.userAgent, "user-agent", "", "Override the User-Agent header (also: PERPLEXITY_USER_AGENT env)")

	rootCmd.AddCommand(newSearchCmd())
	rootCmd.AddCommand(newAskCmd())
	rootCmd.AddCommand(newSchemaCmd())
	rootCmd.AddCommand(newVersionCmd())
}

// envelope is the stable stdout wrapper around every successful command result.
type envelope struct {
	SchemaVersion string `json:"schema_version"`
	Provider      string `json:"provider"`
	Command       string `json:"command"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	Result        any    `json:"result"`
}

// extraClientOptions is a test hook so test code can inject WithEndpoint and
// WithBackoff without adding production flags. Always empty in release builds.
var extraClientOptions []client.Option

// clientOptions builds []client.Option from the global flags. The User-Agent
// resolves from --user-agent, then PERPLEXITY_USER_AGENT env, then default.
func clientOptions() []client.Option {
	ua := g.userAgent
	if ua == "" {
		ua = os.Getenv("PERPLEXITY_USER_AGENT")
	}
	opts := []client.Option{
		client.WithMaxRetries(g.maxRetries),
		client.WithTimeout(time.Duration(g.timeoutSec) * time.Second),
		client.WithRateLimit(g.rateLimit),
	}
	if ua != "" {
		opts = append(opts, client.WithUserAgent(ua))
	}
	return append(opts, extraClientOptions...)
}

// logf writes to stderr only when --verbose is set and --quiet is not.
func logf(stderr io.Writer, format string, a ...any) {
	if g.verbose && !g.quiet {
		fmt.Fprintf(stderr, "perplexity: "+format+"\n", a...)
	}
}

// errorOut prints a user-facing error to stderr. If --json-errors is set it
// emits {error:{message,code}} instead of the plain line.
func errorOut(stderr io.Writer, code int, message string) {
	setExit(code)
	if g.jsonErrors {
		_ = json.NewEncoder(stderr).Encode(map[string]any{
			"error": map[string]any{
				"message": message,
				"code":    code,
			},
		})
		return
	}
	fmt.Fprintln(stderr, "error: "+message)
}

// writeJSON emits v as JSON (indented if --pretty) to stdout or --out file.
func writeJSON(stdout io.Writer, v any) error {
	var data []byte
	var err error
	if g.pretty {
		data, err = json.MarshalIndent(v, "", "  ")
	} else {
		data, err = json.Marshal(v)
	}
	if err != nil {
		setExit(1)
		return fmt.Errorf("marshal output: %w", err)
	}
	data = append(data, '\n')
	if g.out != "" {
		// #nosec G306 -- 0o644 is fine for user-chosen output file.
		if err := os.WriteFile(g.out, data, 0o644); err != nil {
			setExit(1)
			return fmt.Errorf("write %s: %w", g.out, err)
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
