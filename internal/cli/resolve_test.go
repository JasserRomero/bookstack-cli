package cli

import (
	"strings"
	"testing"
)

// Page list items for the slug lookup: deploy exists in books ops and dev.
const (
	page8      = `{"id":8,"book_id":7,"book_slug":"ops","chapter_id":0,"name":"Deploy (copy)","slug":"deploy","editor":"wysiwyg","priority":3,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1}`
	page12     = `{"id":12,"book_id":2,"book_slug":"dev","chapter_id":0,"name":"Deploy","slug":"deploy","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1}`
	page20     = `{"id":20,"book_id":30,"book_slug":"políticas-ti","chapter_id":0,"name":"Cómo desplegar","slug":"cómo-desplegar","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1}`
	deployList = "/api/pages?count=500&filter[slug]=deploy&offset=0"
)

// pageGetOutput is what "pages get 5" prints for pageGetBody.
const pageGetOutput = `{"id":5,"book_id":7,"chapter_id":9,"name":"Deploy","slug":"deploy","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":2,"created_at":"2026-10-01T10:00:00.000000Z","updated_at":"2026-10-20T08:30:15.000000Z","created_by":` + admin + `,"updated_by":` + admin + `,"owned_by":` + admin + `,"tags":[{"name":"env","value":"prod"}],"markdown":"Steps"}` + "\n"

func resolveRoutes() map[string]reply {
	return map[string]reply{
		deployList:                     ok(list(page12, page5, page6)),
		"/api/pages/5":                 ok(pageGetBody),
		"/api/pages/5/export/markdown": {200, "# Deploy\n\nSteps", "application/octet-stream"},
	}
}

// S1, S2, S4: a page URL on url prints exactly what "pages get" prints, after
// one filter[slug] list request; the candidate is picked by book slug.
// Query, fragment and trailing segments are ignored.
func TestPagesResolveURLOnURL(t *testing.T) {
	f := newFake(t, resolveRoutes())
	base := f.srv.URL
	for _, u := range []string{
		base + "/books/ops/page/deploy",
		base + "/books/ops/page/deploy/",
		base + "/books/ops/page/deploy?revision=2#bkmrk-step-1",
		base + "/books/ops/page/deploy/edit",
		base + "/books/ops/page/deploy/revisions",
		base + "/books/ops/page/%64eploy",
	} {
		t.Run(u, func(t *testing.T) {
			exec(t, "", "pages", "resolve-url", u).expect(t, 0, pageGetOutput, "")
		})
	}
	one := []string{deployReq, "GET /api/pages/5", "GET /api/pages/5/export/markdown"}
	var want []string
	for range 6 {
		want = append(want, one...)
	}
	f.expectRequests(t, want...)
}

const deployReq = "GET " + deployList

// S1, S4: --raw prints only the Markdown, like "pages get --raw".
func TestPagesResolveURLRaw(t *testing.T) {
	f := newFake(t, resolveRoutes())
	exec(t, "", "pages", "resolve-url", "--raw", f.srv.URL+"/books/ops/page/deploy").expect(t, 0, "Steps", "")
	f.expectRequests(t, deployReq, "GET /api/pages/5", "GET /api/pages/5/export/markdown")
}

// S1: resolve_url is accepted as base, with its path prefix, a host in any
// case and the default port written explicitly; url still works.
func TestPagesResolveURLOnResolveURL(t *testing.T) {
	f := newFake(t, resolveRoutes())
	t.Setenv("BOOKSTACK_RESOLVE_URL", "https://docs.example.com/wiki/")
	for _, u := range []string{
		"https://docs.example.com/wiki/books/ops/page/deploy",
		"https://DOCS.Example.COM/wiki/books/ops/page/deploy#bkmrk-x",
		"HTTPS://docs.example.com:443/wiki/books/ops/page/deploy/edit",
		f.srv.URL + "/books/ops/page/deploy",
	} {
		t.Run(u, func(t *testing.T) {
			exec(t, "", "pages", "resolve-url", u).expect(t, 0, pageGetOutput, "")
		})
	}
	one := []string{deployReq, "GET /api/pages/5", "GET /api/pages/5/export/markdown"}
	var want []string
	for range 4 {
		want = append(want, one...)
	}
	f.expectRequests(t, want...)
}

// S1: url itself may carry a path prefix, which the API requests also use.
func TestPagesResolveURLBasePathPrefix(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/bookstack" + deployList:                ok(list(page5)),
		"/bookstack/api/pages/5":                 ok(pageGetBody),
		"/bookstack/api/pages/5/export/markdown": {200, "# Deploy\n\nSteps", "application/octet-stream"},
	})
	t.Setenv("BOOKSTACK_URL", f.srv.URL+"/bookstack/")
	exec(t, "", "pages", "resolve-url", "--raw", f.srv.URL+"/bookstack/books/ops/page/deploy").expect(t, 0, "Steps", "")
	exec(t, "", "pages", "resolve-url", f.srv.URL+"/books/ops/page/deploy").expect(t, 2, "",
		errJSON("usage", 0, `url \"`+f.srv.URL+`/books/ops/page/deploy\" is not under the configured url or resolve_url (`+f.srv.URL+`/bookstack)`))
	f.expectRequests(t, "GET /bookstack"+deployList, "GET /bookstack/api/pages/5", "GET /bookstack/api/pages/5/export/markdown")
}

// S1, S2: percent-encoded non-ASCII slugs are decoded and sent as they are.
func TestPagesResolveURLNonASCII(t *testing.T) {
	f := newFake(t, map[string]reply{
		"/api/pages?count=500&filter[slug]=cómo-desplegar&offset=0": ok(list(page20)),
		"/api/pages/20":                 ok(page20),
		"/api/pages/20/export/markdown": {200, "# Cómo desplegar\n\nPaso 1", "application/octet-stream"},
	})
	exec(t, "", "pages", "resolve-url", "--raw", f.srv.URL+"/books/pol%C3%ADticas-ti/page/c%C3%B3mo-desplegar").expect(t, 0, "Paso 1", "")
	f.expectRequests(t, "GET /api/pages?count=500&filter[slug]=cómo-desplegar&offset=0", "GET /api/pages/20", "GET /api/pages/20/export/markdown")
}

// S2, S3: a slug found only in other books, or not at all, is not_found
// (exit 4, no status); several matches in the book are ambiguous (exit 1).
func TestPagesResolveURLNoUniqueMatch(t *testing.T) {
	f := newFake(t, map[string]reply{
		deployList: ok(list(page12, page8, page5)),
		"/api/pages?count=500&filter[slug]=rollback&offset=0": ok(list(page12)),
		"/api/pages?count=500&filter[slug]=missing&offset=0":  ok(list()),
	})
	base := f.srv.URL
	exec(t, "", "pages", "resolve-url", base+"/books/ops/page/rollback").expect(t, 4, "",
		errJSON("not_found", 0, `page \"rollback\" not found in book \"ops\"`))
	exec(t, "", "pages", "resolve-url", base+"/books/qa/page/deploy").expect(t, 4, "",
		errJSON("not_found", 0, `page \"deploy\" not found in book \"qa\"`))
	exec(t, "", "pages", "resolve-url", base+"/books/ops/page/missing").expect(t, 4, "",
		errJSON("not_found", 0, `page \"missing\" not found in book \"ops\"`))
	exec(t, "", "pages", "resolve-url", base+"/books/ops/page/deploy").expect(t, 1, "",
		errJSON("ambiguous", 0, `page \"deploy\" matches several pages in book \"ops\": ids 8, 5`))
	f.expectRequests(t,
		"GET /api/pages?count=500&filter[slug]=rollback&offset=0",
		deployReq,
		"GET /api/pages?count=500&filter[slug]=missing&offset=0",
		deployReq)
}

// S3: URLs of another host, scheme or port, and URLs that are not page URLs,
// are usage errors (exit 2) and no request is made.
func TestPagesResolveURLUsageErrors(t *testing.T) {
	f := newFake(t, nil)
	base := f.srv.URL
	host := strings.TrimPrefix(base, "http://")
	notUnder := func(u string) string {
		return `url \"` + u + `\" is not under the configured url or resolve_url (` + base + `)`
	}
	notPage := func(u string) string {
		return `url \"` + u + `\" is not a page URL: expected <base>/books/<book-slug>/page/<page-slug>`
	}
	invalid := func(u string) string {
		return `invalid url \"` + u + `\": must be an absolute http or https URL`
	}
	cases := []struct {
		args    []string
		message string
	}{
		{[]string{"https://other.example.com/books/ops/page/deploy"}, notUnder("https://other.example.com/books/ops/page/deploy")},
		{[]string{"https://" + host + "/books/ops/page/deploy"}, notUnder("https://" + host + "/books/ops/page/deploy")},
		{[]string{"http://127.0.0.1:1/books/ops/page/deploy"}, notUnder("http://127.0.0.1:1/books/ops/page/deploy")},
		{[]string{"ftp://" + host + "/books/ops/page/deploy"}, invalid("ftp://" + host + "/books/ops/page/deploy")},
		{[]string{"/books/ops/page/deploy"}, invalid("/books/ops/page/deploy")},
		{[]string{"books/ops/page/deploy"}, invalid("books/ops/page/deploy")},
		{[]string{base + "/books/ops"}, notPage(base + "/books/ops")},
		{[]string{base + "/books/ops/chapter/runbooks"}, notPage(base + "/books/ops/chapter/runbooks")},
		{[]string{base + "/shelves/infra"}, notPage(base + "/shelves/infra")},
		{[]string{base + "/books/ops/page/"}, notPage(base + "/books/ops/page/")},
		{[]string{base + "/books//page/deploy"}, notPage(base + "/books//page/deploy")},
		{[]string{base}, notPage(base)},
		{[]string{}, `accepts 1 arg(s), received 0`},
		{[]string{base + "/books/ops/page/deploy", "x"}, `accepts 1 arg(s), received 2`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			exec(t, "", append([]string{"pages", "resolve-url"}, tc.args...)...).expect(t, 2, "", errJSON("usage", 0, tc.message))
		})
	}
	f.expectRequests(t)
}

// S1, S3: the help documents the accepted URLs, what is ignored and the exit codes.
func TestPagesResolveURLHelp(t *testing.T) {
	isolate(t)
	r := exec(t, "", "pages", "resolve-url", "--help")
	for _, want := range []string{
		"resolve-url <url>", "--raw", "resolve_url", "/books/<book-slug>/page/<page-slug>",
		"query string and fragment", "/edit", "filter[slug]", "pages get",
		"Exit codes:", "2  usage", "4  not found", "ambiguous",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("resolve-url help missing %q:\n%s", want, r.stdout)
		}
	}
	if r := exec(t, "", "pages"); !strings.Contains(r.stdout, "resolve-url") {
		t.Errorf("pages help missing resolve-url:\n%s", r.stdout)
	}
}
