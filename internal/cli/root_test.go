package cli

import (
	"bytes"
	"strings"
	"testing"
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
	_, stderr, err := run(t, "dev", "nope")
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(stderr, `unknown command "nope" for "bsk"`) {
		t.Errorf("stderr = %q, want unknown command message", stderr)
	}
}
