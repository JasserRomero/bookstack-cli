// Package cli implements the bsk command-line interface.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the bsk root command. version is printed by --version.
func NewRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "bsk",
		Short:   "bsk is a command-line client for the BookStack API, built for coding agents",
		Version: version,
		// Without Args and RunE, cobra prints help and exits 0 for any argument,
		// so a mistyped command would look like a success to an agent.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}
