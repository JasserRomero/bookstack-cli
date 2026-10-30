package template

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFiles writes files (path relative to dir -> content) under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// newTemplate writes a template named name in a new source directory and
// returns its entry.
func newTemplate(t *testing.T, name string, files map[string]string) Entry {
	t.Helper()
	src := t.TempDir()
	writeFiles(t, filepath.Join(src, name), files)
	return Entry{Name: name, Source: SourceFlag, Dir: filepath.Join(src, name)}
}

const fullManifest = `description = "Project book"
vars = ["proyecto", "owner"]

[book]
name = "Proyecto {{.proyecto}}"
description = "Docs of {{.proyecto}}"
tags = { area = "desarrollo", proyecto = "{{.proyecto}}", draft = "" }

[[book.pages]]
name = "Ficha del documento"
file = "ficha.md"
tags = { tipo = "ficha" }

[[book.chapters]]
name = "Historial y control de cambios"
description = "Changes of {{.proyecto}}"
tags = { tipo = "historial" }

[[book.chapters.pages]]
name = "Changelog {{.proyecto}}"
file = "docs/changelog.md"

[[book.chapters]]
name = "Empty chapter"
`

func fullFiles() map[string]string {
	return map[string]string{
		"template.toml":     fullManifest,
		"ficha.md":          "# Ficha {{.proyecto}}\n\nOwner: {{.owner}}\n",
		"docs/changelog.md": "# Changelog\n",
	}
}

// S2: a valid manifest is parsed with every field, missing lists as empty
// slices, and each page file resolved to its path in the template directory.
func TestLoadValid(t *testing.T) {
	e := newTemplate(t, "proyecto", fullFiles())
	tpl, err := Load(e)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Manifest{
		Description: "Project book",
		Vars:        []string{"proyecto", "owner"},
		Book: &Book{
			Name:        "Proyecto {{.proyecto}}",
			Description: "Docs of {{.proyecto}}",
			Tags:        map[string]string{"area": "desarrollo", "proyecto": "{{.proyecto}}", "draft": ""},
			Pages: []Page{{
				Name: "Ficha del documento", File: "ficha.md", Tags: map[string]string{"tipo": "ficha"},
				Path: filepath.Join(e.Dir, "ficha.md"),
			}},
			Chapters: []Chapter{
				{
					Name: "Historial y control de cambios", Description: "Changes of {{.proyecto}}",
					Tags:  map[string]string{"tipo": "historial"},
					Pages: []Page{{Name: "Changelog {{.proyecto}}", File: "docs/changelog.md", Path: filepath.Join(e.Dir, "docs", "changelog.md")}},
				},
				{Name: "Empty chapter", Pages: []Page{}},
			},
		},
	}
	got := tpl.Manifest
	// Unexported content is checked through Render.
	for i := range got.Book.Pages {
		got.Book.Pages[i].content = ""
	}
	for i := range got.Book.Chapters {
		for j := range got.Book.Chapters[i].Pages {
			got.Book.Chapters[i].Pages[j].content = ""
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("manifest = %+v\nwant       %+v", got, want)
	}
	if tpl.Name != "proyecto" || tpl.Source != SourceFlag || tpl.Dir != e.Dir {
		t.Errorf("template = %q %q %q", tpl.Name, tpl.Source, tpl.Dir)
	}
}

// S2: an empty [book] is valid and its lists are empty, not nil.
func TestLoadMinimal(t *testing.T) {
	tpl, err := Load(newTemplate(t, "min", map[string]string{"template.toml": "[book]\n"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Manifest{Vars: []string{}, Book: &Book{Pages: []Page{}, Chapters: []Chapter{}}}
	if !reflect.DeepEqual(tpl.Manifest, want) {
		t.Errorf("manifest = %+v, want %+v", tpl.Manifest, want)
	}
}

// S2: every validation failure is an *InvalidError naming the file and the
// problem.
func TestLoadInvalid(t *testing.T) {
	page := func(extra string) string {
		return "[book]\nname = \"B\"\n\n[[book.pages]]\nname = \"P\"\n" + extra
	}
	cases := []struct {
		name     string
		files    map[string]string
		file     string // relative to the template directory
		problem  string
		setup    func(t *testing.T, dir string)
		skipRoot bool
	}{
		{name: "invalid toml", files: map[string]string{"template.toml": "[book\n"},
			file: "template.toml", problem: "invalid TOML: toml: line 2: expected '.' or ']' to end table name, but got '\\n' instead"},
		{name: "missing manifest", files: map[string]string{"other.md": "x"},
			file: "template.toml", problem: "cannot read: no such file or directory"},
		{name: "unknown keys", files: map[string]string{"template.toml": "titel = \"x\"\n[book]\nnmae = \"B\"\n[[book.pages]]\nname = \"P\"\nfile = \"p.md\"\nfiel = \"q\"\n", "p.md": "x"},
			file: "template.toml", problem: "unknown keys: titel, book.nmae, book.pages.fiel"},
		{name: "wrong type", files: map[string]string{"template.toml": "vars = \"proyecto\"\n[book]\n"},
			file: "template.toml", problem: "invalid TOML: toml: line 1 (last key \"vars\"): incompatible types: TOML value has type string; destination has type slice"},
		{name: "missing book", files: map[string]string{"template.toml": "description = \"x\"\n"},
			file: "template.toml", problem: "missing [book] table"},
		{name: "blank book name", files: map[string]string{"template.toml": "[book]\nname = \"  \"\n"},
			file: "template.toml", problem: "book.name: must not be blank"},
		{name: "blank page name", files: map[string]string{"template.toml": "[book]\n[[book.pages]]\nname = \" \"\nfile = \"p.md\"\n", "p.md": "x"},
			file: "template.toml", problem: "book.pages[1].name: must not be blank"},
		{name: "page without name", files: map[string]string{"template.toml": "[book]\n[[book.pages]]\nfile = \"p.md\"\n", "p.md": "x"},
			file: "template.toml", problem: "book.pages[1].name: must not be blank"},
		{name: "page without file", files: map[string]string{"template.toml": page("")},
			file: "template.toml", problem: "book.pages[1].file: is required"},
		{name: "chapter page without file", files: map[string]string{"template.toml": "[book]\n[[book.chapters]]\nname = \"C\"\n[[book.chapters.pages]]\nname = \"P\"\n"},
			file: "template.toml", problem: "book.chapters[1].pages[1].file: is required"},
		{name: "blank chapter name", files: map[string]string{"template.toml": "[book]\n[[book.chapters]]\nname = \"\"\n"},
			file: "template.toml", problem: "book.chapters[1].name: must not be blank"},
		{name: "absolute path", files: map[string]string{"template.toml": page("file = \"/etc/passwd\"\n")},
			file: "template.toml", problem: "book.pages[1].file: \"/etc/passwd\" must be a relative path"},
		{name: "dotdot escape", files: map[string]string{"template.toml": page("file = \"sub/../../x.md\"\n")},
			file: "template.toml", problem: "book.pages[1].file: \"sub/../../x.md\" is outside the template directory"},
		{name: "symlink escape", files: map[string]string{"template.toml": page("file = \"link.md\"\n")},
			file: "link.md", problem: "resolves outside the template directory",
			setup: func(t *testing.T, dir string) {
				outside := filepath.Join(t.TempDir(), "secret.md")
				writeFiles(t, filepath.Dir(outside), map[string]string{"secret.md": "x"})
				if err := os.Symlink(outside, filepath.Join(dir, "link.md")); err != nil {
					t.Skipf("symlink: %v", err)
				}
			}},
		{name: "missing file", files: map[string]string{"template.toml": page("file = \"nope.md\"\n")},
			file: "nope.md", problem: "cannot read: no such file or directory"},
		{name: "unreadable", files: map[string]string{"template.toml": page("file = \"p.md\"\n"), "p.md": "x"},
			file: "p.md", problem: "cannot read: permission denied", skipRoot: true,
			setup: func(t *testing.T, dir string) {
				if err := os.Chmod(filepath.Join(dir, "p.md"), 0); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "directory", files: map[string]string{"template.toml": page("file = \"sub\"\n"), "sub/x.md": "x"},
			file: "sub", problem: "cannot read: is a directory"},
		{name: "too large", files: map[string]string{"template.toml": page("file = \"p.md\"\n"), "p.md": strings.Repeat("a", MaxFileBytes+1)},
			file: "p.md", problem: "larger than 10 MiB"},
		{name: "invalid utf8", files: map[string]string{"template.toml": page("file = \"p.md\"\n"), "p.md": "a\xffb"},
			file: "p.md", problem: "not valid UTF-8"},
		{name: "blank file", files: map[string]string{"template.toml": page("file = \"p.md\"\n"), "p.md": " \n\t\n"},
			file: "p.md", problem: "empty or only whitespace"},
		{name: "file syntax", files: map[string]string{"template.toml": page("file = \"p.md\"\n"), "p.md": "{{.x"},
			file: "p.md", problem: "template: p.md:1: unclosed action"},
		{name: "name syntax", files: map[string]string{"template.toml": "[book]\nname = \"{{if}}\"\n"},
			file: "template.toml", problem: "template: book.name:1: missing value for if"},
		{name: "tag value syntax", files: map[string]string{"template.toml": "[book]\ntags = { a = \"{{\" }\n"},
			file: "template.toml", problem: "template: book.tags.a:1: unclosed action"},
		{name: "blank tag key", files: map[string]string{"template.toml": "[book]\n[[book.chapters]]\nname = \"C\"\ntags = { \" \" = \"v\" }\n"},
			file: "template.toml", problem: "book.chapters[1].tags: key must not be blank"},
		{name: "invalid var name", files: map[string]string{"template.toml": "vars = [\"mi-var\"]\n[book]\n"},
			file: "template.toml", problem: "vars: \"mi-var\" must be a letter or underscore followed by letters, digits or underscores"},
		{name: "duplicate var", files: map[string]string{"template.toml": "vars = [\"a\", \"a\"]\n[book]\n"},
			file: "template.toml", problem: "vars: \"a\" is declared twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.skipRoot && os.Geteuid() == 0 {
				t.Skip("root reads any file")
			}
			e := newTemplate(t, "tpl", c.files)
			if c.setup != nil {
				c.setup(t, e.Dir)
			}
			tpl, err := Load(e)
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("Load = %v, %v; want *InvalidError", tpl, err)
			}
			want := InvalidError{Template: "tpl", File: filepath.Join(e.Dir, c.file), Problem: c.problem}
			if *inv != want {
				t.Errorf("error = %+v\nwant    %+v", *inv, want)
			}
			if got, wantMsg := err.Error(), `invalid template "tpl": `+want.File+": "+c.problem; got != wantMsg {
				t.Errorf("message = %q, want %q", got, wantMsg)
			}
		})
	}
}

// S2: a symlink that stays inside the template directory is accepted.
func TestLoadSymlinkInside(t *testing.T) {
	e := newTemplate(t, "tpl", map[string]string{
		"template.toml": "[book]\n[[book.pages]]\nname = \"P\"\nfile = \"link.md\"\n",
		"real/p.md":     "inside",
	})
	if err := os.Symlink(filepath.Join("real", "p.md"), filepath.Join(e.Dir, "link.md")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	tpl, err := Load(e)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := r.Pages[0].Markdown; got != "inside" {
		t.Errorf("markdown = %q", got)
	}
}

// S2: a file of exactly 10 MiB is accepted.
func TestLoadMaxSize(t *testing.T) {
	_, err := Load(newTemplate(t, "tpl", map[string]string{
		"template.toml": "[book]\n[[book.pages]]\nname = \"P\"\nfile = \"p.md\"\n",
		"p.md":          strings.Repeat("a", MaxFileBytes),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
}

// S2, S3: names, descriptions, tag values and files are rendered with the
// vars; tags are sorted by key.
func TestRender(t *testing.T) {
	tpl, err := Load(newTemplate(t, "proyecto", fullFiles()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, err := tpl.Render(map[string]string{"proyecto": "IRIS", "owner": "Ana"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := &Rendered{
		Name:        "Proyecto IRIS",
		Description: "Docs of IRIS",
		Tags:        []Tag{{"area", "desarrollo"}, {"draft", ""}, {"proyecto", "IRIS"}},
		Pages:       []RenderedPage{{Name: "Ficha del documento", Markdown: "# Ficha IRIS\n\nOwner: Ana\n", Tags: []Tag{{"tipo", "ficha"}}}},
		Chapters: []RenderedChapter{
			{
				Name: "Historial y control de cambios", Description: "Changes of IRIS", Tags: []Tag{{"tipo", "historial"}},
				Pages: []RenderedPage{{Name: "Changelog IRIS", Markdown: "# Changelog\n"}},
			},
			{Name: "Empty chapter"},
		},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("rendered = %+v\nwant       %+v", r, want)
	}
}

// S3: a missing var or an undeclared one is a *VarsError, checked before
// rendering.
func TestRenderVars(t *testing.T) {
	tpl, err := Load(newTemplate(t, "proyecto", fullFiles()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	none, err := Load(newTemplate(t, "plain", map[string]string{"template.toml": "[book]\n"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		tpl  *Template
		vars map[string]string
		want string
	}{
		{tpl, map[string]string{"proyecto": "IRIS"}, `missing --var owner: template "proyecto" declares proyecto, owner`},
		{tpl, nil, `missing --var proyecto, owner: template "proyecto" declares proyecto, owner`},
		{tpl, map[string]string{"proyecto": "IRIS", "owner": "A", "proyeto": "x", "b": "y"}, `unknown --var b, proyeto: template "proyecto" declares proyecto, owner`},
		{none, map[string]string{"x": "1"}, `unknown --var x: template "plain" declares no vars`},
	}
	for _, c := range cases {
		_, err := c.tpl.Render(c.vars)
		var ve *VarsError
		if !errors.As(err, &ve) {
			t.Errorf("Render(%v) = %v, want *VarsError", c.vars, err)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("Render(%v) = %q\nwant %q", c.vars, err.Error(), c.want)
		}
		if err := c.tpl.CheckVars(c.vars); err == nil || err.Error() != c.want {
			t.Errorf("CheckVars(%v) = %v, want %q", c.vars, err, c.want)
		}
	}
}

// S3, S2: rendering uses missingkey=error, and a page or name blank after
// rendering is invalid.
func TestRenderInvalid(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		vars    map[string]string
		file    string
		problem string
	}{
		{"undeclared in file", map[string]string{
			"template.toml": "vars = [\"a\"]\n[book]\n[[book.pages]]\nname = \"P\"\nfile = \"p.md\"\n",
			"p.md":          "{{.a}} {{.b}}",
		}, map[string]string{"a": "1"}, "p.md", `template: p.md:1:9: executing "p.md" at <.b>: map has no entry for key "b"`},
		{"undeclared in name", map[string]string{
			"template.toml": "[book]\nname = \"{{.x}}\"\n",
		}, nil, "template.toml", `template: book.name:1:2: executing "book.name" at <.x>: map has no entry for key "x"`},
		{"undeclared in tag value", map[string]string{
			"template.toml": "[book]\n[[book.chapters]]\nname = \"C\"\n[[book.chapters.pages]]\nname = \"P\"\nfile = \"p.md\"\ntags = { k = \"{{.y}}\" }\n",
			"p.md":          "x",
		}, nil, "template.toml", `template: book.chapters[1].pages[1].tags.k:1:2: executing "book.chapters[1].pages[1].tags.k" at <.y>: map has no entry for key "y"`},
		{"blank file after render", map[string]string{
			"template.toml": "vars = [\"a\"]\n[book]\n[[book.pages]]\nname = \"P\"\nfile = \"p.md\"\n",
			"p.md":          "{{.a}}\n",
		}, map[string]string{"a": " "}, "p.md", "empty or only whitespace after rendering"},
		{"blank name after render", map[string]string{
			"template.toml": "vars = [\"a\"]\n[book]\n[[book.chapters]]\nname = \"{{.a}}\"\n",
		}, map[string]string{"a": ""}, "template.toml", "book.chapters[1].name: blank after rendering"},
		{"blank book name after render", map[string]string{
			"template.toml": "vars = [\"a\"]\n[book]\nname = \"{{.a}}\"\n",
		}, map[string]string{"a": ""}, "template.toml", "book.name: blank after rendering"},
		{"too large after render", map[string]string{
			"template.toml": "vars = [\"a\"]\n[book]\n[[book.pages]]\nname = \"P\"\nfile = \"p.md\"\n",
			"p.md":          strings.Repeat("{{.a}}", 4),
		}, map[string]string{"a": strings.Repeat("x", MaxFileBytes/4+1)}, "p.md", "larger than 10 MiB after rendering"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTemplate(t, "tpl", c.files)
			tpl, err := Load(e)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			_, err = tpl.Render(c.vars)
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("Render = %v, want *InvalidError", err)
			}
			want := InvalidError{Template: "tpl", File: filepath.Join(e.Dir, c.file), Problem: c.problem}
			if *inv != want {
				t.Errorf("error = %+v\nwant    %+v", *inv, want)
			}
		})
	}
}

// S7: sources in precedence order; the profile source only with a profile.
func TestSources(t *testing.T) {
	got := Sources("/flag", "/cfg", "work")
	want := []Source{
		{SourceFlag, "/flag"},
		{SourceProfile, filepath.Join("/cfg", "profiles", "work", "templates")},
		{SourceShared, filepath.Join("/cfg", "templates")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sources = %v, want %v", got, want)
	}
	got = Sources("", "/cfg", "")
	want = []Source{{SourceShared, filepath.Join("/cfg", "templates")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sources = %v, want %v", got, want)
	}
}

// S7: Discover lists templates sorted by name; a higher source shadows the
// same name in lower ones; directories without template.toml and missing
// source directories are ignored.
func TestDiscoverAndFind(t *testing.T) {
	flag, cfg := t.TempDir(), t.TempDir()
	profile := filepath.Join(cfg, "profiles", "work", "templates")
	shared := filepath.Join(cfg, "templates")
	manifest := map[string]string{"template.toml": "[book]\n"}
	writeFiles(t, filepath.Join(flag, "b"), manifest)
	writeFiles(t, filepath.Join(profile, "b"), manifest)
	writeFiles(t, filepath.Join(profile, "c"), manifest)
	writeFiles(t, filepath.Join(shared, "c"), manifest)
	writeFiles(t, filepath.Join(shared, "a"), manifest)
	writeFiles(t, filepath.Join(shared, "notes"), map[string]string{"readme.md": "x"})
	writeFiles(t, shared, map[string]string{"loose.toml": "x"})

	got, err := Discover(Sources(flag, cfg, "work"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []Entry{
		{"a", SourceShared, filepath.Join(shared, "a")},
		{"b", SourceFlag, filepath.Join(flag, "b")},
		{"c", SourceProfile, filepath.Join(profile, "c")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover = %v\nwant       %v", got, want)
	}

	// Without a profile, its templates are not used.
	got, err = Discover(Sources("", cfg, ""))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want = []Entry{{"a", SourceShared, filepath.Join(shared, "a")}, {"c", SourceShared, filepath.Join(shared, "c")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover = %v\nwant       %v", got, want)
	}

	// Missing source directories are not an error.
	got, err = Discover(Sources("", filepath.Join(cfg, "nope"), "x"))
	if err != nil || len(got) != 0 {
		t.Errorf("Discover = %v, %v; want empty", got, err)
	}

	e, err := Find(Sources(flag, cfg, "work"), "c")
	if err != nil || e != (Entry{"c", SourceProfile, filepath.Join(profile, "c")}) {
		t.Errorf("Find(c) = %v, %v", e, err)
	}
	for _, name := range []string{"notes", "zzz"} {
		_, err = Find(Sources(flag, cfg, "work"), name)
		var nf *NotFoundError
		if !errors.As(err, &nf) || err.Error() != `unknown template "`+name+`"; available: a, b, c` {
			t.Errorf("Find(%s) = %v", name, err)
		}
	}
	_, err = Find(Sources("", filepath.Join(cfg, "nope"), ""), "x")
	if err == nil || err.Error() != `unknown template "x"; available: none` {
		t.Errorf("Find(x) = %v", err)
	}
	for _, name := range []string{"", ".", "..", "a/b", "../a", `a\b`} {
		_, err = Find(Sources(flag, cfg, "work"), name)
		var nf *NotFoundError
		if errors.As(err, &nf) || err == nil || err.Error() != fmt.Sprintf("invalid template name %q: must be a directory name", name) {
			t.Errorf("Find(%q) = %v", name, err)
		}
	}
}
