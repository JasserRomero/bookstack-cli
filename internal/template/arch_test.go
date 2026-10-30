package template

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// S8: the template package only loads, validates and renders: it must not
// depend on cobra, do HTTP or write to stdout/stderr, so the CLI owns output
// and the client owns requests.
func TestTemplateBoundaries(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "github.com/spf13/") || dep == "net/http" ||
			strings.HasSuffix(dep, "/internal/client") || strings.HasSuffix(dep, "/internal/cli") {
			t.Errorf("internal/template depends on %s", dep)
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"os.Stdout", "os.Stderr", "fmt.Print", `"log"`} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s uses %s", f, banned)
			}
		}
	}
}
