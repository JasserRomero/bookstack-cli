package cli

import (
	"github.com/JasserRomero/bookstack-cli/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd(profile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the effective configuration and the configured profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newConfigShowCmd(profile), newConfigProfilesCmd())
	return cmd
}

type showSources struct {
	URL         string `json:"url"`
	ResolveURL  string `json:"resolve_url"`
	TokenID     string `json:"token_id"`
	TokenSecret string `json:"token_secret"`
}

type showOutput struct {
	Profile     *string     `json:"profile"`
	ConfigFile  string      `json:"config_file"`
	URL         string      `json:"url"`
	ResolveURL  string      `json:"resolve_url"`
	TokenID     string      `json:"token_id"`
	TokenSecret string      `json:"token_secret"`
	Sources     showSources `json:"sources"`
}

func newConfigShowCmd(profile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration and the source of each value",
		Long: `Print the effective configuration as JSON: the selected profile (null when
only environment variables are used), the config file path, each value and its
source (env, file, or default for a resolve_url that falls back to url).

The token secret is never printed: token_secret is always "set".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(*profile)
			if err != nil {
				return err
			}
			out := showOutput{
				ConfigFile:  cfg.File,
				URL:         cfg.URL,
				ResolveURL:  cfg.ResolveURL,
				TokenID:     cfg.TokenID,
				TokenSecret: "set",
				Sources: showSources{
					URL:         cfg.Sources.URL,
					ResolveURL:  cfg.Sources.ResolveURL,
					TokenID:     cfg.Sources.TokenID,
					TokenSecret: cfg.Sources.TokenSecret,
				},
			}
			if cfg.Profile != "" {
				out.Profile = &cfg.Profile
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
}

type profilesOutput struct {
	DefaultProfile *string  `json:"default_profile"`
	Profiles       []string `json:"profiles"`
}

func newConfigProfilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List the profile names of the config file (never their secrets)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			f, err := config.ReadFile(path)
			if err != nil {
				return err
			}
			out := profilesOutput{Profiles: f.ProfileNames()}
			if f.DefaultProfile != "" {
				out.DefaultProfile = &f.DefaultProfile
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
}
