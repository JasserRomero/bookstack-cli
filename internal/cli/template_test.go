package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectTemplate is a template with book pages and chapters; pagesTemplate
// has only book pages and no book.name.
var (
	projectTemplate = map[string]string{
		"template.toml": `description = "Project book"
vars = ["proyecto"]

[book]
name = "Proyecto {{.proyecto}}"
description = "Docs {{.proyecto}}"
tags = { area = "dev", proyecto = "{{.proyecto}}" }

[[book.pages]]
name = "Ficha"
file = "ficha.md"
tags = { tipo = "ficha", draft = "" }

[[book.chapters]]
name = "Historial"
description = "Cambios"

[[book.chapters.pages]]
name = "Changelog"
file = "changelog.md"

[[book.chapters]]
name = "Backlog {{.proyecto}}"
`,
		"ficha.md":     "# Ficha {{.proyecto}}\n",
		"changelog.md": "# Changelog\n",
	}
	pagesTemplate = map[string]string{
		"template.toml": "description = \"Two pages\"\n\n[book]\n\n[[book.pages]]\nname = \"Uno\"\nfile = \"uno.md\"\n\n[[book.pages]]\nname = \"Dos\"\nfile = \"dos.md\"\n",
		"uno.md":        "uno\n",
		"dos.md":        "dos\n",
	}
	brokenTemplate = map[string]string{"template.toml": "description = \"x\"\n"}
)

// writeTemplate writes the files of template name under dir.
func writeTemplate(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, name, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, name)
}

// configDir is the directory of the config file of the isolated test.
func configDir() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "bookstack-cli")
}

// flagTemplates writes the project and pages templates to a new
// --templates-dir and returns it.
func flagTemplates(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTemplate(t, dir, "proyecto", projectTemplate)
	writeTemplate(t, dir, "pagina", pagesTemplate)
	return dir
}

func tplEntry(name, description, vars, source, path string) string {
	return `{"name":"` + name + `","description":"` + description + `","vars":` + vars + `,"source":"` + source + `","path":"` + path + `"}`
}

// S4, S7: list prints every template sorted by name with its source; a higher
// source shadows lower ones; an invalid template is listed with its error;
// no API access (no credentials, no requests).
func TestTemplateList(t *testing.T) {
	path := isolate(t)
	writeConfig(t, path, "default_profile = \"work\"\n\n[profiles.work]\nurl = \"https://work.invalid\"\n\n[profiles.home]\nurl = \"https://home.invalid\"\n")
	cfg := configDir()
	flag := t.TempDir()
	writeTemplate(t, flag, "proyecto", projectTemplate)
	broken := writeTemplate(t, flag, "roto", brokenTemplate)
	writeTemplate(t, filepath.Join(cfg, "templates"), "proyecto", pagesTemplate)
	shared := writeTemplate(t, filepath.Join(cfg, "templates"), "compartida", pagesTemplate)
	work := writeTemplate(t, filepath.Join(cfg, "profiles", "work", "templates"), "perfil", pagesTemplate)
	writeTemplate(t, filepath.Join(cfg, "profiles", "home", "templates"), "casa", pagesTemplate)

	exec(t, "", "template", "list", "--templates-dir", flag).expect(t, 0, "["+
		tplEntry("compartida", "Two pages", "[]", "shared", shared)+","+
		tplEntry("perfil", "Two pages", "[]", "profile", work)+","+
		tplEntry("proyecto", "Project book", `["proyecto"]`, "flag", filepath.Join(flag, "proyecto"))+","+
		strings.TrimSuffix(tplEntry("roto", "", "[]", "flag", broken), "}")+
		`,"error":"invalid template \"roto\": `+filepath.Join(broken, "template.toml")+`: missing [book] table"}`+
		"]\n", "")

	// Without --templates-dir, the shared proyecto is visible.
	exec(t, "", "template", "list").expect(t, 0, "["+
		tplEntry("compartida", "Two pages", "[]", "shared", shared)+","+
		tplEntry("perfil", "Two pages", "[]", "profile", work)+","+
		tplEntry("proyecto", "Two pages", "[]", "shared", filepath.Join(cfg, "templates", "proyecto"))+
		"]\n", "")
}

// S4: list with no template at all prints an empty array.
func TestTemplateListEmpty(t *testing.T) {
	isolate(t)
	exec(t, "", "template", "list").expect(t, 0, "[]\n", "")
}

// S7: the profile is the one T2 selects: --profile, then BOOKSTACK_PROFILE,
// then default_profile, then the only profile; without one, only
// --templates-dir and shared templates are used.
func TestTemplateProfilePrecedence(t *testing.T) {
	path := isolate(t)
	cfg := configDir()
	home := writeTemplate(t, filepath.Join(cfg, "profiles", "home", "templates"), "casa", pagesTemplate)
	work := writeTemplate(t, filepath.Join(cfg, "profiles", "work", "templates"), "trabajo", pagesTemplate)
	casa := "[" + tplEntry("casa", "Two pages", "[]", "profile", home) + "]\n"
	trabajo := "[" + tplEntry("trabajo", "Two pages", "[]", "profile", work) + "]\n"

	two := "[profiles.work]\nurl = \"https://work.invalid\"\n\n[profiles.home]\nurl = \"https://home.invalid\"\n"
	writeConfig(t, path, two)
	exec(t, "", "template", "list").expect(t, 0, "[]\n", "")
	exec(t, "", "--profile", "home", "template", "list").expect(t, 0, casa, "")
	t.Setenv("BOOKSTACK_PROFILE", "work")
	exec(t, "", "template", "list").expect(t, 0, trabajo, "")
	exec(t, "", "--profile", "home", "template", "list").expect(t, 0, casa, "")
	t.Setenv("BOOKSTACK_PROFILE", "")

	writeConfig(t, path, "default_profile = \"work\"\n"+two)
	exec(t, "", "template", "list").expect(t, 0, trabajo, "")

	writeConfig(t, path, "[profiles.home]\nurl = \"https://home.invalid\"\n")
	exec(t, "", "template", "list").expect(t, 0, casa, "")

	exec(t, "", "--profile", "nope", "template", "list").expect(t, 2, "",
		errJSON("usage", 0, `unknown profile \"nope\"; available profiles: home`))

	writeConfig(t, path, "[profiles.\"../x\"]\nurl = \"https://x.invalid\"\n")
	exec(t, "", "template", "list").expect(t, 2, "",
		errJSON("usage", 0, `profile name \"../x\" cannot be used as a templates directory`))
}

// S7: --templates-dir must be an existing directory.
func TestTemplatesDirInvalid(t *testing.T) {
	isolate(t)
	missing := filepath.Join(t.TempDir(), "nope")
	file := tempFile(t, "x")
	exec(t, "", "template", "list", "--templates-dir", missing).expect(t, 2, "",
		errJSON("usage", 0, `invalid --templates-dir \"`+missing+`\": no such file or directory`))
	exec(t, "", "template", "list", "--templates-dir", file).expect(t, 2, "",
		errJSON("usage", 0, `invalid --templates-dir \"`+file+`\": not a directory`))
	exec(t, "", "template", "list", "--templates-dir", "").expect(t, 2, "",
		errJSON("usage", 0, `invalid --templates-dir \"\": must not be empty`))
}

// S4: show prints the parsed manifest, unrendered, with the resolved paths.
func TestTemplateShow(t *testing.T) {
	isolate(t)
	flag := flagTemplates(t)
	dir := filepath.Join(flag, "proyecto")
	exec(t, "", "template", "show", "proyecto", "--templates-dir", flag).expect(t, 0,
		`{"name":"proyecto","description":"Project book","vars":["proyecto"],"source":"flag","path":"`+dir+`",`+
			`"book":{"name":"Proyecto {{.proyecto}}","description":"Docs {{.proyecto}}","tags":{"area":"dev","proyecto":"{{.proyecto}}"},`+
			`"pages":[{"name":"Ficha","file":"ficha.md","tags":{"draft":"","tipo":"ficha"},"path":"`+filepath.Join(dir, "ficha.md")+`"}],`+
			`"chapters":[{"name":"Historial","description":"Cambios","pages":[{"name":"Changelog","file":"changelog.md","path":"`+filepath.Join(dir, "changelog.md")+`"}]},`+
			`{"name":"Backlog {{.proyecto}}","pages":[]}]}}`+"\n", "")
	exec(t, "", "template", "show", "pagina", "--templates-dir", flag).expect(t, 0,
		`{"name":"pagina","description":"Two pages","vars":[],"source":"flag","path":"`+filepath.Join(flag, "pagina")+`",`+
			`"book":{"name":"","pages":[{"name":"Uno","file":"uno.md","path":"`+filepath.Join(flag, "pagina", "uno.md")+`"},`+
			`{"name":"Dos","file":"dos.md","path":"`+filepath.Join(flag, "pagina", "dos.md")+`"}],"chapters":[]}}`+"\n", "")
}

// S2, S4: show of an invalid, unknown or malformed template name fails.
func TestTemplateShowErrors(t *testing.T) {
	isolate(t)
	flag := flagTemplates(t)
	broken := writeTemplate(t, flag, "roto", brokenTemplate)
	exec(t, "", "template", "show", "roto", "--templates-dir", flag).expect(t, 1, "",
		errJSON("template_invalid", 0, `invalid template \"roto\": `+filepath.Join(broken, "template.toml")+`: missing [book] table`))
	exec(t, "", "template", "show", "nope", "--templates-dir", flag).expect(t, 2, "",
		errJSON("usage", 0, `unknown template \"nope\"; available: pagina, proyecto, roto`))
	exec(t, "", "template", "show", "../proyecto", "--templates-dir", flag).expect(t, 2, "",
		errJSON("usage", 0, `invalid template name \"../proyecto\": must be a directory name`))
	exec(t, "", "template", "show").expect(t, 2, "", errJSON("usage", 0, "accepts 1 arg(s), received 0"))
}

// Fixtures of created entities.
const (
	tplBook         = `{"id":20,"name":"Proyecto IRIS","slug":"proyecto-iris"}`
	createdFicha    = `{"id":30,"book_id":20,"name":"Ficha","slug":"ficha"}`
	createdHist     = `{"id":21,"book_id":20,"name":"Historial","slug":"historial"}`
	createdChlog    = `{"id":31,"book_id":20,"chapter_id":21,"name":"Changelog","slug":"changelog"}`
	createdBacklog  = `{"id":22,"book_id":20,"name":"Backlog IRIS","slug":"backlog-iris"}`
	serverError     = `{"error":{"code":500,"message":"Boom"}}`
	bodyBook        = `{"name":"Proyecto IRIS","description":"Docs IRIS","tags":[{"name":"area","value":"dev"},{"name":"proyecto","value":"IRIS"}]}`
	bodyFichaInBook = `,"name":"Ficha","markdown":"# Ficha IRIS\n","tags":[{"name":"draft","value":""},{"name":"tipo","value":"ficha"}]}`
	bodyChlog       = `{"chapter_id":21,"name":"Changelog","markdown":"# Changelog\n"}`
)

func projectFake(t *testing.T) *fakeAPI {
	t.Helper()
	f := newFake(t, map[string]reply{"POST /api/books": ok(tplBook)})
	f.seq = map[string][]reply{
		"POST /api/pages":    {ok(createdFicha), ok(createdChlog)},
		"POST /api/chapters": {ok(createdHist), ok(createdBacklog)},
	}
	return f
}

// S2, S3, S4, S6: apply without a target creates the book, its pages, then
// each chapter followed by its pages, rendered, in manifest order.
func TestTemplateApplyNewBook(t *testing.T) {
	f := projectFake(t)
	flag := flagTemplates(t)
	exec(t, "", "template", "apply", "proyecto", "--var", "proyecto=IRIS", "--templates-dir", flag).expect(t, 0,
		`{"template":"proyecto","complete":true,"created":[{"type":"book","id":20,"name":"Proyecto IRIS"},{"type":"page","id":30,"name":"Ficha"},`+
			`{"type":"chapter","id":21,"name":"Historial"},{"type":"page","id":31,"name":"Changelog"},{"type":"chapter","id":22,"name":"Backlog IRIS"}]}`+"\n", "")
	f.expectRequests(t, "POST /api/books", "POST /api/pages", "POST /api/chapters", "POST /api/pages", "POST /api/chapters")
	f.expectBodies(t,
		bodyBook,
		`{"book_id":20`+bodyFichaInBook,
		`{"book_id":20,"name":"Historial","description":"Cambios"}`,
		bodyChlog,
		`{"book_id":20,"name":"Backlog IRIS"}`)
}

// S4: --book uses the existing book and ignores [book] name, description
// and tags.
func TestTemplateApplyBook(t *testing.T) {
	f := projectFake(t)
	flag := flagTemplates(t)
	exec(t, "", "template", "apply", "proyecto", "--book", "7", "--var", "proyecto=IRIS", "--templates-dir", flag).expect(t, 0,
		`{"template":"proyecto","complete":true,"created":[{"type":"page","id":30,"name":"Ficha"},`+
			`{"type":"chapter","id":21,"name":"Historial"},{"type":"page","id":31,"name":"Changelog"},{"type":"chapter","id":22,"name":"Backlog IRIS"}]}`+"\n", "")
	f.expectRequests(t, "POST /api/pages", "POST /api/chapters", "POST /api/pages", "POST /api/chapters")
	f.expectBodies(t,
		`{"book_id":7`+bodyFichaInBook,
		`{"book_id":7,"name":"Historial","description":"Cambios"}`,
		bodyChlog,
		`{"book_id":7,"name":"Backlog IRIS"}`)
}

// S4: --chapter creates the book pages inside that chapter; a template
// without book.name works with --book or --chapter.
func TestTemplateApplyChapter(t *testing.T) {
	f := newFake(t, nil)
	f.seq = map[string][]reply{"POST /api/pages": {ok(`{"id":40,"name":"Uno"}`), ok(`{"id":41,"name":"Dos"}`)}}
	flag := flagTemplates(t)
	exec(t, "", "template", "apply", "pagina", "--chapter", "9", "--templates-dir", flag).expect(t, 0,
		`{"template":"pagina","complete":true,"created":[{"type":"page","id":40,"name":"Uno"},{"type":"page","id":41,"name":"Dos"}]}`+"\n", "")
	f.expectRequests(t, "POST /api/pages", "POST /api/pages")
	f.expectBodies(t, `{"chapter_id":9,"name":"Uno","markdown":"uno\n"}`, `{"chapter_id":9,"name":"Dos","markdown":"dos\n"}`)
}

// S6: the first failing request stops the apply: stdout has complete:false
// and what was created, stderr the mapped error, the exit code is the
// error's, and nothing further is sent.
func TestTemplateApplyPartialFailure(t *testing.T) {
	f := projectFake(t)
	f.seq["POST /api/chapters"] = []reply{ok(createdHist), {500, serverError, "application/json"}}
	f.seq["POST /api/pages"] = append(f.seq["POST /api/pages"], ok(`{"id":99,"name":"never"}`))
	flag := flagTemplates(t)
	exec(t, "", "template", "apply", "proyecto", "--var", "proyecto=IRIS", "--templates-dir", flag).expect(t, 1,
		`{"template":"proyecto","complete":false,"created":[{"type":"book","id":20,"name":"Proyecto IRIS"},{"type":"page","id":30,"name":"Ficha"},`+
			`{"type":"chapter","id":21,"name":"Historial"},{"type":"page","id":31,"name":"Changelog"}]}`+"\n",
		errJSON("server", 500, "Boom"))
	f.expectRequests(t, "POST /api/books", "POST /api/pages", "POST /api/chapters", "POST /api/pages", "POST /api/chapters")

	// A failure on the first request reports an empty created list, with its
	// own exit code.
	g := newFake(t, map[string]reply{"POST /api/pages": {404, `{"error":{"code":404,"message":"Book not found"}}`, "application/json"}})
	exec(t, "", "template", "apply", "pagina", "--book", "999", "--templates-dir", flag).expect(t, 4,
		`{"template":"pagina","complete":false,"created":[]}`+"\n", errJSON("not_found", 404, "Book not found"))
	g.expectRequests(t, "POST /api/pages")
}

// S5: --dry-run prints the plan without any request, and needs no
// credentials.
func TestTemplateApplyDryRun(t *testing.T) {
	f := newFake(t, nil)
	flag := flagTemplates(t)
	exec(t, "", "template", "apply", "proyecto", "--var", "proyecto=IRIS", "--dry-run", "--templates-dir", flag).expect(t, 0,
		`{"template":"proyecto","target":{"type":"new_book","id":null},"operations":[`+
			`{"op":"create_book","name":"Proyecto IRIS"},`+
			`{"op":"create_page","name":"Ficha","parent":"Proyecto IRIS","bytes":13},`+
			`{"op":"create_chapter","name":"Historial","parent":"Proyecto IRIS"},`+
			`{"op":"create_page","name":"Changelog","parent":"Historial","bytes":12},`+
			`{"op":"create_chapter","name":"Backlog IRIS","parent":"Proyecto IRIS"}]}`+"\n", "")
	exec(t, "", "template", "apply", "proyecto", "--var", "proyecto=IRIS", "--dry-run", "--book", "7", "--templates-dir", flag).expect(t, 0,
		`{"template":"proyecto","target":{"type":"book","id":7},"operations":[`+
			`{"op":"create_page","name":"Ficha","parent":7,"bytes":13},`+
			`{"op":"create_chapter","name":"Historial","parent":7},`+
			`{"op":"create_page","name":"Changelog","parent":"Historial","bytes":12},`+
			`{"op":"create_chapter","name":"Backlog IRIS","parent":7}]}`+"\n", "")
	exec(t, "", "template", "apply", "pagina", "--dry-run", "--chapter", "9", "--templates-dir", flag).expect(t, 0,
		`{"template":"pagina","target":{"type":"chapter","id":9},"operations":[`+
			`{"op":"create_page","name":"Uno","parent":9,"bytes":4},{"op":"create_page","name":"Dos","parent":9,"bytes":4}]}`+"\n", "")
	f.expectRequests(t)

	t.Setenv("BOOKSTACK_URL", "")
	t.Setenv("BOOKSTACK_TOKEN_SECRET", "")
	exec(t, "", "template", "apply", "pagina", "--dry-run", "--book", "7", "--templates-dir", flag).expect(t, 0,
		`{"template":"pagina","target":{"type":"book","id":7},"operations":[`+
			`{"op":"create_page","name":"Uno","parent":7,"bytes":4},{"op":"create_page","name":"Dos","parent":7,"bytes":4}]}`+"\n", "")
	f.expectRequests(t)
}

// S2, S3, S4: every usage and validation error is reported before any
// request.
func TestTemplateApplyErrors(t *testing.T) {
	f := newFake(t, nil)
	flag := flagTemplates(t)
	broken := writeTemplate(t, flag, "roto", brokenTemplate)
	blank := writeTemplate(t, flag, "vacia", map[string]string{
		"template.toml": "vars = [\"x\"]\n[book]\n[[book.pages]]\nname = \"P\"\nfile = \"p.md\"\n",
		"p.md":          "{{.x}}",
	})
	cases := []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"proyecto", "--book", "7", "--chapter", "9", "--var", "proyecto=IRIS"}, 2,
			errJSON("usage", 0, "--book and --chapter are mutually exclusive")},
		{[]string{"proyecto"}, 2,
			errJSON("usage", 0, `missing --var proyecto: template \"proyecto\" declares proyecto`)},
		{[]string{"proyecto", "--var", "proyecto=IRIS", "--var", "proyeto=x"}, 2,
			errJSON("usage", 0, `unknown --var proyeto: template \"proyecto\" declares proyecto`)},
		{[]string{"proyecto", "--var", "proyecto"}, 2,
			errJSON("usage", 0, `invalid argument \"proyecto\" for \"--var\" flag: must be key=value`)},
		{[]string{"proyecto", "--var", "=IRIS"}, 2,
			errJSON("usage", 0, `invalid argument \"=IRIS\" for \"--var\" flag: key must not be empty`)},
		{[]string{"proyecto", "--var", "proyecto=a", "--var", "proyecto=b"}, 2,
			errJSON("usage", 0, `invalid argument \"proyecto=b\" for \"--var\" flag: proyecto is given twice`)},
		{[]string{"proyecto", "--chapter", "9", "--var", "proyecto=IRIS"}, 2,
			errJSON("usage", 0, `template \"proyecto\" has chapters, which cannot be created inside a chapter; use --book or no target`)},
		{[]string{"pagina"}, 2,
			errJSON("usage", 0, `template \"pagina\" has no book.name; use --book or --chapter to apply it to an existing book or chapter`)},
		{[]string{"nope"}, 2,
			errJSON("usage", 0, `unknown template \"nope\"; available: pagina, proyecto, roto, vacia`)},
		{[]string{"proyecto", "--book", "0"}, 2,
			errJSON("usage", 0, `invalid argument \"0\" for \"--book\" flag: must be a positive integer`)},
		{[]string{}, 2, errJSON("usage", 0, "accepts 1 arg(s), received 0")},
		{[]string{"roto"}, 1,
			errJSON("template_invalid", 0, `invalid template \"roto\": `+filepath.Join(broken, "template.toml")+`: missing [book] table`)},
		{[]string{"vacia", "--book", "7", "--var", "x= "}, 1,
			errJSON("template_invalid", 0, `invalid template \"vacia\": `+filepath.Join(blank, "p.md")+`: empty or only whitespace after rendering`)},
	}
	for _, c := range cases {
		for _, dry := range []bool{false, true} {
			args := append([]string{"template", "apply"}, c.args...)
			args = append(args, "--templates-dir", flag)
			if dry {
				args = append(args, "--dry-run")
			}
			r := exec(t, "", args...)
			if r.code != c.code || r.stdout != "" || r.stderr != c.stderr {
				t.Errorf("%q = %d %q %q\nwant %d \"\" %q", args, r.code, r.stdout, r.stderr, c.code, c.stderr)
			}
		}
	}
	f.expectRequests(t)
}

// S4, S7: apply finds templates through the same precedence as list.
func TestTemplateApplyShared(t *testing.T) {
	f := newFake(t, nil)
	f.seq = map[string][]reply{"POST /api/pages": {ok(`{"id":40,"name":"Uno"}`), ok(`{"id":41,"name":"Dos"}`)}}
	writeTemplate(t, filepath.Join(configDir(), "templates"), "pagina", pagesTemplate)
	exec(t, "", "template", "apply", "pagina", "--book", "7").expect(t, 0,
		`{"template":"pagina","complete":true,"created":[{"type":"page","id":40,"name":"Uno"},{"type":"page","id":41,"name":"Dos"}]}`+"\n", "")
	f.expectRequests(t, "POST /api/pages", "POST /api/pages")
}

// Apply without credentials fails before any write and prints nothing on
// stdout.
func TestTemplateApplyConfigMissing(t *testing.T) {
	f := newFake(t, nil)
	t.Setenv("BOOKSTACK_URL", "")
	flag := flagTemplates(t)
	exec(t, "", "template", "apply", "pagina", "--book", "7", "--templates-dir", flag).expect(t, 1, "",
		errJSON("config_missing", 0, `missing: url; set the BOOKSTACK_* environment variables or run \"bsk auth\"`))
	f.expectRequests(t)
}

// Help documents the format, precedence, targets, dry-run, partial failure
// and exit codes.
func TestTemplateHelp(t *testing.T) {
	isolate(t)
	cases := map[string][]string{
		"template":       {"template.toml", "[[book.chapters.pages]]", "--templates-dir", "profiles/<profile>/templates", "shadows", "missingkey", "10 MiB", "examples/templates/proyecto"},
		"template list":  {`"source"`, `"error"`, "no API access", "template_invalid"},
		"template show":  {"resolved", "path", "template_invalid"},
		"template apply": {"--book", "--chapter", "mutually exclusive", "--dry-run", `"complete":false`, "rolled back", "template_invalid", "4  not found", "chapters cannot nest"},
	}
	for cmd, wants := range cases {
		r := exec(t, "", append(strings.Fields(cmd), "--help")...)
		for _, want := range wants {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("%s --help missing %q", cmd, want)
			}
		}
	}
}
