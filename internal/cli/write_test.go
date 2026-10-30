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
