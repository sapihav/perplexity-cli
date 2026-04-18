// Package cmd wires the cobra command tree. M1 exposes only `search`.
package cmd

import (
	"github.com/spf13/cobra"
)

// exitCode is set by subcommands before returning an error to signal which
// process exit code main() should use. See the conventions in README.md:
// 0 success, 1 API error, 2 user/config error, 3 network error.
var exitCode int

// ExitCode returns the exit code the last-run command requested.
func ExitCode() int { return exitCode }

func setExit(code int) { exitCode = code }

// rootCmd is the top-level `perplexity` command.
var rootCmd = &cobra.Command{
	Use:           "perplexity",
	Short:         "Thin CLI wrapper for the Perplexity Sonar API",
	SilenceUsage:  true, // don't spam usage on runtime errors
	SilenceErrors: true, // we print errors ourselves with our own formatting
}

// Execute runs the CLI.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(newSearchCmd())
	rootCmd.AddCommand(newVersionCmd())
}
