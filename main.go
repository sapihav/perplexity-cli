// Command perplexity is a thin CLI wrapper around the Perplexity Sonar API.
package main

import (
	"fmt"
	"os"

	"github.com/sapihav/perplexity-cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Errors are already printed by cobra / the command itself; exit with
		// the code the command chose (stashed on cmd.ExitCode).
		code := cmd.ExitCode()
		if code == 0 {
			// Defensive: if a command returned an error without setting a code,
			// still surface a non-zero status.
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
		os.Exit(code)
	}
}
