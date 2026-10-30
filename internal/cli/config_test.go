package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secret = "s3cr3t-VALUE-never-print"

const twoProfiles = `default_profile = "work"

[profiles.work]
url = "https://work.example.com/"
token_id = "work-id"
token_secret = "work-secret"

[profiles.home]
url = "https://home.example.com"
resolve_url = "https://public.home.example.com/"
token_id = "home-id"
token_secret = "home-secret"
`

// isolate points the config directory at a temp dir and clears every
// BOOKSTACK_* variable so the developer's environment cannot leak in.
// It returns the config file path.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, k := range []string{"BOOKSTACK_URL", "BOOKSTACK_RESOLVE_URL", "BOOKSTACK_TOKEN_ID", "BOOKSTACK_TOKEN_SECRET", "BOOKSTACK_PROFILE"} {
		t.Setenv(k, "")
	}
	return filepath.Join(dir, "bookstack-cli", "config.toml")
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

type result struct {
	stdout, stderr string
	code           int
}

// exec runs bsk with args and stdin through the same path as main.
func exec(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	return execVersion(t, "dev", stdin, args...)
}

// execVersion is exec with the build version passed to NewRootCmd.
func execVersion(t *testing.T, version, stdin string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewRootCmd(version)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := Execute(cmd)
	return result{out.String(), errOut.String(), ExitCode(err)}
}

func (r result) expect(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	if r.code != code {
		t.Errorf("exit code = %d, want %d", r.code, code)
	}
	if r.stdout != stdout {
		t.Errorf("stdout = %q\nwant     %q", r.stdout, stdout)
	}
	if r.stderr != stderr {
		t.Errorf("stderr = %q\nwant     %q", r.stderr, stderr)
	}
	if strings.Contains(r.stdout+r.stderr, secret) {
		t.Errorf("secret leaked in output")
	}
}

// expectErrorPrefix checks an error whose message ends with OS-specific text
// (such as a file or network failure): the exit code, an empty stdout and one
// JSON error on stderr with the code, no status and a message that starts
// with prefix and adds a non-empty reason.
func (r result) expectErrorPrefix(t *testing.T, code int, errCode, prefix string) {
	t.Helper()
	if r.code != code {
		t.Errorf("exit code = %d, want %d", r.code, code)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Status  *int   `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	dec := json.NewDecoder(strings.NewReader(r.stderr))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil || !strings.HasSuffix(r.stderr, "}}\n") || strings.Count(r.stderr, "\n") != 1 {
		t.Fatalf("stderr = %q is not one JSON error line (%v)", r.stderr, err)
	}
	if got.Error.Code != errCode || got.Error.Status != nil {
		t.Errorf("error code = %q, status = %v; want %q without status", got.Error.Code, got.Error.Status, errCode)
	}
	if !strings.HasPrefix(got.Error.Message, prefix) || len(got.Error.Message) == len(prefix) {
		t.Errorf("message = %q, want prefix %q and a reason", got.Error.Message, prefix)
	}
	if strings.Contains(r.stdout+r.stderr, secret) {
		t.Errorf("secret leaked in output")
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %o, want %o", path, got, want)
	}
}

// S1, S4, S6, S8: env vars alone, profile null, secret masked.
func TestConfigShowEnvOnly(t *testing.T) {
	path := isolate(t)
	t.Setenv("BOOKSTACK_URL", "https://env.example.com/")
	t.Setenv("BOOKSTACK_TOKEN_ID", "env-id")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", secret)
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":null,"config_file":"`+path+`","url":"https://env.example.com","resolve_url":"https://env.example.com","token_id":"env-id","token_secret":"set","sources":{"url":"env","resolve_url":"default","token_id":"env","token_secret":"env"}}`+"\n", "")
}

// S2, S4, S7: file only with default_profile.
func TestConfigShowFileOnly(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"work","config_file":"`+path+`","url":"https://work.example.com","resolve_url":"https://work.example.com","token_id":"work-id","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")
}

// S8: per-key env override of the selected profile.
func TestConfigShowPerKeyEnvOverride(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	t.Setenv("BOOKSTACK_TOKEN_SECRET", secret)
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"work","config_file":"`+path+`","url":"https://work.example.com","resolve_url":"https://work.example.com","token_id":"work-id","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"env"}}`+"\n", "")
}

// S8: explicit --profile ignores connection env vars; flag accepted before the command.
func TestConfigShowExplicitProfileIgnoresEnv(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	t.Setenv("BOOKSTACK_URL", "https://env.example.com")
	t.Setenv("BOOKSTACK_TOKEN_ID", "env-id")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", secret)
	want := `{"profile":"home","config_file":"` + path + `","url":"https://home.example.com","resolve_url":"https://public.home.example.com","token_id":"home-id","token_secret":"set","sources":{"url":"file","resolve_url":"file","token_id":"file","token_secret":"file"}}` + "\n"
	exec(t, "", "config", "show", "--profile", "home").expect(t, 0, want, "")
	exec(t, "", "--profile", "home", "config", "show").expect(t, 0, want, "")
}

// S7: BOOKSTACK_PROFILE selects the profile.
func TestConfigShowProfileFromEnv(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	t.Setenv("BOOKSTACK_PROFILE", "home")
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"home","config_file":"`+path+`","url":"https://home.example.com","resolve_url":"https://public.home.example.com","token_id":"home-id","token_secret":"set","sources":{"url":"file","resolve_url":"file","token_id":"file","token_secret":"file"}}`+"\n", "")
}

// S7: a single profile works without flag or default_profile.
func TestConfigShowSingleProfile(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, "[profiles.only]\nurl = \"https://only.example.com\"\ntoken_id = \"i\"\ntoken_secret = \""+secret+"\"\n")
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"only","config_file":"`+path+`","url":"https://only.example.com","resolve_url":"https://only.example.com","token_id":"i","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")
}

// S7: unknown profile is a usage error listing available profiles.
func TestConfigShowUnknownProfile(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	exec(t, "", "config", "show", "--profile", "nope").expect(t, 2, "",
		`{"error":{"code":"usage","message":"unknown profile \"nope\"; available profiles: home, work"}}`+"\n")
}

// S5: missing keys -> config_missing naming them, exit 1.
func TestConfigShowMissing(t *testing.T) {
	isolate(t)
	t.Setenv("BOOKSTACK_TOKEN_ID", "env-id")
	exec(t, "", "config", "show").expect(t, 1, "",
		`{"error":{"code":"config_missing","message":"missing: url, token_secret; set the BOOKSTACK_* environment variables or run \"bsk auth\""}}`+"\n")
}

// S5: malformed TOML -> config_invalid naming the file, exit 1, no secret.
func TestConfigShowMalformed(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, "[profiles.a]\ntoken_secret = "+secret+"\n")
	exec(t, "", "config", "show").expect(t, 1, "",
		`{"error":{"code":"config_invalid","message":"invalid config file `+path+`: invalid TOML at line 2"}}`+"\n")
}

// S9: config profiles lists sorted names and the default, never secrets.
func TestConfigProfiles(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	exec(t, "", "config", "profiles").expect(t, 0, `{"default_profile":"work","profiles":["home","work"]}`+"\n", "")
}

// S9: with no config file, an empty list and exit 0.
func TestConfigProfilesNoFile(t *testing.T) {
	isolate(t)
	exec(t, "", "config", "profiles").expect(t, 0, `{"default_profile":null,"profiles":[]}`+"\n", "")
}

func TestConfigProfilesMalformed(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, "x = [\n")
	exec(t, "", "config", "profiles").expect(t, 1, "",
		`{"error":{"code":"config_invalid","message":"invalid config file `+path+`: invalid TOML at line 1"}}`+"\n")
}

func TestConfigUnknownSubcommandIsUsage(t *testing.T) {
	isolate(t)
	exec(t, "", "config", "nope").expect(t, 2, "",
		`{"error":{"code":"usage","message":"unknown command \"nope\" for \"bsk config\""}}`+"\n")
}

// S3, S6, S9: piped secret, file 0600 / dir 0700, default_profile set, slash stripped.
func TestAuthPipedSecretCreatesConfig(t *testing.T) {
	path := isolate(t)
	exec(t, secret+"\nignored second line\n", "auth", "--url", "https://docs.example.com/", "--token-id", "tid").
		expect(t, 0, `{"profile":"default","config_file":"`+path+`"}`+"\n", "")
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
	want := "default_profile = \"default\"\n\n[profiles]\n  [profiles.default]\n    url = \"https://docs.example.com\"\n    token_id = \"tid\"\n    token_secret = \"" + secret + "\"\n"
	if got := string(readBytes(t, path)); got != want {
		t.Errorf("file = %q\nwant  %q", got, want)
	}
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"default","config_file":"`+path+`","url":"https://docs.example.com","resolve_url":"https://docs.example.com","token_id":"tid","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")
}

// S3, S9: auth --profile adds a profile, keeps the others and default_profile,
// and tightens a pre-existing 0644 file.
func TestAuthProfilePreservesOthersAndTightensPermissions(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	exec(t, secret+"\n", "auth", "--profile", "client", "--url", "https://client.example.com", "--token-id", "cid", "--resolve-url", "https://pub.client.example.com/").
		expect(t, 0, `{"profile":"client","config_file":"`+path+`"}`+"\n", "")
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
	exec(t, "", "config", "profiles").expect(t, 0, `{"default_profile":"work","profiles":["client","home","work"]}`+"\n", "")
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"work","config_file":"`+path+`","url":"https://work.example.com","resolve_url":"https://work.example.com","token_id":"work-id","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")
	exec(t, "", "config", "show", "--profile", "client").expect(t, 0,
		`{"profile":"client","config_file":"`+path+`","url":"https://client.example.com","resolve_url":"https://pub.client.example.com","token_id":"cid","token_secret":"set","sources":{"url":"file","resolve_url":"file","token_id":"file","token_secret":"file"}}`+"\n", "")
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("config dir has %d entries, want only config.toml", len(entries))
	}
}

// S3: target profile is BOOKSTACK_PROFILE, else default_profile.
func TestAuthTargetProfileSelection(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	exec(t, secret+"\n", "auth", "--url", "https://w2.example.com", "--token-id", "w2").
		expect(t, 0, `{"profile":"work","config_file":"`+path+`"}`+"\n", "")
	t.Setenv("BOOKSTACK_PROFILE", "home")
	exec(t, secret+"\n", "auth", "--url", "https://h2.example.com", "--token-id", "h2").
		expect(t, 0, `{"profile":"home","config_file":"`+path+`"}`+"\n", "")
	t.Setenv("BOOKSTACK_PROFILE", "")
	exec(t, "", "config", "show", "--profile", "home").expect(t, 0,
		`{"profile":"home","config_file":"`+path+`","url":"https://h2.example.com","resolve_url":"https://h2.example.com","token_id":"h2","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"work","config_file":"`+path+`","url":"https://w2.example.com","resolve_url":"https://w2.example.com","token_id":"w2","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")
}

// S3: invalid auth input is a usage error (exit 2) and leaves the file unchanged.
func TestAuthUsageErrorsLeaveFileUnchanged(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, twoProfiles)
	before := readBytes(t, path)
	cases := []struct {
		name, stdin string
		args        []string
		message     string
	}{
		{"missing url", secret + "\n", []string{"auth", "--token-id", "x"},
			`missing --url; pass the flag or run auth in a terminal to be prompted`},
		{"missing token id", secret + "\n", []string{"auth", "--url", "https://x.example.com"},
			`missing --token-id; pass the flag or run auth in a terminal to be prompted`},
		{"empty secret", "\n", []string{"auth", "--url", "https://x.example.com", "--token-id", "x"},
			`missing token secret; pipe it as the first line of stdin`},
		{"no stdin", "", []string{"auth", "--url", "https://x.example.com", "--token-id", "x"},
			`missing token secret; pipe it as the first line of stdin`},
		{"bad url", secret + "\n", []string{"auth", "--url", "docs.example.com", "--token-id", "x"},
			`invalid --url \"docs.example.com\": must be an absolute http or https URL`},
		{"bad resolve url", secret + "\n", []string{"auth", "--url", "https://x.example.com", "--token-id", "x", "--resolve-url", "ftp://x"},
			`invalid --resolve-url \"ftp://x\": must be an absolute http or https URL`},
		{"token secret flag", secret + "\n", []string{"auth", "--url", "https://x.example.com", "--token-id", "x", "--token-secret", "y"},
			`unknown flag: --token-secret`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec(t, tc.stdin, tc.args...).expect(t, 2, "", `{"error":{"code":"usage","message":"`+tc.message+`"}}`+"\n")
			if !bytes.Equal(readBytes(t, path), before) {
				t.Error("config file changed")
			}
		})
	}
}

// S3, S5: auth over a malformed file fails with config_invalid and keeps it.
func TestAuthMalformedFileUnchanged(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, "x = [\n")
	exec(t, secret+"\n", "auth", "--url", "https://x.example.com", "--token-id", "x").expect(t, 1, "",
		`{"error":{"code":"config_invalid","message":"invalid config file `+path+`: invalid TOML at line 1"}}`+"\n")
	if got := string(readBytes(t, path)); got != "x = [\n" {
		t.Errorf("file changed: %q", got)
	}
}

// S3: missing values are prompted on a terminal; the secret uses the hidden reader.
func TestAuthPromptsOnTerminal(t *testing.T) {
	path := isolate(t)
	origTerm, origSecret := isTerminal, readHidden
	t.Cleanup(func() { isTerminal, readHidden = origTerm, origSecret })
	isTerminal = func(io.Reader) bool { return true }
	readHidden = func(io.Reader) (string, error) { return secret, nil }
	exec(t, "https://t.example.com/\ntid\n", "auth").expect(t, 0,
		`{"profile":"default","config_file":"`+path+`"}`+"\n",
		"BookStack URL: Token ID: Token secret: \n")
	exec(t, "", "config", "show").expect(t, 0,
		`{"profile":"default","config_file":"`+path+`","url":"https://t.example.com","resolve_url":"https://t.example.com","token_id":"tid","token_secret":"set","sources":{"url":"file","resolve_url":"default","token_id":"file","token_secret":"file"}}`+"\n", "")

	readHidden = func(io.Reader) (string, error) { return "", errors.New("boom") }
	before := readBytes(t, path)
	exec(t, "", "auth", "--url", "https://t.example.com", "--token-id", "x").expect(t, 1, "",
		"Token secret: \n"+`{"error":{"code":"error","message":"read token secret: boom"}}`+"\n")
	if !bytes.Equal(readBytes(t, path), before) {
		t.Error("config file changed")
	}
}

// S5: unknown flags are usage errors.
func TestUnknownFlagIsUsage(t *testing.T) {
	isolate(t)
	exec(t, "", "--nope").expect(t, 2, "", `{"error":{"code":"usage","message":"unknown flag: --nope"}}`+"\n")
}

func TestHelpDocumentsConfigCommands(t *testing.T) {
	r := exec(t, "", "--help")
	for _, want := range []string{"auth", "config", "--profile", "BOOKSTACK_PROFILE", "BOOKSTACK_TOKEN_SECRET",
		`printf '%s\n' "$SECRET" | bsk auth --url`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("root help missing %q:\n%s", want, r.stdout)
		}
	}
	r = exec(t, "", "auth", "--help")
	for _, want := range []string{"--url", "--token-id", "--resolve-url", "--profile", "0600", "first line of stdin",
		`printf '%s\n' "$SECRET" | bsk auth --url`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("auth help missing %q:\n%s", want, r.stdout)
		}
	}
	r = exec(t, "", "config", "--help")
	for _, want := range []string{"show", "profiles"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("config help missing %q:\n%s", want, r.stdout)
		}
	}
}
