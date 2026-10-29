package config_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/JasserRomero/bookstack-cli/internal/config"
)

const secret = "s3cr3t-VALUE-never-print"

// isolate points the config directory at a temp dir and clears every
// BOOKSTACK_* variable so the developer's environment cannot leak in.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, k := range []string{config.EnvURL, config.EnvResolveURL, config.EnvTokenID, config.EnvTokenSecret, config.EnvProfile} {
		t.Setenv(k, "")
	}
	return filepath.Join(dir, "bookstack-cli", "config.toml")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const twoProfiles = `default_profile = "work"

[profiles.work]
url = "https://work.example.com/"
token_id = "work-id"
token_secret = "work-secret"

[profiles.home]
url = "https://home.example.com"
resolve_url = "https://public.home.example.com//"
token_id = "home-id"
token_secret = "home-secret"
`

func TestPathUsesXDGConfigHome(t *testing.T) {
	want := isolate(t)
	got, err := config.Path()
	if err != nil || got != want {
		t.Fatalf("Path() = %q, %v; want %q", got, err, want)
	}
}

func TestPathFallsBackToHomeConfig(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	got, err := config.Path()
	want := filepath.Join(home, ".config", "bookstack-cli", "config.toml")
	if err != nil || got != want {
		t.Fatalf("Path() = %q, %v; want %q", got, err, want)
	}
}

// S1, S8: env vars alone are a complete configuration with no file.
func TestLoadEnvOnly(t *testing.T) {
	path := isolate(t)
	t.Setenv(config.EnvURL, "https://env.example.com/")
	t.Setenv(config.EnvTokenID, "env-id")
	t.Setenv(config.EnvTokenSecret, secret)
	got, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := &config.Config{
		Profile: "", File: path,
		URL: "https://env.example.com", ResolveURL: "https://env.example.com",
		TokenID: "env-id", TokenSecret: secret,
		Sources: config.Sources{URL: "env", ResolveURL: "default", TokenID: "env", TokenSecret: "env"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v\nwant %+v", got, want)
	}
}

// S2, S6, S7: file only, default_profile selects, trailing slashes stripped,
// resolve_url defaults to url.
func TestLoadFileOnlyDefaultProfile(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, twoProfiles)
	got, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := &config.Config{
		Profile: "work", File: path,
		URL: "https://work.example.com", ResolveURL: "https://work.example.com",
		TokenID: "work-id", TokenSecret: "work-secret",
		Sources: config.Sources{URL: "file", ResolveURL: "default", TokenID: "file", TokenSecret: "file"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v\nwant %+v", got, want)
	}
}

// S6: an explicit resolve_url is kept (slashes stripped) with source file.
func TestLoadResolveURLFromFile(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, twoProfiles)
	got, err := config.Load("home")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ResolveURL != "https://public.home.example.com" || got.Sources.ResolveURL != "file" {
		t.Fatalf("resolve_url = %q (%s)", got.ResolveURL, got.Sources.ResolveURL)
	}
}

// S8: each env var replaces only its own key.
func TestLoadPerKeyEnvOverride(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, twoProfiles)
	t.Setenv(config.EnvTokenSecret, secret)
	t.Setenv(config.EnvResolveURL, "https://resolve.example.com/")
	got, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := &config.Config{
		Profile: "work", File: path,
		URL: "https://work.example.com", ResolveURL: "https://resolve.example.com",
		TokenID: "work-id", TokenSecret: secret,
		Sources: config.Sources{URL: "file", ResolveURL: "env", TokenID: "file", TokenSecret: "env"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %+v\nwant %+v", got, want)
	}
}

// S8: an explicit --profile ignores connection env vars.
func TestLoadExplicitProfileIgnoresEnv(t *testing.T) {
	isolate(t)
	writeFile(t, mustPath(t), twoProfiles)
	t.Setenv(config.EnvURL, "https://env.example.com")
	t.Setenv(config.EnvTokenID, "env-id")
	t.Setenv(config.EnvTokenSecret, secret)
	t.Setenv(config.EnvResolveURL, "https://env-resolve.example.com")
	got, err := config.Load("home")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Profile != "home" || got.URL != "https://home.example.com" || got.TokenID != "home-id" ||
		got.TokenSecret != "home-secret" || got.ResolveURL != "https://public.home.example.com" ||
		got.Sources != (config.Sources{URL: "file", ResolveURL: "file", TokenID: "file", TokenSecret: "file"}) {
		t.Fatalf("Load = %+v", got)
	}
}

// S7: BOOKSTACK_PROFILE selects a profile over default_profile.
func TestLoadProfileFromEnv(t *testing.T) {
	isolate(t)
	writeFile(t, mustPath(t), twoProfiles)
	t.Setenv(config.EnvProfile, "home")
	got, err := config.Load("")
	if err != nil || got.Profile != "home" || got.TokenID != "home-id" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

// S7: a single profile works with no flag and no default_profile.
func TestLoadSingleProfileWithoutDefault(t *testing.T) {
	isolate(t)
	writeFile(t, mustPath(t), "[profiles.only]\nurl = \"https://only.example.com\"\ntoken_id = \"i\"\ntoken_secret = \"s\"\n")
	got, err := config.Load("")
	if err != nil || got.Profile != "only" || got.URL != "https://only.example.com" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

// S7: unknown profile lists the available ones.
func TestLoadUnknownProfile(t *testing.T) {
	isolate(t)
	writeFile(t, mustPath(t), twoProfiles)
	for _, tc := range []struct{ flag, env string }{{"nope", ""}, {"", "nope"}} {
		t.Setenv(config.EnvProfile, tc.env)
		_, err := config.Load(tc.flag)
		var ue *config.UnknownProfileError
		if !errors.As(err, &ue) {
			t.Fatalf("Load(%q) env %q err = %v, want UnknownProfileError", tc.flag, tc.env, err)
		}
		if ue.Name != "nope" || !reflect.DeepEqual(ue.Available, []string{"home", "work"}) {
			t.Fatalf("error = %+v", ue)
		}
		if ue.Error() != `unknown profile "nope"; available profiles: home, work` {
			t.Fatalf("message = %q", ue.Error())
		}
	}
}

// S5: missing keys are named, in a stable order.
func TestLoadMissingKeys(t *testing.T) {
	isolate(t)
	t.Setenv(config.EnvTokenID, "env-id")
	_, err := config.Load("")
	var me *config.MissingError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want MissingError", err)
	}
	if !reflect.DeepEqual(me.Keys, []string{"url", "token_secret"}) {
		t.Fatalf("keys = %v", me.Keys)
	}
	if !strings.HasPrefix(me.Error(), "missing: url, token_secret") {
		t.Fatalf("message = %q", me.Error())
	}
}

// S5: incomplete profile also reports missing keys.
func TestLoadIncompleteProfile(t *testing.T) {
	isolate(t)
	writeFile(t, mustPath(t), "[profiles.a]\nurl = \"https://a.example.com\"\n")
	_, err := config.Load("a")
	var me *config.MissingError
	if !errors.As(err, &me) || !reflect.DeepEqual(me.Keys, []string{"token_id", "token_secret"}) {
		t.Fatalf("err = %v", err)
	}
}

// S5: malformed TOML is config_invalid naming the file, without leaking values.
func TestLoadMalformedTOML(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, "[profiles.a]\ntoken_secret = "+secret+"\n")
	_, err := config.Load("")
	var ie *config.InvalidError
	if !errors.As(err, &ie) || ie.Path != path {
		t.Fatalf("err = %v, want InvalidError for %s", err, path)
	}
	if !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), secret) {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestReadFileMissingIsEmpty(t *testing.T) {
	path := isolate(t)
	f, err := config.ReadFile(path)
	if err != nil || f.DefaultProfile != "" || len(f.ProfileNames()) != 0 {
		t.Fatalf("ReadFile = %+v, %v", f, err)
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://a.example.com":    "https://a.example.com",
		"https://a.example.com/":   "https://a.example.com",
		"https://a.example.com///": "https://a.example.com",
		"https://a.example.com/x/": "https://a.example.com/x",
	} {
		if got := config.NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// S3: creates file 0600 and directory 0700, sets default_profile when absent.
func TestSaveProfileCreatesFileWithPermissions(t *testing.T) {
	path := isolate(t)
	p := config.Profile{URL: "https://a.example.com", TokenID: "id", TokenSecret: secret}
	if err := config.SaveProfile(path, "a", p); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
	f, err := config.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.DefaultProfile != "a" || f.Profiles["a"] != p {
		t.Fatalf("file = %+v", f)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// S3, S9: rewrite preserves other profiles and default_profile, tightens perms.
func TestSaveProfilePreservesOthersAndTightensPermissions(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, twoProfiles)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before, _ := config.ReadFile(path)
	p := config.Profile{URL: "https://new.example.com", TokenID: "new-id", TokenSecret: secret}
	if err := config.SaveProfile(path, "new", p); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)
	after, err := config.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.DefaultProfile != "work" || after.Profiles["work"] != before.Profiles["work"] ||
		after.Profiles["home"] != before.Profiles["home"] || after.Profiles["new"] != p {
		t.Fatalf("after = %+v", after)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// S5, S3: an invalid existing file is reported and left byte-for-byte unchanged.
func TestSaveProfileInvalidFileUnchanged(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, "not = [valid\n")
	err := config.SaveProfile(path, "a", config.Profile{URL: "u", TokenID: "i", TokenSecret: "s"})
	var ie *config.InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want InvalidError", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, []byte("not = [valid\n")) {
		t.Fatalf("file changed: %q", got)
	}
}

// S10: the library must not depend on cobra or terminal handling.
func TestConfigIsALibrary(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/spf13/") || strings.HasPrefix(dep, "golang.org/x/term") {
			t.Errorf("internal/config depends on %s", dep)
		}
	}
}

func mustPath(t *testing.T) string {
	t.Helper()
	p, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	return p
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

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" {
			t.Errorf("unexpected file left in config dir: %s", e.Name())
		}
	}
}

// F2: rewriting a symlinked config file (dotfile managers) must update the
// link target and keep the symlink, without touching the target directory mode.
func TestSaveProfileKeepsSymlink(t *testing.T) {
	path := isolate(t)
	targetDir := t.TempDir()
	if err := os.Chmod(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "bookstack.toml")
	writeFile(t, target, twoProfiles)
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if err := config.SaveProfile(path, "new", config.Profile{URL: "https://n.example.com", TokenID: "n", TokenSecret: secret}); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink (mode %v)", path, info.Mode())
	}
	f, err := config.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.ProfileNames(); strings.Join(got, ",") != "home,new,work" {
		t.Errorf("target profiles = %v, want home,new,work", got)
	}
	assertMode(t, target, 0o600)
	assertMode(t, targetDir, 0o755)
	assertMode(t, filepath.Dir(path), 0o700)
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("unexpected files left in target dir: %v", entries)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

// F6: default_profile naming an undefined profile is a broken file.
func TestLoadUndefinedDefaultProfile(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, "default_profile = \"gone\"\n\n[profiles.work]\nurl = \"u\"\ntoken_id = \"i\"\ntoken_secret = \""+secret+"\"\n")
	_, err := config.Load("")
	var ie *config.InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want InvalidError", err)
	}
	if want := `default_profile "gone" is not defined`; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks the secret: %q", err)
	}
}

// F6: several profiles, no default and nothing selected must point the user
// at --profile and list the available profiles.
func TestLoadAmbiguousProfileMessage(t *testing.T) {
	path := isolate(t)
	writeFile(t, path, strings.Replace(twoProfiles, "default_profile = \"work\"\n", "", 1))
	_, err := config.Load("")
	var me *config.MissingError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v, want MissingError", err)
	}
	want := "missing: url, token_id, token_secret; no profile selected, use --profile or BOOKSTACK_PROFILE (available profiles: home, work)"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}
