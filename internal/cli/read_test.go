package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// reply is a canned response of the fake BookStack.
type reply struct {
	status      int
	body        string
	contentType string
}

func ok(body string) reply { return reply{http.StatusOK, body, "application/json"} }

// fakeAPI is an httptest BookStack. Routes are keyed by path, or by
// "path?query" with the query decoded, which takes precedence; a key prefixed
// with the method ("PUT /api/pages/5") takes precedence over both. Every
// request must carry the configured token and User-Agent.
type fakeAPI struct {
	t        *testing.T
	srv      *httptest.Server
	routes   map[string]reply
	wantAuth string
	wantUA   string

	mu     sync.Mutex
	reqs   []string
	bodies []string
	// seq holds successive replies for a "METHOD path" key, consumed in
	// order before routes is consulted.
	seq map[string][]reply
}

// newFake starts a fake BookStack and configures bsk to use it through the
// BOOKSTACK_* variables in an isolated config directory.
func newFake(t *testing.T, routes map[string]reply) *fakeAPI {
	t.Helper()
	isolate(t)
	noRetryWait(t)
	f := &fakeAPI{t: t, routes: routes, wantAuth: "Token tid:" + secret, wantUA: "bsk/dev"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	t.Setenv("BOOKSTACK_URL", f.srv.URL+"/")
	t.Setenv("BOOKSTACK_TOKEN_ID", "tid")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", secret)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	query, err := url.QueryUnescape(r.URL.RawQuery)
	if err != nil {
		f.t.Errorf("bad query %q", r.URL.RawQuery)
	}
	key := r.URL.Path
	if query != "" {
		key += "?" + query
	}
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Method+" "+key)
	f.bodies = append(f.bodies, string(body))
	queued := f.seq[r.Method+" "+key]
	var rep reply
	found := len(queued) > 0
	if found {
		rep, f.seq[r.Method+" "+key] = queued[0], queued[1:]
	}
	f.mu.Unlock()
	if got := r.Header.Get("Authorization"); got != f.wantAuth {
		f.t.Errorf("Authorization = %q, want %q", got, f.wantAuth)
	}
	if got := r.Header.Get("User-Agent"); got != f.wantUA {
		f.t.Errorf("User-Agent = %q, want %q", got, f.wantUA)
	}
	if !found {
		rep, found = f.routes[r.Method+" "+key]
	}
	if !found {
		rep, found = f.routes[key]
	}
	if !found {
		rep, found = f.routes[r.URL.Path]
	}
	if !found {
		f.t.Errorf("unexpected request %s", key)
		rep = reply{http.StatusTeapot, `{}`, "application/json"}
	}
	w.Header().Set("Content-Type", rep.contentType)
	w.WriteHeader(rep.status)
	_, _ = w.Write([]byte(rep.body))
}

// expectRequests asserts the exact requests received, in order.
func (f *fakeAPI) expectRequests(t *testing.T, want ...string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if want == nil {
		want = []string{}
	}
	got := append([]string{}, f.reqs...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requests = %q\nwant       %q", got, want)
	}
}

// expectBodies asserts the exact request bodies received, in order; a
// request without a body is "".
func (f *fakeAPI) expectBodies(t *testing.T, want ...string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if want == nil {
		want = []string{}
	}
	got := append([]string{}, f.bodies...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bodies = %q\nwant     %q", got, want)
	}
}

// noRetryWait makes client retries immediate.
func noRetryWait(t *testing.T) {
	t.Helper()
	orig := retrySleep
	t.Cleanup(func() { retrySleep = orig })
	retrySleep = func(context.Context, time.Duration) error { return nil }
}

// Fixtures are in the field order the CLI writes them, so a list output is
// the items joined into an array.
const (
	admin    = `{"id":1,"name":"Admin","slug":"admin"}`
	shelf1   = `{"id":1,"name":"Infra","slug":"infra","created_by":1,"updated_by":1,"owned_by":1}`
	shelf2   = `{"id":2,"name":"Dev","slug":"dev","created_by":1,"updated_by":1,"owned_by":1}`
	shelf3   = `{"id":3,"name":"Docs 2.0","slug":"docs-20","created_by":1,"updated_by":1,"owned_by":1}`
	book7    = `{"id":7,"name":"Ops","slug":"ops","created_by":1,"updated_by":1,"owned_by":1}`
	chapter9 = `{"id":9,"book_id":7,"book_slug":"ops","name":"Runbooks","slug":"runbooks","priority":1,"created_by":1,"updated_by":1,"owned_by":1}`
	page5    = `{"id":5,"book_id":7,"book_slug":"ops","chapter_id":9,"name":"Deploy","slug":"deploy","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":2,"created_at":"2026-10-01T10:00:00.000000Z","updated_at":"2026-10-20T08:30:15.000000Z","created_by":1,"updated_by":1,"owned_by":1}`
	page6    = `{"id":6,"book_id":7,"book_slug":"ops","chapter_id":0,"name":"Rollback","slug":"rollback","editor":"markdown","priority":2,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1}`
	hit5     = `{"id":5,"name":"Deploy","slug":"deploy","type":"page","url":"https://docs.example.com/books/ops/page/deploy","book_id":7,"chapter_id":9,"preview_html":{"name":"<strong>Deploy</strong>","content":"how to <strong>deploy</strong> & roll back"},"tags":[]}`
)

// list is a one-page list response holding items.
func list(items ...string) string {
	return `{"data":[` + strings.Join(items, ",") + `],"total":` + strconv.Itoa(len(items)) + `}`
}

// errJSON is the expected stderr line; status 0 means no status field.
func errJSON(code string, status int, message string) string {
	s := ""
	if status != 0 {
		s = `"status":` + strconv.Itoa(status) + `,`
	}
	return `{"error":{"code":"` + code + `",` + s + `"message":"` + message + `"}}` + "\n"
}

// S2, S1: test calls /api/system with the token and prints the documented
// object; the build version reaches the User-Agent.
func TestTestCommand(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/system": ok(`{"version":"v25.11.6","instance_id":"abc","app_name":"Team Docs"}`),
	})
	f.wantUA = "bsk/1.2.3"
	execVersion(t, "1.2.3", "", "test").expect(t, 0,
		`{"ok":true,"profile":null,"url":"`+f.srv.URL+`","token_id":"tid","version":"v25.11.6","app_name":"Team Docs"}`+"\n", "")
	f.expectRequests(t, "GET /api/system")
}

// S2, S5: an auth failure is the mapped error with exit 3.
func TestTestCommandAuthFailure(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/system": {401, `{"error":{"code":401,"message":"No authorization token found on the request"}}`, "application/json"},
	})
	exec(t, "", "test").expect(t, 3, "", errJSON("auth", 401, "No authorization token found on the request"))
	f.expectRequests(t, "GET /api/system")
}

// S1, S2: --profile selects the profile whose credentials are used.
func TestTestCommandWithProfile(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/system": ok(`{"version":"v25.11.6","instance_id":"abc","app_name":"Docs"}`),
	})
	path := isolate(t)
	writeConfig(t, path, "default_profile = \"work\"\n\n[profiles.work]\nurl = \"https://work.invalid\"\ntoken_id = \"work-id\"\ntoken_secret = \"work-secret\"\n\n"+
		"[profiles.home]\nurl = \""+f.srv.URL+"\"\ntoken_id = \"home-id\"\ntoken_secret = \""+secret+"\"\n")
	f.wantAuth = "Token home-id:" + secret
	exec(t, "", "--profile", "home", "test").expect(t, 0,
		`{"ok":true,"profile":"home","url":"`+f.srv.URL+`","token_id":"home-id","version":"v25.11.6","app_name":"Docs"}`+"\n", "")
	f.expectRequests(t, "GET /api/system")
}

// S1, S5: API commands report missing configuration exactly like T2.
func TestReadCommandsConfigMissing(t *testing.T) {
	f := newFake(t, nil)
	t.Setenv("BOOKSTACK_URL", "")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", "")
	want := `{"error":{"code":"config_missing","message":"missing: url, token_secret; set the BOOKSTACK_* environment variables or run \"bsk auth\""}}` + "\n"
	for _, args := range [][]string{{"test"}, {"search", "x"}, {"books", "list"}, {"pages", "get", "5"}} {
		exec(t, "", args...).expect(t, 1, "", want)
	}
	f.expectRequests(t)
}

// S1: search sends the query with {type:...} appended and count (default 20)
// and prints {"total":N,"data":[...]}.
func TestSearch(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/search?count=20&query=deploy":                 ok(`{"data":[` + hit5 + `],"total":1}`),
		"/api/search?count=5&query=deploy {type:page}":      ok(`{"data":[` + hit5 + `],"total":1}`),
		"/api/search?count=100&query=nada {type:bookshelf}": ok(`{"data":[],"total":0}`),
	})
	exec(t, "", "search", "deploy").expect(t, 0, `{"total":1,"data":[`+hit5+`]}`+"\n", "")
	exec(t, "", "search", "deploy", "--type", "page", "--count", "5").expect(t, 0, `{"total":1,"data":[`+hit5+`]}`+"\n", "")
	exec(t, "", "search", "--count", "100", "--type", "bookshelf", "nada").expect(t, 0, `{"total":0,"data":[]}`+"\n", "")
	f.expectRequests(t,
		"GET /api/search?count=20&query=deploy",
		"GET /api/search?count=5&query=deploy {type:page}",
		"GET /api/search?count=100&query=nada {type:bookshelf}")
}

// S1: invalid ids, filters, --type and --count are usage errors (exit 2)
// and no request is made.
func TestReadUsageErrors(t *testing.T) {
	f := newFake(t, nil)
	cases := []struct {
		args    []string
		message string
	}{
		{[]string{"shelves", "get", "abc"}, `invalid id \"abc\": must be a positive integer`},
		{[]string{"books", "get", "0"}, `invalid id \"0\": must be a positive integer`},
		{[]string{"chapters", "get", "--", "-1"}, `invalid id \"-1\": must be a positive integer`},
		{[]string{"chapters", "get", "-1"}, `unknown shorthand flag: '1' in -1`},
		{[]string{"pages", "get", "1.5"}, `invalid id \"1.5\": must be a positive integer`},
		{[]string{"pages", "get", "5", "6"}, `accepts 1 arg(s), received 2`},
		{[]string{"books", "get"}, `accepts 1 arg(s), received 0`},
		{[]string{"books", "list", "7"}, `unknown command \"7\" for \"bsk books list\"`},
		{[]string{"pages", "list", "--book", "abc"}, `invalid argument \"abc\" for \"--book\" flag: must be a positive integer`},
		{[]string{"pages", "list", "--book", "0"}, `invalid argument \"0\" for \"--book\" flag: must be a positive integer`},
		{[]string{"pages", "list", "--chapter=-1"}, `invalid argument \"-1\" for \"--chapter\" flag: must be a positive integer`},
		{[]string{"chapters", "list", "--book", "x7"}, `invalid argument \"x7\" for \"--book\" flag: must be a positive integer`},
		{[]string{"search", "deploy", "--type", "attachment"}, `invalid --type \"attachment\": must be one of page, chapter, book, bookshelf`},
		{[]string{"search", "deploy", "--count", "0"}, `invalid --count 0: must be between 1 and 100`},
		{[]string{"search", "deploy", "--count", "101"}, `invalid --count 101: must be between 1 and 100`},
		{[]string{"search"}, `accepts 1 arg(s), received 0`},
		{[]string{"search", "  "}, `empty search query`},
		{[]string{"test", "extra"}, `unknown command \"extra\" for \"bsk test\"`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			exec(t, "", tc.args...).expect(t, 2, "", errJSON("usage", 0, tc.message))
		})
	}
	f.expectRequests(t)
}

// S1: list prints every item across pages as one JSON array.
func TestShelvesListPaginates(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/shelves?count=500&offset=0": ok(`{"data":[` + shelf1 + `,` + shelf2 + `],"total":3}`),
		"/api/shelves?count=500&offset=2": ok(`{"data":[` + shelf3 + `],"total":3}`),
	})
	exec(t, "", "shelves", "list").expect(t, 0, `[`+shelf1+`,`+shelf2+`,`+shelf3+`]`+"\n", "")
	f.expectRequests(t, "GET /api/shelves?count=500&offset=0", "GET /api/shelves?count=500&offset=2")
}

// S1: get prints one object; a shelf includes books, a book its contents,
// a chapter its pages, even when empty.
func TestGetCommands(t *testing.T) {
	shelf := `{"id":3,"name":"Docs 2.0","slug":"docs-20","created_by":` + admin + `,"updated_by":` + admin + `,"owned_by":` + admin + `,"tags":[{"name":"area","value":"ti"}],"books":[` + book7 + `]}`
	contents := `[{"id":9,"name":"Runbooks","slug":"runbooks","type":"chapter","book_id":7,"priority":1,"url":"https://docs.example.com/books/ops/chapter/runbooks","pages":[{"id":5,"name":"Deploy","slug":"deploy","book_id":7,"chapter_id":9,"priority":1,"url":"https://docs.example.com/books/ops/page/deploy"}]}]`
	book := `{"id":7,"name":"Ops","slug":"ops","created_by":` + admin + `,"updated_by":` + admin + `,"owned_by":` + admin + `,"contents":` + contents + `}`
	emptyBook := `{"id":8,"name":"Portal 360","slug":"portal-360","created_by":1,"updated_by":1,"owned_by":1}`
	chapter := `{"id":9,"book_id":7,"name":"Runbooks","slug":"runbooks","priority":1,"created_by":1,"updated_by":1,"owned_by":1,"pages":[{"id":5,"book_id":7,"chapter_id":9,"name":"Deploy","slug":"deploy","priority":1,"draft":false,"template":false,"revision_count":2,"created_by":1,"updated_by":1,"owned_by":1}]}`
	f := newFake(t, map[string]reply{
		"/api/shelves/3":  ok(shelf),
		"/api/shelves/4":  ok(`{"id":4,"name":"Empty","slug":"empty","created_by":1,"updated_by":1,"owned_by":1}`),
		"/api/books/7":    ok(book),
		"/api/books/8":    ok(emptyBook),
		"/api/chapters/9": ok(chapter),
	})
	exec(t, "", "shelves", "get", "3").expect(t, 0, shelf+"\n", "")
	exec(t, "", "shelves", "get", "4").expect(t, 0, `{"id":4,"name":"Empty","slug":"empty","created_by":1,"updated_by":1,"owned_by":1,"books":[]}`+"\n", "")
	exec(t, "", "books", "get", "7").expect(t, 0, book+"\n", "")
	exec(t, "", "books", "get", "8").expect(t, 0, strings.TrimSuffix(emptyBook, "}")+`,"contents":[]}`+"\n", "")
	exec(t, "", "chapters", "get", "9").expect(t, 0, chapter+"\n", "")
	f.expectRequests(t, "GET /api/shelves/3", "GET /api/shelves/4", "GET /api/books/7", "GET /api/books/8", "GET /api/chapters/9")
}

// S1, S3: books/chapters/pages list; --book and --chapter are server-side filters.
func TestListCommandsAndFilters(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/books?count=500&offset=0":                                        ok(list(book7)),
		"/api/chapters?count=500&offset=0":                                     ok(list(chapter9)),
		"/api/chapters?count=500&filter[book_id]=7&offset=0":                   ok(list(chapter9)),
		"/api/pages?count=500&offset=0":                                        ok(list(page5, page6)),
		"/api/pages?count=500&filter[book_id]=7&offset=0":                      ok(list(page5, page6)),
		"/api/pages?count=500&filter[book_id]=7&filter[chapter_id]=9&offset=0": ok(list(page5)),
		"/api/pages?count=500&filter[chapter_id]=10&offset=0":                  ok(list()),
	})
	exec(t, "", "books", "list").expect(t, 0, `[`+book7+`]`+"\n", "")
	exec(t, "", "chapters", "list").expect(t, 0, `[`+chapter9+`]`+"\n", "")
	exec(t, "", "chapters", "list", "--book", "7").expect(t, 0, `[`+chapter9+`]`+"\n", "")
	exec(t, "", "pages", "list").expect(t, 0, `[`+page5+`,`+page6+`]`+"\n", "")
	exec(t, "", "pages", "list", "--book", "7").expect(t, 0, `[`+page5+`,`+page6+`]`+"\n", "")
	exec(t, "", "pages", "list", "--chapter", "9", "--book", "7").expect(t, 0, `[`+page5+`]`+"\n", "")
	exec(t, "", "pages", "list", "--chapter", "10").expect(t, 0, "[]\n", "")
	f.expectRequests(t,
		"GET /api/books?count=500&offset=0",
		"GET /api/chapters?count=500&offset=0",
		"GET /api/chapters?count=500&filter[book_id]=7&offset=0",
		"GET /api/pages?count=500&offset=0",
		"GET /api/pages?count=500&filter[book_id]=7&offset=0",
		"GET /api/pages?count=500&filter[book_id]=7&filter[chapter_id]=9&offset=0",
		"GET /api/pages?count=500&filter[chapter_id]=10&offset=0")
}

const pageGetBody = `{"id":5,"book_id":7,"chapter_id":9,"name":"Deploy","slug":"deploy","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":2,"created_at":"2026-10-01T10:00:00.000000Z","updated_at":"2026-10-20T08:30:15.000000Z","created_by":` + admin + `,"updated_by":` + admin + `,"owned_by":` + admin + `,"html":"<p>Hi</p>","raw_html":"<p>Hi</p>","markdown":"","tags":[{"name":"env","value":"prod"}]}`

// S4: pages get has the page fields, markdown from the export, no html/raw_html,
// and updated_at verbatim; an empty export is still reported.
func TestPagesGet(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/pages/5":                 ok(pageGetBody),
		"/api/pages/5/export/markdown": {200, "# Deploy\n\nCómo \"desplegar\"", "application/octet-stream"},
		"/api/pages/6":                 ok(`{"id":6,"book_id":7,"chapter_id":0,"name":"Blank","slug":"blank","editor":"markdown","priority":0,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1,"html":"","markdown":""}`),
		"/api/pages/6/export/markdown": {200, "", "application/octet-stream"},
	})
	exec(t, "", "pages", "get", "5").expect(t, 0,
		`{"id":5,"book_id":7,"chapter_id":9,"name":"Deploy","slug":"deploy","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":2,"created_at":"2026-10-01T10:00:00.000000Z","updated_at":"2026-10-20T08:30:15.000000Z","created_by":`+admin+`,"updated_by":`+admin+`,"owned_by":`+admin+`,"tags":[{"name":"env","value":"prod"}],"markdown":"Cómo \"desplegar\""}`+"\n", "")
	exec(t, "", "pages", "get", "6").expect(t, 0,
		`{"id":6,"book_id":7,"chapter_id":0,"name":"Blank","slug":"blank","editor":"markdown","priority":0,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1,"markdown":""}`+"\n", "")
	f.expectRequests(t, "GET /api/pages/5", "GET /api/pages/5/export/markdown", "GET /api/pages/6", "GET /api/pages/6/export/markdown")
}

// S4: --raw prints the export body byte-for-byte (minus BookStack's prepended
// title heading, absent here because it is followed by CRLF), with no added newline.
func TestPagesGetRaw(t *testing.T) {
	body := "# Deploy\r\n\nCómo <b>desplegar</b> & \"más\"\n\n"
	f := newFake(t, map[string]reply{
		"/api/pages/5":                 ok(pageGetBody),
		"/api/pages/5/export/markdown": {200, body, "application/octet-stream"},
		"/api/pages/7":                 ok(strings.Replace(pageGetBody, `"id":5`, `"id":7`, 1)),
		"/api/pages/7/export/markdown": {200, "no newline", "application/octet-stream"},
	})
	exec(t, "", "pages", "get", "5", "--raw").expect(t, 0, body, "")
	exec(t, "", "pages", "get", "--raw", "7").expect(t, 0, "no newline", "")
	f.expectRequests(t, "GET /api/pages/5", "GET /api/pages/5/export/markdown", "GET /api/pages/7", "GET /api/pages/7/export/markdown")
}

// S5: API errors map to exit codes and carry the HTTP status when there is one;
// the secret never appears, even when the server echoes it.
func TestAPIErrorMapping(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/books/99":                {404, "<html><body>Not here</body></html>", "text/html"},
		"/api/pages/5":                 {403, `{"error":{"code":403,"message":"You do not have permission"}}`, "application/json"},
		"/api/pages/6":                 ok(pageGetBody),
		"/api/pages/6/export/markdown": {404, `{"error":{"code":404,"message":"Page not found"}}`, "application/json"},
		"/api/search?count=20&query=x": {422, `{"error":{"code":422,"message":"The given data was invalid.","validation":{"query":["The query field is required."]}}}`, "application/json"},
		"/api/shelves/1":               {500, `{"error":{"code":500,"message":"boom Token tid:` + secret + `"}}`, "application/json"},
		"/api/chapters/2":              {429, `{"error":{"code":429,"message":"Too Many Attempts."}}`, "application/json"},
		"/api/books/3":                 {409, `{"error":{"code":409,"message":"Conflict here"}}`, "application/json"},
		"/api/books/4":                 ok(`<html>login</html>`),
	})
	exec(t, "", "books", "get", "99").expect(t, 4, "", errJSON("not_found", 404, "Not Found"))
	exec(t, "", "pages", "get", "5").expect(t, 3, "", errJSON("auth", 403, "You do not have permission"))
	exec(t, "", "pages", "get", "6").expect(t, 4, "", errJSON("not_found", 404, "Page not found"))
	exec(t, "", "search", "x").expect(t, 6, "", errJSON("validation", 422, "The given data was invalid. query: The query field is required."))
	exec(t, "", "shelves", "get", "1").expect(t, 1, "", errJSON("server", 500, "boom Token tid:[REDACTED]"))
	exec(t, "", "chapters", "get", "2").expect(t, 1, "", errJSON("rate_limited", 429, "Too Many Attempts."))
	exec(t, "", "books", "get", "3").expect(t, 1, "", errJSON("http_error", 409, "Conflict here"))
	exec(t, "", "books", "get", "4").expect(t, 1, "", errJSON("invalid_response", 200, "invalid JSON response from GET /api/books/4: invalid character '<' looking for beginning of value"))
	f.expectRequests(t,
		"GET /api/books/99",
		"GET /api/pages/5",
		"GET /api/pages/6", "GET /api/pages/6/export/markdown",
		"GET /api/search?count=20&query=x",
		"GET /api/shelves/1",
		"GET /api/chapters/2", "GET /api/chapters/2", "GET /api/chapters/2", "GET /api/chapters/2", "GET /api/chapters/2",
		"GET /api/books/3",
		"GET /api/books/4")
}

// S5: a network failure is code network, exit 1, with no status.
func TestAPINetworkError(t *testing.T) {
	isolate(t)
	noRetryWait(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	t.Setenv("BOOKSTACK_URL", srv.URL)
	t.Setenv("BOOKSTACK_TOKEN_ID", "tid")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", secret)
	host := strings.TrimPrefix(srv.URL, "http://")
	// The reason after the address is OS text ("connect: connection refused"
	// on Unix, "connectex: ..." on Windows).
	exec(t, "", "test").expectErrorPrefix(t, 1, "network", "request failed: dial tcp "+host+": ")
}

// S5: an unusable configured URL is a configuration error, not a crash.
func TestAPIInvalidConfiguredURL(t *testing.T) {
	isolate(t)
	t.Setenv("BOOKSTACK_URL", "docs.example.com")
	t.Setenv("BOOKSTACK_TOKEN_ID", "tid")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", secret)
	exec(t, "", "books", "list").expect(t, 1, "",
		errJSON("config_invalid", 0, `invalid url \"docs.example.com\": must be an absolute http or https URL`))
}

// S5: help texts document the commands, their flags and the exit codes.
func TestHelpDocumentsReadCommands(t *testing.T) {
	isolate(t)
	r := exec(t, "", "--help")
	for _, want := range []string{"test", "search", "shelves", "books", "chapters", "pages", `"status"`, "3 ", "4 ", "6 "} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("root help missing %q:\n%s", want, r.stdout)
		}
	}
	exitCodes := []string{"Exit codes:", "3  auth", "4  not found", "6  validation"}
	helps := map[string][]string{
		"test":          {"/api/system", "never printed"},
		"search":        {"--type", "--count", "{type:page}", "page, chapter, book, bookshelf", "1-100", "default 20"},
		"shelves list":  {"JSON array"},
		"shelves get":   {"books"},
		"books list":    {"JSON array"},
		"books get":     {"contents"},
		"chapters list": {"--book"},
		"chapters get":  {"pages"},
		"pages list":    {"--book", "--chapter", "server-side"},
		"pages get":     {"--raw", "/export/markdown", "markdown", "html"},
	}
	for name, wants := range helps {
		r := exec(t, "", append(strings.Fields(name), "--help")...)
		for _, want := range append(wants, exitCodes...) {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("%s help missing %q:\n%s", name, want, r.stdout)
			}
		}
	}
	for _, group := range []string{"shelves", "books", "chapters", "pages"} {
		r := exec(t, "", group)
		if r.code != 0 || !strings.Contains(r.stdout, "list") || !strings.Contains(r.stdout, "get") {
			t.Errorf("%s without subcommand: code %d, stdout:\n%s", group, r.code, r.stdout)
		}
	}
}

// BookStack's Markdown export prepends "# <page name>" to the content. pages get
// (JSON and --raw) removes exactly that one heading, so what an agent reads can
// be written back without duplicating the title.
func TestPagesGetStripsExportTitle(t *testing.T) {
	page := func(id int, name string) reply {
		return ok(fmt.Sprintf(`{"id":%d,"book_id":7,"chapter_id":0,"name":%q,"slug":"s","editor":"markdown","priority":0,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1}`, id, name))
	}
	export := func(body string) reply { return reply{200, body, "application/octet-stream"} }
	cases := []struct {
		name, export, want string
	}{
		{"Deploy", "# Deploy\n\nbody", "body"},
		{"Título", "# Título\n\n# Título\n\nya empieza con el título", "# Título\n\nya empieza con el título"},
		{"a *b* #c [d] <x> & y", "# a *b* #c [d] <x> & y\n\ncuerpo", "cuerpo"},
		{"Deploy", "# Deploy", ""},
		{"Deploy", "# Deploy\n", ""},
		{"Deploy", "# Other\n\nbody", "# Other\n\nbody"},
		{"Deploy", "# Deployment\n\nbody", "# Deployment\n\nbody"},
		{"Deploy", "intro\n\n# Deploy\n\nbody", "intro\n\n# Deploy\n\nbody"},
		{"Deploy", "", ""},
	}
	routes := map[string]reply{}
	for i, c := range cases {
		id := 100 + i
		routes[fmt.Sprintf("/api/pages/%d", id)] = page(id, c.name)
		routes[fmt.Sprintf("/api/pages/%d/export/markdown", id)] = export(c.export)
	}
	newFake(t, routes)
	for i, c := range cases {
		id := strconv.Itoa(100 + i)
		t.Run(id, func(t *testing.T) {
			r := exec(t, "", "pages", "get", id)
			var got struct{ Markdown string }
			if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
				t.Fatalf("stdout %q: %v (stderr %q)", r.stdout, err, r.stderr)
			}
			if got.Markdown != c.want {
				t.Errorf("markdown = %q, want %q", got.Markdown, c.want)
			}
			exec(t, "", "pages", "get", id, "--raw").expect(t, 0, c.want, "")
		})
	}
}
