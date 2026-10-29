package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Environment variables that configure the connection (S1) and the profile (S7).
const (
	EnvURL         = "BOOKSTACK_URL"
	EnvResolveURL  = "BOOKSTACK_RESOLVE_URL"
	EnvTokenID     = "BOOKSTACK_TOKEN_ID"
	EnvTokenSecret = "BOOKSTACK_TOKEN_SECRET"
	EnvProfile     = "BOOKSTACK_PROFILE"
)

// Sources of an effective configuration value.
const (
	SourceEnv     = "env"
	SourceFile    = "file"
	SourceDefault = "default"
)

// DefaultProfileName is the profile written by auth when nothing selects one.
const DefaultProfileName = "default"

const (
	dirName  = "bookstack-cli"
	fileName = "config.toml"
	dirMode  = 0o700
	fileMode = 0o600
)

// Profile holds the connection settings of one named profile.
type Profile struct {
	URL         string `toml:"url"`
	ResolveURL  string `toml:"resolve_url,omitempty"`
	TokenID     string `toml:"token_id"`
	TokenSecret string `toml:"token_secret"`
}

// File is the content of the configuration file.
type File struct {
	DefaultProfile string             `toml:"default_profile,omitempty"`
	Profiles       map[string]Profile `toml:"profiles"`
}

// Sources records where each effective value came from.
type Sources struct {
	URL         string
	ResolveURL  string
	TokenID     string
	TokenSecret string
}

// Config is the effective connection configuration.
type Config struct {
	// Profile is the selected profile name, or "" when no profile was used.
	Profile     string
	File        string
	URL         string
	ResolveURL  string
	TokenID     string
	TokenSecret string
	Sources     Sources
}

// MissingError reports required keys that no source provided.
type MissingError struct {
	Keys []string
	// Available lists the profiles of the file when none could be selected.
	Available []string
}

func (e *MissingError) Error() string {
	msg := "missing: " + strings.Join(e.Keys, ", ")
	if len(e.Available) > 0 {
		return msg + "; no profile selected, use --profile or " + EnvProfile +
			" (available profiles: " + strings.Join(e.Available, ", ") + ")"
	}
	return msg + `; set the BOOKSTACK_* environment variables or run "bsk auth"`
}

// InvalidError reports a configuration file that cannot be used.
type InvalidError struct {
	Path   string
	Reason string
}

func (e *InvalidError) Error() string {
	return "invalid config file " + e.Path + ": " + e.Reason
}

// UnknownProfileError reports a selected profile that the file does not define.
type UnknownProfileError struct {
	Name      string
	Available []string
}

func (e *UnknownProfileError) Error() string {
	available := "none"
	if len(e.Available) > 0 {
		available = strings.Join(e.Available, ", ")
	}
	return fmt.Sprintf("unknown profile %q; available profiles: %s", e.Name, available)
}

// Path returns the configuration file path: $XDG_CONFIG_HOME/bookstack-cli/config.toml,
// falling back to ~/.config/bookstack-cli/config.toml on every OS.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate config directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, dirName, fileName), nil
}

// ReadFile reads the configuration file at path. A missing file is an empty File.
func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, &InvalidError{Path: path, Reason: err.Error()}
	}
	var f File
	if _, err := toml.Decode(string(data), &f); err != nil {
		// Only the position is reported: parser messages may quote file content,
		// which can include a secret.
		reason := "invalid TOML"
		var pe toml.ParseError
		if errors.As(err, &pe) {
			reason = fmt.Sprintf("invalid TOML at line %d", pe.Position.Line)
		}
		return nil, &InvalidError{Path: path, Reason: reason}
	}
	if f.Profiles == nil {
		f.Profiles = map[string]Profile{}
	}
	return &f, nil
}

// ProfileNames returns the sorted profile names of f.
func (f *File) ProfileNames() []string {
	names := make([]string, 0, len(f.Profiles))
	for name := range f.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Load resolves the effective configuration. profile is the explicit --profile
// value; when set, connection env vars are ignored (S8). Otherwise the profile
// is BOOKSTACK_PROFILE, else default_profile, else the only profile, and each
// set connection env var overrides its own key.
func Load(profile string) (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	f, err := ReadFile(path)
	if err != nil {
		return nil, err
	}

	explicit := profile != ""
	name := profile
	if name == "" {
		name = os.Getenv(EnvProfile)
	}
	if name != "" {
		if _, ok := f.Profiles[name]; !ok {
			return nil, &UnknownProfileError{Name: name, Available: f.ProfileNames()}
		}
	} else if f.DefaultProfile != "" {
		if _, ok := f.Profiles[f.DefaultProfile]; !ok {
			return nil, &InvalidError{Path: path, Reason: fmt.Sprintf("default_profile %q is not defined", f.DefaultProfile)}
		}
		name = f.DefaultProfile
	} else if len(f.Profiles) == 1 {
		name = f.ProfileNames()[0]
	}

	p := f.Profiles[name]
	cfg := &Config{Profile: name, File: path}
	pick := func(fileValue, env string) (string, string) {
		if !explicit {
			if v := os.Getenv(env); v != "" {
				return v, SourceEnv
			}
		}
		if fileValue != "" {
			return fileValue, SourceFile
		}
		return "", ""
	}
	cfg.URL, cfg.Sources.URL = pick(p.URL, EnvURL)
	cfg.ResolveURL, cfg.Sources.ResolveURL = pick(p.ResolveURL, EnvResolveURL)
	cfg.TokenID, cfg.Sources.TokenID = pick(p.TokenID, EnvTokenID)
	cfg.TokenSecret, cfg.Sources.TokenSecret = pick(p.TokenSecret, EnvTokenSecret)

	var missing []string
	if cfg.URL == "" {
		missing = append(missing, "url")
	}
	if cfg.TokenID == "" {
		missing = append(missing, "token_id")
	}
	if cfg.TokenSecret == "" {
		missing = append(missing, "token_secret")
	}
	if len(missing) > 0 {
		e := &MissingError{Keys: missing}
		if name == "" {
			e.Available = f.ProfileNames()
		}
		return nil, e
	}

	cfg.URL = NormalizeURL(cfg.URL)
	if cfg.ResolveURL == "" {
		cfg.ResolveURL, cfg.Sources.ResolveURL = cfg.URL, SourceDefault
	}
	cfg.ResolveURL = NormalizeURL(cfg.ResolveURL)
	return cfg, nil
}

// SaveProfile writes profile p as name into the file at path. The write is
// atomic, keeps every other profile and default_profile (set to name when
// absent), and enforces 0600 on the file and 0700 on its directory.
func SaveProfile(path, name string, p Profile) error {
	f, err := ReadFile(path)
	if err != nil {
		return err
	}
	f.Profiles[name] = p
	if f.DefaultProfile == "" {
		f.DefaultProfile = name
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("set config directory permissions: %w", err)
	}
	return writeAtomic(path, buf.Bytes())
}

func writeAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+fileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(fileMode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// NormalizeURL strips trailing slashes from u.
func NormalizeURL(u string) string {
	return strings.TrimRight(u, "/")
}
