package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/JasserRomero/bookstack-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Terminal hooks, replaced in tests.
var (
	isTerminal = func(r io.Reader) bool {
		f, ok := r.(*os.File)
		return ok && term.IsTerminal(int(f.Fd()))
	}
	readHidden = func(r io.Reader) (string, error) {
		f, ok := r.(*os.File)
		if !ok {
			return "", errors.New("stdin is not a terminal")
		}
		b, err := term.ReadPassword(int(f.Fd()))
		return string(b), err
	}
)

const authLong = `Store BookStack credentials in a profile of the config file
($XDG_CONFIG_HOME/bookstack-cli/config.toml, default
~/.config/bookstack-cli/config.toml). The file is written atomically with mode
0600 in a 0700 directory; other profiles and default_profile are kept, and
default_profile is set to this profile when the file has none.

There is no flag for the token secret, so it never reaches shell history or the
process list. On a terminal it is read with a hidden prompt, and missing --url
or --token-id are prompted. Otherwise the secret is the first line of stdin and
--url and --token-id are required.

The profile written is --profile, else $BOOKSTACK_PROFILE, else default_profile,
else "default". auth does not contact BookStack.`

const authExample = `  # Non-interactive (agents, scripts):
  printf '%s\n' "$SECRET" | bsk auth --url https://docs.example.com --token-id <id>

  # Interactive, into a named profile:
  bsk auth --profile client`

type authOutput struct {
	Profile    string `json:"profile"`
	ConfigFile string `json:"config_file"`
}

func newAuthCmd(profile *string) *cobra.Command {
	var rawURL, tokenID, resolveURL string
	cmd := &cobra.Command{
		Use:     "auth",
		Short:   "Store BookStack credentials in a config profile",
		Long:    authLong,
		Example: authExample,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, prompts := cmd.InOrStdin(), cmd.ErrOrStderr()
			tty := isTerminal(in)
			lines := bufio.NewReader(in)
			if !tty {
				if rawURL == "" {
					return usageErrorf("missing --url; pass the flag or run auth in a terminal to be prompted")
				}
				if tokenID == "" {
					return usageErrorf("missing --token-id; pass the flag or run auth in a terminal to be prompted")
				}
			}

			path, err := config.Path()
			if err != nil {
				return err
			}
			f, err := config.ReadFile(path)
			if err != nil {
				return err
			}
			name := targetProfile(*profile, f)

			if rawURL == "" {
				if rawURL, err = prompt(prompts, lines, "BookStack URL: "); err != nil {
					return err
				}
			}
			if rawURL == "" {
				return usageErrorf("missing --url")
			}
			if err := validateURL("--url", rawURL); err != nil {
				return err
			}
			if resolveURL != "" {
				if err := validateURL("--resolve-url", resolveURL); err != nil {
					return err
				}
			}
			if tokenID == "" {
				if tokenID, err = prompt(prompts, lines, "Token ID: "); err != nil {
					return err
				}
			}
			if tokenID == "" {
				return usageErrorf("missing --token-id")
			}

			secret, err := readSecret(tty, in, lines, prompts)
			if err != nil {
				return err
			}

			p := config.Profile{
				URL:         config.NormalizeURL(rawURL),
				ResolveURL:  config.NormalizeURL(resolveURL),
				TokenID:     tokenID,
				TokenSecret: secret,
			}
			if err := config.SaveProfile(path, name, p); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), authOutput{Profile: name, ConfigFile: path})
		},
	}
	cmd.Flags().StringVar(&rawURL, "url", "", "BookStack base URL used for API calls")
	cmd.Flags().StringVar(&tokenID, "token-id", "", "API token ID")
	cmd.Flags().StringVar(&resolveURL, "resolve-url", "", "public URL of the instance, when it differs from --url (default: --url)")
	return cmd
}

func targetProfile(flag string, f *config.File) string {
	for _, name := range []string{flag, os.Getenv(config.EnvProfile), f.DefaultProfile} {
		if name != "" {
			return name
		}
	}
	return config.DefaultProfileName
}

func prompt(w io.Writer, r *bufio.Reader, label string) (string, error) {
	fmt.Fprint(w, label)
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", strings.TrimSuffix(label, ": "), err)
	}
	return strings.TrimSpace(line), nil
}

// readSecret reads the token secret with a hidden prompt on a terminal, or
// from the first line of stdin otherwise.
func readSecret(tty bool, in io.Reader, lines *bufio.Reader, prompts io.Writer) (string, error) {
	if tty {
		fmt.Fprint(prompts, "Token secret: ")
		s, err := readHidden(in)
		// The hidden prompt swallows the newline typed by the user.
		fmt.Fprintln(prompts)
		if err != nil {
			return "", fmt.Errorf("read token secret: %w", err)
		}
		if s = strings.TrimSpace(s); s == "" {
			return "", usageErrorf("missing token secret")
		}
		return s, nil
	}
	line, err := lines.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read token secret: %w", err)
	}
	if line = strings.TrimSpace(line); line == "" {
		return "", usageErrorf("missing token secret; pipe it as the first line of stdin")
	}
	return line, nil
}

func validateURL(flag, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return usageErrorf("invalid %s %q: must be an absolute http or https URL", flag, raw)
	}
	return nil
}
