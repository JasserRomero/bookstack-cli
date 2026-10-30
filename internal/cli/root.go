// Package cli implements the bsk command-line interface.
package cli

import "github.com/spf13/cobra"

const rootLong = `bsk is a command-line client for the BookStack API, built for coding agents.

Output is JSON on stdout ("pages get --raw" prints Markdown). Errors are JSON
on stderr, {"error":{"code":"<code>","status":<http>,"message":"..."}}, where
"status" is present only for errors with an HTTP status, and the exit code is:
  0  ok
  1  error
  2  usage (invalid arguments or flags)
  3  auth: invalid token or missing permission (HTTP 401/403)
  4  not found (HTTP 404)
  5  conflict: the page changed since --if-unmodified-since (pages update)
  6  validation (HTTP 422)

Start with "bsk test" to check the configuration, then "bsk search" or
"bsk books list" / "bsk books get <id>" / "bsk pages get <id>".

Configuration, highest precedence first:
  --profile <name>        use that profile of the config file; connection
                          environment variables are then ignored
  BOOKSTACK_URL, BOOKSTACK_TOKEN_ID, BOOKSTACK_TOKEN_SECRET, BOOKSTACK_RESOLVE_URL
                          each overrides its own key of the selected profile;
                          together they work without a config file
  BOOKSTACK_PROFILE       selects the profile when --profile is not given
  config file             $XDG_CONFIG_HOME/bookstack-cli/config.toml
                          (default ~/.config/bookstack-cli/config.toml),
                          written by "bsk auth"; default_profile is used when
                          nothing else selects a profile

Store credentials without putting the secret in shell history:
  printf '%s\n' "$SECRET" | bsk auth --url https://docs.example.com --token-id <id>`

// NewRootCmd builds the bsk root command. version is printed by --version
// and sent to BookStack in the User-Agent as bsk/<version>.
func NewRootCmd(version string) *cobra.Command {
	var profile string
	root := &cobra.Command{
		Use:     "bsk",
		Short:   "bsk is a command-line client for the BookStack API, built for coding agents",
		Long:    rootLong,
		Version: version,
		// Without Args and RunE, cobra prints help and exits 0 for any argument,
		// so a mistyped command would look like a success to an agent.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		// Errors are reported as JSON by Execute; cobra must not print usage.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&profile, "profile", "",
		"config profile to use (default: $BOOKSTACK_PROFILE, then default_profile)")
	d := apiDeps{profile: &profile, version: version}
	root.AddCommand(newAuthCmd(&profile), newConfigCmd(&profile), newTestCmd(d), newSearchCmd(d),
		newShelvesCmd(d), newBooksCmd(d), newChaptersCmd(d), newPagesCmd(d), newTemplateCmd(d))
	return root
}
