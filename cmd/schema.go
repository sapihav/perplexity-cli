package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// schemaFlag is the machine-readable description of a single flag.
type schemaFlag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage,omitempty"`
}

// schemaCommand is one command node (and its subcommands).
type schemaCommand struct {
	Name        string          `json:"name"`
	Short       string          `json:"short,omitempty"`
	Use         string          `json:"use,omitempty"`
	Flags       []schemaFlag    `json:"flags"`
	Subcommands []schemaCommand `json:"subcommands,omitempty"`
}

// schemaDoc is the top-level envelope `perplexity schema` emits.
type schemaDoc struct {
	SchemaVersion string        `json:"schema_version"`
	Provider      string        `json:"provider"`
	Command       string        `json:"command"`
	Result        schemaCommand `json:"result"`
}

func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Emit the full command tree (commands, flags, output shapes) as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			doc := schemaDoc{
				SchemaVersion: "1",
				Provider:      "perplexity",
				Command:       "schema",
				Result:        describeCommand(cmd.Root()),
			}
			return writeJSON(cmd.OutOrStdout(), doc)
		},
	}
}

// describeCommand walks a cobra.Command tree, capturing each node's flags and
// children. Hidden commands are skipped so the schema matches `--help` output.
func describeCommand(c *cobra.Command) schemaCommand {
	node := schemaCommand{
		Name:  c.Name(),
		Short: c.Short,
		Use:   c.Use,
		Flags: collectFlags(c),
	}
	for _, sub := range c.Commands() {
		// Skip hidden, help, and cobra's auto-generated completion tree —
		// none are part of the perplexity CLI contract surface that agents
		// should script against.
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		node.Subcommands = append(node.Subcommands, describeCommand(sub))
	}
	return node
}

// collectFlags merges a command's local + inherited (persistent) flags so
// clients see every switch they can pass, no matter where it was declared.
func collectFlags(c *cobra.Command) []schemaFlag {
	var out []schemaFlag
	visit := func(f *pflag.Flag) {
		out = append(out, schemaFlag{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
			Usage:     f.Usage,
		})
	}
	c.LocalFlags().VisitAll(visit)
	c.InheritedFlags().VisitAll(visit)
	if out == nil {
		out = []schemaFlag{}
	}
	return out
}
