package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writtenMeta is a write response without its content fields, in the order
// the CLI prints a page; written inserts the content fields BookStack adds.
const writtenMeta = `{"id":12,"book_id":7,"chapter_id":9,"name":"New","slug":"new","editor":"markdown","priority":3,"draft":false,"template":false,"revision_count":1,"created_at":"2026-10-30T10:00:00.000000Z","updated_at":"2026-10-30T10:00:00.000000Z","created_by":` + admin + `,"updated_by":` + admin + `,"owned_by":` + admin + `,"tags":[{"name":"env","value":"prod"}]}`

var written = strings.Replace(writtenMeta, `,"tags":`, `,"html":"<p>x</p>","raw_html":"<p>x</p>","markdown":"x","tags":`, 1)

// Pages as returned by GET /api/pages/<id>, with their content.
const (
	mdPage   = `{"id":6,"book_id":7,"chapter_id":0,"name":"Rollback","slug":"rollback","editor":"markdown","priority":2,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1,"html":"<p>old</p>","raw_html":"<p>old</p>","markdown":"old\n\n\n","tags":[{"name":"env","value":"prod"}]}`
	emptyMD  = `{"id":8,"book_id":7,"chapter_id":0,"name":"Blank","slug":"blank","editor":"markdown","priority":2,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1,"html":"","raw_html":"","markdown":""}`
	wysPage  = `{"id":5,"book_id":7,"chapter_id":9,"name":"Deploy","slug":"deploy","editor":"wysiwyg","priority":1,"draft":false,"template":false,"revision_count":2,"created_by":1,"updated_by":1,"owned_by":1,"html":"<p>Intro</p>\n<p>Included text</p>","raw_html":"<p>Intro</p>\n<p>{{@3}}</p>","markdown":""}`
	noEditor = `{"id":4,"book_id":7,"chapter_id":0,"name":"Legacy","slug":"legacy","priority":1,"draft":false,"template":false,"revision_count":1,"created_by":1,"updated_by":1,"owned_by":1,"html":"<p>legacy</p>","raw_html":"<p>legacy</p>"}`
)

// gfm exercises every GFM extension and raw HTML; gfmHTML is its conversion.
const gfm = "| a | b |\n|---|---|\n| 1 | ~~2~~ |\n\n- [x] done\n- [ ] todo\n\nSee https://example.com & **more**\n\n<div class=\"note\">raw <em>html</em></div>\n"

const gfmHTML = "<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>1</td>\n<td><del>2</del></td>\n</tr>\n</tbody>\n</table>\n" +
	"<ul>\n<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> done</li>\n<li><input disabled=\"\" type=\"checkbox\"> todo</li>\n</ul>\n" +
	"<p>See <a href=\"https://example.com\">https://example.com</a> &amp; <strong>more</strong></p>\n" +
	"<div class=\"note\">raw <em>html</em></div>\n"

// jstr is s as a JSON string, HTML not escaped, as the client sends it.
func jstr(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// tempFile writes content to a new file and returns its path.
func tempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "page.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// S1, S4, S5: create sends Markdown with the parent, name and tags given and
// prints the page metadata; without --tag no tags are sent; - reads stdin.
func TestPagesCreate(t *testing.T) {
	f := newFake(t, map[string]reply{"POST /api/pages": ok(written)})
	file := tempFile(t, "# Title\n\n<b>body</b> & más\n")
	exec(t, "", "pages", "create", "--book", "7", "--name", "New", "--file", file, "--tag", "env=prod", "--tag", "draft", "--tag", "k=a=b").
		expect(t, 0, writtenMeta+"\n", "")
	exec(t, "from stdin", "pages", "create", "--chapter", "9", "--name", "New", "--file", "-").expect(t, 0, writtenMeta+"\n", "")
	f.expectRequests(t, "POST /api/pages", "POST /api/pages")
	f.expectBodies(t,
		`{"book_id":7,"name":"New","markdown":"# Title\n\n<b>body</b> & más\n","tags":[{"name":"env","value":"prod"},{"name":"draft","value":""},{"name":"k","value":"a=b"}]}`,
		`{"chapter_id":9,"name":"New","markdown":"from stdin"}`)
}

// S2, S4, S5: an update without content sends only the given fields, with
// no prior GET; --tag replaces the tag set.
func TestPagesUpdateMetadataOnly(t *testing.T) {
	f := newFake(t, map[string]reply{"PUT /api/pages/12": ok(written)})
	exec(t, "", "pages", "update", "12", "--name", "Renamed").expect(t, 0, writtenMeta+"\n", "")
	exec(t, "", "pages", "update", "12", "--tag", "env=prod").expect(t, 0, writtenMeta+"\n", "")
	exec(t, "", "pages", "update", "--tag", "a", "--name", "N", "--tag", "b=", "12").expect(t, 0, writtenMeta+"\n", "")
	f.expectRequests(t, "PUT /api/pages/12", "PUT /api/pages/12", "PUT /api/pages/12")
	f.expectBodies(t,
		`{"name":"Renamed"}`,
		`{"tags":[{"name":"env","value":"prod"}]}`,
		`{"name":"N","tags":[{"name":"a","value":""},{"name":"b","value":""}]}`)
}

// S2, S8: --file replaces the content in the page's editor format: Markdown
// for a Markdown page, converted HTML (GFM, raw HTML kept) otherwise.
func TestPagesUpdateFileKeepsEditor(t *testing.T) {
	f := newFake(t, map[string]reply{
		"GET /api/pages/6": ok(mdPage), "PUT /api/pages/6": ok(written),
		"GET /api/pages/5": ok(wysPage), "PUT /api/pages/5": ok(written),
		"GET /api/pages/4": ok(noEditor), "PUT /api/pages/4": ok(written),
	})
	file := tempFile(t, gfm)
	exec(t, "", "pages", "update", "6", "--file", file, "--name", "N").expect(t, 0, writtenMeta+"\n", "")
	exec(t, "", "pages", "update", "5", "--file", file, "--tag", "x=y").expect(t, 0, writtenMeta+"\n", "")
	exec(t, gfm, "pages", "update", "4", "--file", "-").expect(t, 0, writtenMeta+"\n", "")
	f.expectRequests(t, "GET /api/pages/6", "PUT /api/pages/6", "GET /api/pages/5", "PUT /api/pages/5", "GET /api/pages/4", "PUT /api/pages/4")
	f.expectBodies(t,
		"", `{"name":"N","markdown":`+jstr(t, gfm)+`}`,
		"", `{"html":`+jstr(t, gfmHTML)+`,"tags":[{"name":"x","value":"y"}]}`,
		"", `{"html":`+jstr(t, gfmHTML)+`}`)
}

// S3: append on a Markdown page joins the Markdown source with one blank
// line after trimming its trailing newlines, with no separator when empty.
func TestPagesUpdateAppendMarkdown(t *testing.T) {
	f := newFake(t, map[string]reply{
		"GET /api/pages/6": ok(mdPage), "PUT /api/pages/6": ok(written),
		"GET /api/pages/8": ok(emptyMD), "PUT /api/pages/8": ok(written),
	})
	file := tempFile(t, "new <b>part</b>\n")
	exec(t, "", "pages", "update", "6", "--append-file", file).expect(t, 0, writtenMeta+"\n", "")
	exec(t, "new <b>part</b>\n", "pages", "update", "8", "--append-file", "-").expect(t, 0, writtenMeta+"\n", "")
	f.expectRequests(t, "GET /api/pages/6", "PUT /api/pages/6", "GET /api/pages/8", "PUT /api/pages/8")
	f.expectBodies(t,
		"", `{"markdown":"old\n\nnew <b>part</b>\n"}`,
		"", `{"markdown":"new <b>part</b>\n"}`)
}

// S3, S8: append on a WYSIWYG page adds the converted HTML to raw_html, so
// an include such as {{@3}} survives instead of its expanded html.
func TestPagesUpdateAppendWysiwygKeepsIncludes(t *testing.T) {
	f := newFake(t, map[string]reply{"GET /api/pages/5": ok(wysPage), "PUT /api/pages/5": ok(written)})
	exec(t, "new **part**\n", "pages", "update", "5", "--append-file", "-", "--tag", "k").expect(t, 0, writtenMeta+"\n", "")
	f.expectRequests(t, "GET /api/pages/5", "PUT /api/pages/5")
	f.expectBodies(t, "", `{"html":"<p>Intro</p>\n<p>{{@3}}</p><p>new <strong>part</strong></p>\n","tags":[{"name":"k","value":""}]}`)
}

// S7, S1, S2, S4: invalid input is a usage error naming the flag, and no
// request is made.
func TestPagesWriteUsageErrors(t *testing.T) {
	f := newFake(t, nil)
	md := tempFile(t, "ok")
	dir := t.TempDir()
	big := tempFile(t, strings.Repeat("a", maxContentBytes+1))
	bad := tempFile(t, "ok \xff")
	blank := tempFile(t, " \n\t\r\n ")
	missing := filepath.Join(dir, "missing.md")
	cases := []struct {
		name, stdin string
		args        []string
		message     string
	}{
		{"create no parent", "", []string{"pages", "create", "--name", "N", "--file", md}, `exactly one of --book or --chapter is required`},
		{"create both parents", "", []string{"pages", "create", "--book", "7", "--chapter", "9", "--name", "N", "--file", md}, `exactly one of --book or --chapter is required`},
		{"create no name", "", []string{"pages", "create", "--book", "7", "--file", md}, `--name is required`},
		{"create blank name", "", []string{"pages", "create", "--book", "7", "--name", " ", "--file", md}, `invalid --name: must not be blank`},
		{"create no file", "", []string{"pages", "create", "--book", "7", "--name", "N"}, `--file is required: BookStack rejects a page without content`},
		{"create bad book", "", []string{"pages", "create", "--book", "x", "--name", "N", "--file", md}, `invalid argument \"x\" for \"--book\" flag: must be a positive integer`},
		{"create args", "", []string{"pages", "create", "7"}, `unknown command \"7\" for \"bsk pages create\"`},
		{"create empty tag key", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", md, "--tag", "=v"}, `invalid argument \"=v\" for \"--tag\" flag: key must not be empty`},
		{"create missing file", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", missing}, `invalid --file \"` + missing + `\": cannot read: no such file or directory`},
		{"create directory", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", dir}, `invalid --file \"` + dir + `\": cannot read: is a directory`},
		{"create too big", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", big}, `invalid --file \"` + big + `\": larger than 10 MiB`},
		{"create invalid utf8", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", bad}, `invalid --file \"` + bad + `\": not valid UTF-8`},
		{"create blank file", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", blank}, `invalid --file \"` + blank + `\": empty or only whitespace`},
		{"create empty stdin", "", []string{"pages", "create", "--book", "7", "--name", "N", "--file", "-"}, `invalid --file \"-\": empty or only whitespace`},
		{"update nothing", "", []string{"pages", "update", "12"}, `nothing to update: give at least one of --name, --file, --append-file, --tag`},
		{"update bad id", "", []string{"pages", "update", "abc", "--name", "N"}, `invalid id \"abc\": must be a positive integer`},
		{"update no id", "", []string{"pages", "update", "--name", "N"}, `accepts 1 arg(s), received 0`},
		{"update blank name", "", []string{"pages", "update", "12", "--name", ""}, `invalid --name: must not be blank`},
		{"update file and append", "", []string{"pages", "update", "12", "--file", md, "--append-file", md}, `--file and --append-file are mutually exclusive`},
		{"update stdin twice", "x", []string{"pages", "update", "12", "--file", "-", "--append-file", "-"}, `stdin (\"-\") can be read by only one flag; --file and --append-file both use it`},
		{"update tag key", "", []string{"pages", "update", "12", "--tag", "=v"}, `invalid argument \"=v\" for \"--tag\" flag: key must not be empty`},
		{"update blank tag key", "", []string{"pages", "update", "12", "--tag", " "}, `invalid argument \" \" for \"--tag\" flag: key must not be empty`},
		{"update missing file", "", []string{"pages", "update", "12", "--file", missing}, `invalid --file \"` + missing + `\": cannot read: no such file or directory`},
		{"append missing", "", []string{"pages", "update", "12", "--append-file", missing}, `invalid --append-file \"` + missing + `\": cannot read: no such file or directory`},
		{"append too big", "", []string{"pages", "update", "12", "--append-file", big}, `invalid --append-file \"` + big + `\": larger than 10 MiB`},
		{"append invalid utf8", "", []string{"pages", "update", "12", "--append-file", bad}, `invalid --append-file \"` + bad + `\": not valid UTF-8`},
		{"append blank stdin", " \n", []string{"pages", "update", "12", "--append-file", "-"}, `invalid --append-file \"-\": empty or only whitespace`},
		{"append stdin invalid utf8", "\xff\xfe", []string{"pages", "update", "12", "--append-file", "-"}, `invalid --append-file \"-\": not valid UTF-8`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec(t, tc.stdin, tc.args...).expect(t, 2, "", errJSON("usage", 0, tc.message))
		})
	}
	f.expectRequests(t)
}

// S7: the 10 MiB limit is inclusive; stdin is limited the same way.
func TestReadContentLimit(t *testing.T) {
	exact := strings.Repeat("a", maxContentBytes)
	got, err := readContent("--file", "-", strings.NewReader(exact))
	if err != nil || got != exact {
		t.Fatalf("10 MiB input: len %d, err %v", len(got), err)
	}
	_, err = readContent("--file", "-", strings.NewReader(exact+"a"))
	if err == nil || err.Error() != `invalid --file "-": larger than 10 MiB` {
		t.Fatalf("10 MiB + 1 input: err %v", err)
	}
}

// S5: a 422 surfaces the field details with exit 6; other API errors follow
// T4 S5: a 5xx write is not retried and the secret is redacted; a missing
// page stops before the write.
func TestPagesWriteAPIErrors(t *testing.T) {
	f := newFake(t, map[string]reply{
		"POST /api/pages":   {422, `{"error":{"code":422,"message":"The given data was invalid.","validation":{"name":["The name field is required."],"markdown":["The markdown field is required when html is not present."]}}}`, "application/json"},
		"PUT /api/pages/12": {422, `{"error":{"code":422,"message":"The given data was invalid.","validation":{"name":["The name may not be greater than 255 characters."]}}}`, "application/json"},
		"GET /api/pages/6":  ok(mdPage),
		"PUT /api/pages/6":  {503, `{"error":{"code":503,"message":"down Token tid:` + secret + `"}}`, "application/json"},
		"GET /api/pages/99": {404, `{"error":{"code":404,"message":"Page not found"}}`, "application/json"},
	})
	exec(t, "x", "pages", "create", "--book", "7", "--name", "N", "--file", "-").expect(t, 6, "",
		errJSON("validation", 422, "The given data was invalid. markdown: The markdown field is required when html is not present.; name: The name field is required."))
	exec(t, "", "pages", "update", "12", "--name", "N").expect(t, 6, "",
		errJSON("validation", 422, "The given data was invalid. name: The name may not be greater than 255 characters."))
	exec(t, "x", "pages", "update", "6", "--file", "-").expect(t, 1, "", errJSON("server", 503, "down Token tid:[REDACTED]"))
	exec(t, "x", "pages", "update", "99", "--append-file", "-").expect(t, 4, "", errJSON("not_found", 404, "Page not found"))
	f.expectRequests(t, "POST /api/pages", "PUT /api/pages/12", "GET /api/pages/6", "PUT /api/pages/6", "GET /api/pages/99")
}

// S1-S5: the help documents flags, format preservation, append, tags and
// exit codes; the pages group lists the write commands.
func TestHelpDocumentsWriteCommands(t *testing.T) {
	isolate(t)
	exitCodes := []string{"Exit codes:", "2  usage", "6  validation"}
	helps := map[string][]string{
		"pages create": {"--book", "--chapter", "--name", "--file", "--tag", "stdin", "Markdown editor", "10 MiB", "key=value"},
		"pages update": {"--name", "--file", "--append-file", "--tag", "stdin", "editor", "raw_html", "{{@3}}", "replaces", "unchanged", "10 MiB", "GFM"},
	}
	for name, wants := range helps {
		r := exec(t, "", append(strings.Fields(name), "--help")...)
		for _, want := range append(wants, exitCodes...) {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("%s help missing %q:\n%s", name, want, r.stdout)
			}
		}
	}
	r := exec(t, "", "pages")
	if !strings.Contains(r.stdout, "create") || !strings.Contains(r.stdout, "update") {
		t.Errorf("pages group help:\n%s", r.stdout)
	}
}

// W2 (T6 review): appending to a WYSIWYG page whose raw_html is missing while
// html has content would replace the whole page with only the new part. The
// command refuses, makes no write, and points at --file.
func TestPagesUpdateAppendRefusesWithoutRawHTML(t *testing.T) {
	page := strings.Replace(wysPage, `"raw_html":`, `"ignored_raw_html":`, 1)
	if page == wysPage {
		t.Fatal("fixture wysPage has no raw_html field")
	}
	f := newFake(t, map[string]reply{"GET /api/pages/5": ok(page)})
	exec(t, "new part\n", "pages", "update", "5", "--append-file", "-").expect(t, 1, "",
		errJSON("unsafe_write", 0, "page 5 has HTML content but BookStack returned no raw_html; refusing to append because it would replace the page. Use --file to replace the content explicitly"))
	f.expectRequests(t, "GET /api/pages/5")
}

// guardedPage is mdPage as BookStack returns it from GET /api/pages/<id>:
// with updated_at and updated_by as an object.
var guardedPage = strings.Replace(mdPage, `"updated_by":1,`, `"updated_at":"2026-10-20T08:30:15.000000Z","updated_by":{"id":3,"name":"Ana","slug":"ana"},`, 1)

// T7 S1, S2, S5: a value naming the same instant as the page's updated_at, in
// any RFC 3339 format, proceeds: one GET shared with the content logic, then
// the same PUT as without the guard.
func TestPagesUpdateGuardSameInstantProceeds(t *testing.T) {
	if guardedPage == mdPage {
		t.Fatal("fixture guardedPage was not built")
	}
	f := newFake(t, map[string]reply{"GET /api/pages/6": ok(guardedPage), "PUT /api/pages/6": ok(written)})
	file := tempFile(t, "new part\n")
	exec(t, "", "pages", "update", "6", "--name", "N", "--if-unmodified-since", "2026-10-20T08:30:15.000000Z").expect(t, 0, writtenMeta+"\n", "")
	exec(t, "", "pages", "update", "6", "--if-unmodified-since", "2026-10-20T08:30:15Z", "--tag", "k").expect(t, 0, writtenMeta+"\n", "")
	exec(t, "", "pages", "update", "6", "--append-file", file, "--if-unmodified-since", "2026-10-20T03:30:15.0-05:00").expect(t, 0, writtenMeta+"\n", "")
	exec(t, "", "pages", "update", "6", "--file", file, "--if-unmodified-since", "2026-10-20T08:30:15.000Z").expect(t, 0, writtenMeta+"\n", "")
	f.expectRequests(t,
		"GET /api/pages/6", "PUT /api/pages/6",
		"GET /api/pages/6", "PUT /api/pages/6",
		"GET /api/pages/6", "PUT /api/pages/6",
		"GET /api/pages/6", "PUT /api/pages/6")
	f.expectBodies(t,
		"", `{"name":"N"}`,
		"", `{"tags":[{"name":"k","value":""}]}`,
		"", `{"markdown":"old\n\nnew part\n"}`,
		"", `{"markdown":"new part\n"}`)
}

// T7 S1, S2, S5: a different instant aborts every kind of change after the
// GET with exit 5, no status, the current updated_at and updater, and no PUT.
func TestPagesUpdateGuardConflict(t *testing.T) {
	f := newFake(t, map[string]reply{"GET /api/pages/6": ok(guardedPage)})
	file := tempFile(t, "new part\n")
	cases := []struct {
		name, given string
		args        []string
	}{
		{"name", "2026-10-20T08:30:14Z", []string{"--name", "N"}},
		{"tag", "2026-10-20T08:30:14.000000Z", []string{"--tag", "k"}},
		{"file", "2026-10-20T08:30:16Z", []string{"--file", file}},
		{"append-file", "2026-10-19T08:30:15Z", []string{"--append-file", file}},
		{"fraction differs", "2026-10-20T08:30:15.000001Z", []string{"--name", "N"}},
		{"offset differs", "2026-10-20T08:30:15+01:00", []string{"--append-file", file, "--tag", "k"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"pages", "update", "6", "--if-unmodified-since", tc.given}, tc.args...)
			exec(t, "", args...).expect(t, 5, "",
				errJSON("conflict", 0, "page 6 was modified at 2026-10-20T08:30:15.000000Z by Ana; expected "+tc.given))
		})
	}
	want := make([]string, len(cases))
	for i := range want {
		want[i] = "GET /api/pages/6"
	}
	f.expectRequests(t, want...)
}

// T7 S1: without the updater's name (a bare id or null updated_by) the
// conflict message names the user id, or "an unknown user".
func TestPagesUpdateGuardConflictWithoutUpdaterName(t *testing.T) {
	byID := strings.Replace(guardedPage, `{"id":3,"name":"Ana","slug":"ana"}`, `3`, 1)
	byNull := strings.Replace(guardedPage, `{"id":3,"name":"Ana","slug":"ana"}`, `null`, 1)
	if byID == guardedPage || byNull == guardedPage {
		t.Fatal("fixtures were not built")
	}
	f := newFake(t, map[string]reply{"GET /api/pages/6": ok(byID), "GET /api/pages/7": ok(byNull)})
	exec(t, "", "pages", "update", "6", "--name", "N", "--if-unmodified-since", "2026-10-20T08:30:14Z").expect(t, 5, "",
		errJSON("conflict", 0, "page 6 was modified at 2026-10-20T08:30:15.000000Z by user 3; expected 2026-10-20T08:30:14Z"))
	exec(t, "", "pages", "update", "7", "--name", "N", "--if-unmodified-since", "2026-10-20T08:30:14Z").expect(t, 5, "",
		errJSON("conflict", 0, "page 7 was modified at 2026-10-20T08:30:15.000000Z by an unknown user; expected 2026-10-20T08:30:14Z"))
	f.expectRequests(t, "GET /api/pages/6", "GET /api/pages/7")
}

// T7 S1: a page whose updated_at is missing or unparsable cannot be checked;
// the update fails closed with no PUT.
func TestPagesUpdateGuardUncheckablePage(t *testing.T) {
	bad := strings.Replace(guardedPage, `"2026-10-20T08:30:15.000000Z"`, `"soon"`, 1)
	if bad == guardedPage {
		t.Fatal("fixture was not built")
	}
	f := newFake(t, map[string]reply{"GET /api/pages/6": ok(bad), "GET /api/pages/8": ok(emptyMD)})
	exec(t, "", "pages", "update", "6", "--name", "N", "--if-unmodified-since", "2026-10-20T08:30:15Z").expect(t, 1, "",
		errJSON("invalid_response", 0, `cannot check --if-unmodified-since: page 6 has an invalid updated_at \"soon\"; nothing was written`))
	exec(t, "", "pages", "update", "8", "--name", "N", "--if-unmodified-since", "2026-10-20T08:30:15Z").expect(t, 1, "",
		errJSON("invalid_response", 0, `cannot check --if-unmodified-since: page 8 has an invalid updated_at \"\"; nothing was written`))
	f.expectRequests(t, "GET /api/pages/6", "GET /api/pages/8")
}

// T7 S1, S5: an unparsable value is a usage error and no request is made,
// whatever the change.
func TestPagesUpdateGuardUnparsable(t *testing.T) {
	f := newFake(t, nil)
	file := tempFile(t, "x")
	for _, v := range []string{"", "yesterday", "2026-10-20", "2026-10-20 08:30:15Z", "2026-10-20T08:30:15", "1729413015"} {
		for _, change := range [][]string{{"--name", "N"}, {"--append-file", file}} {
			args := append([]string{"pages", "update", "6", "--if-unmodified-since", v}, change...)
			exec(t, "", args...).expect(t, 2, "",
				errJSON("usage", 0, `invalid --if-unmodified-since \"`+v+`\": must be an RFC 3339 timestamp such as the updated_at printed by \"pages get\"`))
		}
	}
	f.expectRequests(t)
}

// T7 S1, S3, S4: the update help documents the optional, non-atomic guard,
// the recommended pattern and exit 5.
func TestHelpDocumentsConcurrencyGuard(t *testing.T) {
	isolate(t)
	r := exec(t, "", "pages", "update", "--help")
	for _, want := range []string{
		"--if-unmodified-since", "optional", "not atomic", "revisions",
		"bsk pages get 5", "bsk pages update 5 --if-unmodified-since",
		"5  conflict",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("pages update help missing %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "--force") {
		t.Errorf("pages update help mentions --force:\n%s", r.stdout)
	}
	r = exec(t, "", "pages", "get", "--help")
	if !strings.Contains(r.stdout, "5  conflict") {
		t.Errorf("shared exit-code help missing exit 5:\n%s", r.stdout)
	}
}

// createdBookMeta and createdChapterMeta are create responses as printed, in
// the CLI field order; the created* replies add the description_html
// BookStack returns, which the output omits.
const (
	createdBookMeta    = `{"id":21,"name":"Ops","slug":"ops-gd3","description":"Run <b>it</b>","created_at":"2026-10-30T10:00:00.000000Z","updated_at":"2026-10-30T10:00:00.000000Z","created_by":1,"updated_by":1,"owned_by":1,"tags":[{"name":"env","value":"prod"}]}`
	createdChapterMeta = `{"id":31,"book_id":7,"book_slug":"ops","name":"Runbooks","slug":"runbooks","description":"Run <b>it</b>","priority":2,"created_at":"2026-10-30T10:00:00.000000Z","updated_at":"2026-10-30T10:00:00.000000Z","created_by":1,"updated_by":1,"owned_by":1,"tags":[{"name":"env","value":"prod"}]}`
)

var (
	createdBook    = strings.Replace(createdBookMeta, `,"created_at":`, `,"description_html":"<p>Run &lt;b&gt;it&lt;/b&gt;</p>","created_at":`, 1)
	createdChapter = strings.Replace(createdChapterMeta, `,"priority":`, `,"description_html":"<p>Run &lt;b&gt;it&lt;/b&gt;</p>","priority":`, 1)
)

// T8 S2-S4: books create sends name, plain description and tags only when
// given and prints the book without description_html.
func TestBooksCreate(t *testing.T) {
	if createdBook == createdBookMeta {
		t.Fatal("fixture createdBook was not built")
	}
	f := newFake(t, map[string]reply{"POST /api/books": ok(createdBook)})
	exec(t, "", "books", "create", "--name", "Ops", "--description", "Run <b>it</b>", "--tag", "env=prod", "--tag", "draft", "--tag", "k=a=b").
		expect(t, 0, createdBookMeta+"\n", "")
	exec(t, "", "books", "create", "--name", "Ops").expect(t, 0, createdBookMeta+"\n", "")
	f.expectRequests(t, "POST /api/books", "POST /api/books")
	f.expectBodies(t,
		`{"name":"Ops","description":"Run <b>it</b>","tags":[{"name":"env","value":"prod"},{"name":"draft","value":""},{"name":"k","value":"a=b"}]}`,
		`{"name":"Ops"}`)
}

// T8 S1, S3, S4: chapters create sends the book, name, plain description and
// tags only when given and prints the chapter without description_html.
func TestChaptersCreate(t *testing.T) {
	if createdChapter == createdChapterMeta {
		t.Fatal("fixture createdChapter was not built")
	}
	f := newFake(t, map[string]reply{"POST /api/chapters": ok(createdChapter)})
	exec(t, "", "chapters", "create", "--book", "7", "--name", "Runbooks", "--description", "Run <b>it</b>", "--tag", "env=prod", "--tag", "draft").
		expect(t, 0, createdChapterMeta+"\n", "")
	exec(t, "", "chapters", "create", "--name", "Runbooks", "--book", "7").expect(t, 0, createdChapterMeta+"\n", "")
	f.expectRequests(t, "POST /api/chapters", "POST /api/chapters")
	f.expectBodies(t,
		`{"book_id":7,"name":"Runbooks","description":"Run <b>it</b>","tags":[{"name":"env","value":"prod"},{"name":"draft","value":""}]}`,
		`{"book_id":7,"name":"Runbooks"}`)
}

// T8 S3: invalid input is a usage error and no request is made.
func TestStructureCreateUsageErrors(t *testing.T) {
	f := newFake(t, nil)
	cases := []struct {
		name    string
		args    []string
		message string
	}{
		{"book no name", []string{"books", "create"}, `--name is required`},
		{"book no name with description", []string{"books", "create", "--description", "d"}, `--name is required`},
		{"book blank name", []string{"books", "create", "--name", " \t"}, `invalid --name: must not be blank`},
		{"book empty name", []string{"books", "create", "--name", ""}, `invalid --name: must not be blank`},
		{"book args", []string{"books", "create", "--name", "N", "extra"}, `unknown command \"extra\" for \"bsk books create\"`},
		{"book empty tag key", []string{"books", "create", "--name", "N", "--tag", "=v"}, `invalid argument \"=v\" for \"--tag\" flag: key must not be empty`},
		{"chapter no book", []string{"chapters", "create", "--name", "N"}, `--book is required`},
		{"chapter bad book", []string{"chapters", "create", "--book", "x", "--name", "N"}, `invalid argument \"x\" for \"--book\" flag: must be a positive integer`},
		{"chapter zero book", []string{"chapters", "create", "--book", "0", "--name", "N"}, `invalid argument \"0\" for \"--book\" flag: must be a positive integer`},
		{"chapter negative book", []string{"chapters", "create", "--book", "-3", "--name", "N"}, `invalid argument \"-3\" for \"--book\" flag: must be a positive integer`},
		{"chapter no name", []string{"chapters", "create", "--book", "7"}, `--name is required`},
		{"chapter blank name", []string{"chapters", "create", "--book", "7", "--name", "  "}, `invalid --name: must not be blank`},
		{"chapter args", []string{"chapters", "create", "7"}, `unknown command \"7\" for \"bsk chapters create\"`},
		{"chapter blank tag key", []string{"chapters", "create", "--book", "7", "--name", "N", "--tag", " "}, `invalid argument \" \" for \"--tag\" flag: key must not be empty`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec(t, "", tc.args...).expect(t, 2, "", errJSON("usage", 0, tc.message))
		})
	}
	f.expectRequests(t)
}

// T8 S3, S5: a missing book is not_found (exit 4) from the API; a 422 is a
// validation error (exit 6); a 5xx create is not retried.
func TestStructureCreateAPIErrors(t *testing.T) {
	f := newFake(t, map[string]reply{
		"POST /api/chapters": {404, `{"error":{"code":404,"message":"Book not found"}}`, "application/json"},
		"POST /api/books":    {422, `{"error":{"code":422,"message":"The given data was invalid.","validation":{"name":["The name may not be greater than 255 characters."]}}}`, "application/json"},
	})
	exec(t, "", "chapters", "create", "--book", "999", "--name", "N").expect(t, 4, "", errJSON("not_found", 404, "Book not found"))
	exec(t, "", "books", "create", "--name", "N").expect(t, 6, "",
		errJSON("validation", 422, "The given data was invalid. name: The name may not be greater than 255 characters."))
	f.expectRequests(t, "POST /api/chapters", "POST /api/books")

	f = newFake(t, map[string]reply{
		"POST /api/chapters": {422, `{"error":{"code":422,"message":"The given data was invalid.","validation":{"name":["The name field is required."]}}}`, "application/json"},
		"POST /api/books":    {503, `{"error":{"code":503,"message":"down"}}`, "application/json"},
	})
	exec(t, "", "chapters", "create", "--book", "7", "--name", "N").expect(t, 6, "",
		errJSON("validation", 422, "The given data was invalid. name: The name field is required."))
	exec(t, "", "books", "create", "--name", "N").expect(t, 1, "", errJSON("server", 503, "down"))
	f.expectRequests(t, "POST /api/chapters", "POST /api/books")
}

// T8 S3, S4: the create help documents the flags, plain descriptions,
// duplicates, looking up first and the exit codes; the groups list create.
func TestHelpDocumentsStructureCreate(t *testing.T) {
	isolate(t)
	common := []string{"--name", "--description", "--tag", "key=value", "plain text", "description_html",
		"duplicate", "twice", "suffix", "Exit codes:", "2  usage", "4  not found", "6  validation"}
	helps := map[string][]string{
		"books create":    {"bsk books list"},
		"chapters create": {"--book", "bsk chapters list --book"},
	}
	for name, wants := range helps {
		r := exec(t, "", append(strings.Fields(name), "--help")...)
		for _, want := range append(wants, common...) {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("%s help missing %q:\n%s", name, want, r.stdout)
			}
		}
	}
	for _, group := range []string{"books", "chapters"} {
		r := exec(t, "", group)
		if r.code != 0 || !strings.Contains(r.stdout, "create") {
			t.Errorf("%s group help: code %d\n%s", group, r.code, r.stdout)
		}
	}
}
