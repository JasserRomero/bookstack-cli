package client

import (
	"os/exec"
	"strings"
	"testing"
)

// The client package must stay reusable outside the CLI (for example by a
// future MCP server), so it must not depend on cobra, directly or transitively.
func TestClientDoesNotDependOnCobra(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/spf13/") {
			t.Errorf("internal/client depends on %s", dep)
		}
	}
}
