//go:build e2e

// Package e2e runs bsk against the disposable local BookStack of this
// directory (up.sh). It is built only with -tags e2e and runs only with
// BSK_E2E=1 and a BOOKSTACK_URL on 127.0.0.1 or localhost; "make e2e" does
// all of that. Never point it at a real BookStack instance.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Suite state set by TestMain.
var (
	skipReason string // non-empty when the suite is not enabled
	bin        string // the bsk binary built for this run
	baseURL    string
	tokenID    string
	secret     string
	runID      = time.Now().UTC().Format("20060102T150405.000000000")
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv("BSK_E2E") != "1" {
		skipReason = "set BSK_E2E=1 to run the end-to-end tests (make e2e)"
		return m.Run()
	}
	baseURL = strings.TrimRight(os.Getenv("BOOKSTACK_URL"), "/")
	tokenID = os.Getenv("BOOKSTACK_TOKEN_ID")
	secret = os.Getenv("BOOKSTACK_TOKEN_SECRET")
	if err := checkSandbox(baseURL); err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	if tokenID == "" || secret == "" {
		fmt.Fprintln(os.Stderr, "e2e: BOOKSTACK_TOKEN_ID and BOOKSTACK_TOKEN_SECRET are required; source test/e2e/.env.local")
		return 1
	}
	dir, err := os.MkdirTemp("", "bsk-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	bin = filepath.Join(dir, "bsk")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "./cmd/bsk")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: building bsk: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// checkSandbox refuses any BookStack that is not on this machine.
func checkSandbox(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return fmt.Errorf("BOOKSTACK_URL %q is not a URL; source test/e2e/.env.local", raw)
	}
	if h := u.Hostname(); h != "127.0.0.1" && h != "localhost" {
		return fmt.Errorf("refusing to run against %q: BOOKSTACK_URL must be on 127.0.0.1 or localhost (the sandbox of test/e2e/up.sh)", h)
	}
	return nil
}

// setup skips the test when the suite is not enabled.
func setup(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip(skipReason)
	}
}

// unique returns a name no other test or run uses.
func unique(t *testing.T, label string) string {
	return fmt.Sprintf("bsk-e2e %s %s %s", runID, t.Name(), label)
}

type result struct {
	stdout, stderr string
	code           int
}

// bsk runs the binary with stdin and an isolated config directory, with the
// sandbox connection in the environment. Every output is checked for the
// token secret.
func bsk(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	return bskEnv(t, isolatedEnv(t), stdin, args...)
}

// bskEnv is bsk with the given process environment.
func bskEnv(t *testing.T, env []string, stdin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	r := result{out.String(), errOut.String(), 0}
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("bsk %v: %v", args, err)
		}
		r.code = ee.ExitCode()
	}
	if strings.Contains(r.stdout+r.stderr, secret) {
		t.Fatalf("bsk %v printed the token secret", args)
	}
	return r
}

// isolatedEnv is the process environment without any BOOKSTACK_* or
// XDG_CONFIG_HOME value of the developer, plus a fresh config directory and
// the sandbox connection.
func isolatedEnv(t *testing.T) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "BOOKSTACK_") || strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"XDG_CONFIG_HOME="+t.TempDir(),
		"BOOKSTACK_URL="+baseURL,
		"BOOKSTACK_TOKEN_ID="+tokenID,
		"BOOKSTACK_TOKEN_SECRET="+secret,
	)
}

// ok fails unless r succeeded with an empty stderr, and decodes stdout into v
// when v is not nil.
func (r result) ok(t *testing.T, v any) {
	t.Helper()
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if v != nil {
		if err := json.Unmarshal([]byte(r.stdout), v); err != nil {
			t.Fatalf("stdout %q: %v", r.stdout, err)
		}
	}
}

// cliError is the JSON error bsk prints on stderr.
type cliError struct {
	Error struct {
		Code    string `json:"code"`
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// fails checks a failure: the exit code, an empty stdout and the error code
// and HTTP status (0 = none) on stderr. It returns the message.
func (r result) fails(t *testing.T, exit int, code string, status int) string {
	t.Helper()
	if r.code != exit {
		t.Errorf("exit = %d, want %d (stderr %q)", r.code, exit, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	var e cliError
	if err := json.Unmarshal([]byte(r.stderr), &e); err != nil {
		t.Fatalf("stderr %q: %v", r.stderr, err)
	}
	if e.Error.Code != code || e.Error.Status != status {
		t.Errorf("error = %+v, want code %q status %d", e.Error, code, status)
	}
	return e.Error.Message
}

// api calls the BookStack API directly, for setup bsk cannot do (a WYSIWYG
// page, an edit by someone else) and for cleanup. body is JSON or nil; the
// response is decoded into v when v is not nil.
func api(t *testing.T, method, path string, body, v any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Token "+tokenID+":"+secret)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		// The error names the URL, never the header with the secret.
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, data)
	}
	if v != nil {
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatalf("%s %s: %q: %v", method, path, data, err)
		}
	}
}

// entity is the part of a book, chapter or page the tests read.
type entity struct {
	ID        int    `json:"id"`
	BookID    int    `json:"book_id"`
	ChapterID int    `json:"chapter_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Editor    string `json:"editor"`
	UpdatedAt string `json:"updated_at"`
	Markdown  string `json:"markdown"`
	HTML      string `json:"html"`
	RawHTML   string `json:"raw_html"`
}

// newBook creates a uniquely named book with bsk and deletes it (to the
// recycle bin) when the test ends.
func newBook(t *testing.T) entity {
	t.Helper()
	var b entity
	bsk(t, "", "books", "create", "--name", unique(t, "book")).ok(t, &b)
	if b.ID == 0 {
		t.Fatal("books create returned no id")
	}
	deleteBookAtEnd(t, b.ID)
	return b
}

func deleteBookAtEnd(t *testing.T, id int) {
	t.Cleanup(func() { api(t, http.MethodDelete, fmt.Sprintf("/api/books/%d", id), nil, nil) })
}

// getPage reads a page with its content straight from the API.
func getPage(t *testing.T, id int) entity {
	t.Helper()
	var p entity
	api(t, http.MethodGet, fmt.Sprintf("/api/pages/%d", id), nil, &p)
	return p
}

// newWYSIWYGPage creates a page from HTML through the API, which stores it
// with the WYSIWYG editor; bsk only creates Markdown pages.
func newWYSIWYGPage(t *testing.T, bookID int, html string) entity {
	t.Helper()
	var p entity
	api(t, http.MethodPost, "/api/pages", map[string]any{"book_id": bookID, "name": unique(t, "wysiwyg"), "html": html}, &p)
	if p.Editor != "wysiwyg" {
		t.Fatalf("API page editor = %q, want wysiwyg", p.Editor)
	}
	return p
}
