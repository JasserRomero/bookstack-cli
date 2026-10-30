package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func run(t *testing.T, version string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewRootCmd(version)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestRootVersionPrintsInjectedVersion(t *testing.T) {
	stdout, stderr, err := run(t, "1.2.3", "--version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "bsk version 1.2.3\n" {
		t.Errorf("stdout = %q, want %q", stdout, "bsk version 1.2.3\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestRootHelpShowsUsage(t *testing.T) {
	stdout, stderr, err := run(t, "dev", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"BookStack", "Usage:", "bsk [flags]", "--version"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output missing %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestRootWithoutArgsShowsHelp(t *testing.T) {
	stdout, _, err := run(t, "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Errorf("stdout missing usage:\n%s", stdout)
	}
}

func TestRootUnknownCommandFails(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := NewRootCmd("dev")
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"nope"})
	err := Execute(cmd)
	if code := ExitCode(err); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	want := `{"error":{"code":"usage","message":"unknown command \"nope\" for \"bsk\""}}` + "\n"
	if errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

// Error classification must not depend on each command remembering a wrapper:
// a command added without any wrapper still reports runtime failures as generic
// errors (exit 1) and bad arguments as usage errors (exit 2).
func TestExecuteClassifiesCommandsWithoutWrapper(t *testing.T) {
	newRoot := func(args ...string) (string, error) {
		var out, errOut bytes.Buffer
		root := NewRootCmd("dev")
		root.AddCommand(&cobra.Command{
			Use:  "boom",
			Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error { return errors.New("disk full") },
		})
		root.SetOut(&out)
		root.SetErr(&errOut)
		root.SetArgs(args)
		err := Execute(root)
		return errOut.String(), err
	}

	stderr, err := newRoot("boom")
	if code := ExitCode(err); code != 1 {
		t.Errorf("runtime failure exit code = %d, want 1", code)
	}
	if want := `{"error":{"code":"error","message":"disk full"}}` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}

	stderr, err = newRoot("boom", "extra")
	if code := ExitCode(err); code != 2 {
		t.Errorf("bad argument exit code = %d, want 2", code)
	}
	if want := `{"error":{"code":"usage","message":"unknown command \"extra\" for \"bsk boom\""}}` + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// F4: commands cobra injects at execution time (help, completion) follow the
// same classification: a bad argument is a usage error.
func TestExecuteClassifiesInjectedCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	root := NewRootCmd("dev")
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"completion", "bash", "extra"})
	err := Execute(root)
	if code := ExitCode(err); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	want := `{"error":{"code":"usage","message":"unknown command \"extra\" for \"bsk completion bash\""}}` + "\n"
	if errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
}

// The root help lists every exit code a command can return, like the
// subcommand helps do.
func TestRootHelpListsAllExitCodes(t *testing.T) {
	stdout, _, err := run(t, "dev", "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"0  ok", "1  error", "2  usage", "3  auth", "4  not found", "5  conflict", "6  validation"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("root help missing %q", want)
		}
	}
}
