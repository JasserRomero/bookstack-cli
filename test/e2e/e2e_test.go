//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// bsk test reaches the sandbox with the token from the environment.
func TestConnection(t *testing.T) {
	setup(t)
	var got struct {
		OK      bool    `json:"ok"`
		Profile *string `json:"profile"`
		URL     string  `json:"url"`
		TokenID string  `json:"token_id"`
		Version string  `json:"version"`
	}
	bsk(t, "", "test").ok(t, &got)
	if !got.OK || got.Profile != nil || got.URL != baseURL || got.TokenID != tokenID || got.Version != "v26.09.2" {
		t.Errorf("test = %+v", got)
	}
}

// A book, a chapter in it and a page in the chapter from stdin; the page
// reads back without a duplicated title, and writing that Markdown back is
// stable.
func TestCreateTreeAndRoundTrip(t *testing.T) {
	setup(t)
	book := newBook(t)
	var chapter entity
	bsk(t, "", "chapters", "create", "--book", strconv.Itoa(book.ID), "--name", "Runbooks").ok(t, &chapter)
	if chapter.BookID != book.ID {
		t.Fatalf("chapter book_id = %d, want %d", chapter.BookID, book.ID)
	}
	content := "Intro paragraph.\n\n## Steps\n\n- one\n- two\n"
	var page entity
	bsk(t, content, "pages", "create", "--chapter", strconv.Itoa(chapter.ID), "--name", "Deploy", "--file", "-").ok(t, &page)
	if page.BookID != book.ID || page.ChapterID != chapter.ID || page.Editor != "markdown" {
		t.Fatalf("page = %+v, want book %d chapter %d editor markdown", page, book.ID, chapter.ID)
	}
	// BookStack trims the trailing newline.
	if got := getPage(t, page.ID).Markdown; got != strings.TrimSuffix(content, "\n") {
		t.Errorf("stored markdown = %q, want %q", got, content)
	}

	var contents struct {
		Contents []struct {
			Type  string `json:"type"`
			ID    int    `json:"id"`
			Pages []struct {
				ID int `json:"id"`
			} `json:"pages"`
		} `json:"contents"`
	}
	bsk(t, "", "books", "get", strconv.Itoa(book.ID)).ok(t, &contents)
	c := contents.Contents
	if len(c) != 1 || c[0].Type != "chapter" || c[0].ID != chapter.ID || len(c[0].Pages) != 1 || c[0].Pages[0].ID != page.ID {
		t.Errorf("book contents = %+v", c)
	}

	id := strconv.Itoa(page.ID)
	first := bsk(t, "", "pages", "get", id, "--raw")
	first.ok(t, nil)
	if strings.Contains(first.stdout, "# Deploy") || !strings.Contains(first.stdout, "## Steps") {
		t.Fatalf("pages get --raw = %q: want the content without the title heading", first.stdout)
	}
	bsk(t, first.stdout, "pages", "update", id, "--file", "-").ok(t, nil)
	second := bsk(t, "", "pages", "get", id, "--raw")
	second.ok(t, nil)
	if second.stdout != first.stdout {
		t.Errorf("round trip changed the page:\nfirst  %q\nsecond %q", first.stdout, second.stdout)
	}
}

// --file on a WYSIWYG page converts the Markdown and keeps the editor.
func TestUpdateWYSIWYGKeepsEditor(t *testing.T) {
	setup(t)
	book := newBook(t)
	page := newWYSIWYGPage(t, book.ID, "<p>Old</p>")
	var out entity
	bsk(t, "## New\n\n**bold** text\n", "pages", "update", strconv.Itoa(page.ID), "--file", "-").ok(t, &out)
	got := getPage(t, page.ID)
	if out.Editor != "wysiwyg" || got.Editor != "wysiwyg" {
		t.Errorf("editor = %q (output %q), want wysiwyg", got.Editor, out.Editor)
	}
	if !strings.Contains(got.RawHTML, "<h2") || !strings.Contains(got.RawHTML, "<strong>bold</strong>") || strings.Contains(got.RawHTML, "Old") {
		t.Errorf("raw_html = %q, want the converted Markdown only", got.RawHTML)
	}
}

// --append-file on a WYSIWYG page keeps an include tag unexpanded.
func TestAppendKeepsInclude(t *testing.T) {
	setup(t)
	book := newBook(t)
	var source entity
	bsk(t, "Included text.\n", "pages", "create", "--book", strconv.Itoa(book.ID), "--name", "Source", "--file", "-").ok(t, &source)
	include := fmt.Sprintf("{{@%d}}", source.ID)
	page := newWYSIWYGPage(t, book.ID, "<p>Intro</p><p>"+include+"</p>")
	bsk(t, "## Appended\n\nmore\n", "pages", "update", strconv.Itoa(page.ID), "--append-file", "-").ok(t, nil)
	got := getPage(t, page.ID)
	if got.Editor != "wysiwyg" {
		t.Errorf("editor = %q, want wysiwyg", got.Editor)
	}
	for _, want := range []string{"Intro", include, "Appended", "more"} {
		if !strings.Contains(got.RawHTML, want) {
			t.Errorf("raw_html = %q, missing %q", got.RawHTML, want)
		}
	}
	if !strings.Contains(got.HTML, "Included text.") {
		t.Errorf("html = %q, want the include rendered", got.HTML)
	}
}

// An edit made through the API after "pages get" makes a guarded update fail
// with exit 5 and leaves the page as that edit left it.
func TestUpdateConflict(t *testing.T) {
	setup(t)
	book := newBook(t)
	var page entity
	bsk(t, "Original.\n", "pages", "create", "--book", strconv.Itoa(book.ID), "--name", "Guarded", "--file", "-").ok(t, &page)
	id := strconv.Itoa(page.ID)
	var read entity
	bsk(t, "", "pages", "get", id).ok(t, &read)
	// updated_at has a resolution of one second.
	time.Sleep(1100 * time.Millisecond)
	api(t, http.MethodPut, "/api/pages/"+id, map[string]any{"markdown": "Human edit.\n"}, nil)
	edited := getPage(t, page.ID)
	if edited.UpdatedAt == read.UpdatedAt {
		t.Fatalf("the API edit did not change updated_at (%s)", read.UpdatedAt)
	}

	msg := bsk(t, "Agent edit.\n", "pages", "update", id, "--if-unmodified-since", read.UpdatedAt, "--file", "-").fails(t, 5, "conflict", 0)
	if !strings.Contains(msg, edited.UpdatedAt) {
		t.Errorf("message %q does not name the current updated_at %s", msg, edited.UpdatedAt)
	}
	after := getPage(t, page.ID)
	if after.Markdown != "Human edit." || after.UpdatedAt != edited.UpdatedAt {
		t.Errorf("page changed by a rejected update: markdown %q, updated_at %s", after.Markdown, after.UpdatedAt)
	}

	// With the current updated_at the same update goes through.
	bsk(t, "Agent edit.\n", "pages", "update", id, "--if-unmodified-since", edited.UpdatedAt, "--file", "-").ok(t, nil)
	if got := getPage(t, page.ID).Markdown; got != "Agent edit." {
		t.Errorf("markdown = %q after the guarded update", got)
	}
}

// resolve-url prints the same as "pages get" for the page's web URL.
func TestResolveURL(t *testing.T) {
	setup(t)
	book := newBook(t)
	var page entity
	bsk(t, "Resolvable.\n", "pages", "create", "--book", strconv.Itoa(book.ID), "--name", "Target", "--file", "-").ok(t, &page)
	want := bsk(t, "", "pages", "get", strconv.Itoa(page.ID))
	want.ok(t, nil)
	link := baseURL + "/books/" + book.Slug + "/page/" + page.Slug + "#bkmrk-x"
	got := bsk(t, "", "pages", "resolve-url", link)
	got.ok(t, nil)
	if got.stdout != want.stdout {
		t.Errorf("resolve-url = %q\nwant pages get %q", got.stdout, want.stdout)
	}
}

// template apply of examples/templates/proyecto creates the expected tree.
func TestTemplateApply(t *testing.T) {
	setup(t)
	proyecto := strings.ReplaceAll(unique(t, "p"), " ", "-")
	var got struct {
		Template string `json:"template"`
		Complete bool   `json:"complete"`
		Created  []struct {
			Type string `json:"type"`
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"created"`
	}
	r := bsk(t, "", "template", "apply", "proyecto", "--var", "proyecto="+proyecto,
		"--templates-dir", filepath.Join("..", "..", "examples", "templates"))
	if len(r.stdout) > 0 {
		if err := json.Unmarshal([]byte(r.stdout), &got); err == nil && len(got.Created) > 0 && got.Created[0].Type == "book" {
			deleteBookAtEnd(t, got.Created[0].ID)
		}
	}
	r.ok(t, nil)
	want := []string{
		"book Proyecto " + proyecto,
		"page Ficha del documento",
		"chapter Historial y control de cambios",
		"page Changelog",
		"page Registro de incidencias y soluciones",
		"page Lista de mejoras pendientes y backlog",
	}
	var names []string
	for _, c := range got.Created {
		names = append(names, c.Type+" "+c.Name)
	}
	if got.Template != "proyecto" || !got.Complete || strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("apply = %+v\nwant created %q", got, want)
	}

	var book struct {
		Contents []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Pages []struct {
				Name string `json:"name"`
			} `json:"pages"`
		} `json:"contents"`
	}
	bsk(t, "", "books", "get", strconv.Itoa(got.Created[0].ID)).ok(t, &book)
	var tree []string
	for _, c := range book.Contents {
		tree = append(tree, c.Type+" "+c.Name)
		for _, p := range c.Pages {
			tree = append(tree, "page "+p.Name)
		}
	}
	if strings.Join(tree, "\n") != strings.Join(want[1:], "\n") {
		t.Errorf("book tree = %q, want %q", tree, want[1:])
	}
}

// search finds a created page; indexing may lag, so it retries for 10 s.
func TestSearch(t *testing.T) {
	setup(t)
	book := newBook(t)
	word := "bske2e" + strings.NewReplacer("T", "", ".", "").Replace(runID)
	var page entity
	bsk(t, "Searchable "+word+" content.\n", "pages", "create", "--book", strconv.Itoa(book.ID), "--name", "Findable", "--file", "-").ok(t, &page)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var got struct {
			Total int `json:"total"`
			Data  []struct {
				ID   int    `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
		}
		bsk(t, "", "search", word, "--type", "page").ok(t, &got)
		for _, d := range got.Data {
			if d.ID == page.ID && d.Type == "page" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("search %q did not find page %d within 10 s: %+v", word, page.ID, got)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Usage errors exit 2 without a request; a missing entity exits 4.
func TestErrorExits(t *testing.T) {
	setup(t)
	bsk(t, "", "pages", "get", "abc").fails(t, 2, "usage", 0)
	bsk(t, "", "pages", "update", "1").fails(t, 2, "usage", 0)
	bsk(t, "", "pages", "get", "999999999").fails(t, 4, "not_found", 404)
	bsk(t, "", "books", "get", "999999999").fails(t, 4, "not_found", 404)
}

// The secret stays out of every output, also when it comes from a profile
// written by "bsk auth". (bsk itself checks every run of the suite.)
func TestSecretNeverPrinted(t *testing.T) {
	setup(t)
	env := isolatedEnv(t)
	run := func(stdin string, args ...string) result {
		t.Helper()
		// Same environment for every call: one config directory, no
		// connection variables, so only the profile is used.
		var kept []string
		for _, kv := range env {
			if !strings.HasPrefix(kv, "BOOKSTACK_") {
				kept = append(kept, kv)
			}
		}
		return bskEnv(t, kept, stdin, args...)
	}
	run(secret+"\n", "auth", "--profile", "sandbox", "--url", baseURL, "--token-id", tokenID).ok(t, nil)
	for _, c := range []struct {
		args []string
		exit int
	}{
		{[]string{"config", "show"}, 0},
		{[]string{"config", "profiles"}, 0},
		{[]string{"--profile", "sandbox", "config", "show"}, 0},
		{[]string{"--profile", "sandbox", "test"}, 0},
		{[]string{"--profile", "sandbox", "pages", "get", "999999999"}, 4},
		{[]string{"--profile", "sandbox", "--help"}, 0},
	} {
		r := run("", c.args...)
		if r.code != c.exit {
			t.Errorf("bsk %v: exit %d, want %d (stderr %q)", c.args, r.code, c.exit, r.stderr)
		}
		if r.stdout+r.stderr == "" {
			t.Errorf("bsk %v printed nothing", c.args)
		}
	}
}
