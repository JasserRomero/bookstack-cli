package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JasserRomero/bookstack-cli/internal/client"
	"github.com/JasserRomero/bookstack-cli/internal/config"
	"github.com/JasserRomero/bookstack-cli/internal/template"
	"github.com/spf13/cobra"
)

const templateLong = `Apply company templates stored as files

A template is a directory <name>/ holding template.toml and the Markdown
files it references; <name> is the template name. bsk only reads templates,
it never writes them.

template.toml:
  description = "Project book with document sheet and change log"
  vars = ["proyecto"]                # variables, each required by apply

  [book]
  name = "Proyecto {{.proyecto}}"    # required to create a new book
  description = "..."                # optional, plain text
  tags = { area = "desarrollo" }     # optional, key -> value ("" = no value)

  [[book.pages]]                     # pages directly in the book, in order
  name = "Ficha del documento"
  file = "ficha.md"                  # Markdown, relative to the template dir
  tags = { tipo = "ficha" }          # optional

  [[book.chapters]]                  # chapters, in order, each with its pages
  name = "Historial y control de cambios"
  description = "..."                # optional
  tags = { estado = "vigente" }      # optional
  [[book.chapters.pages]]
  name = "Changelog"
  file = "changelog.md"

Unknown keys are an error. Every page needs a non-blank name and a file that
stays inside the template directory (no absolute path, no "..", symlinks
resolved), readable, valid UTF-8, at most 10 MiB and not blank after
rendering. Names, descriptions, tag values and page files are Go
text/template text rendered with missingkey=error: {{.proyecto}} is the
value of --var proyecto=...; referencing anything else is an error.

Where templates are found, highest precedence first (the "source" field):
  --templates-dir <dir>                         flag
  <config-dir>/profiles/<profile>/templates/    profile
  <config-dir>/templates/                       shared
A name found in a higher source shadows the same name in lower ones.
<config-dir> is the directory of the config file (default
~/.config/bookstack-cli/). <profile> is the profile "bsk test" would use:
--profile, then BOOKSTACK_PROFILE, then default_profile, then the only
profile; without one, only --templates-dir and shared templates are used.

An example reproducing a project book is in examples/templates/proyecto/ of
the bsk repository: copy that directory into <config-dir>/templates/.`

// templateErrorsHelp is the start of the error help of template commands.
const templateErrorsHelp = `

Errors are JSON on stderr: {"error":{"code":"<code>","status":<http>,"message":"..."}};
status is present only when BookStack answered with an HTTP status.

Exit codes:
  0  ok`

const templateReadExitHelp = templateErrorsHelp + `
  1  error (template_invalid: the template fails validation, naming the file
     and the problem; config_invalid, error)
  2  usage (invalid arguments or flags, unknown template or profile)`

const templateApplyExitHelp = templateErrorsHelp + `
  1  error (template_invalid: the template fails validation or rendering,
     naming the file and the problem; config_missing, config_invalid,
     rate_limited, server, http_error, network, invalid_response)
  2  usage (invalid arguments or flags, unknown template or profile, missing
     or undeclared --var, a target the template cannot use; no request is made)
  3  auth: invalid token or missing permission (HTTP 401/403)
  4  not found (HTTP 404), such as a --book or --chapter that does not exist
  6  validation (HTTP 422)`

const templateListLong = `List the available templates
Prints a JSON array sorted by name:
  [{"name","description","vars","source","path"}]
source is flag, profile or shared and path is the template directory. Only
the highest-precedence template of each name is listed. A template that
fails validation is listed with an "error" field instead of failing the
command. There is no API access: no credentials are needed.`

const templateShowLong = `Show a template
Prints the parsed template.toml as JSON, not rendered, with the name,
source and path of the template and the resolved path of every page file:
  {"name","description","vars","source","path","book":{"name","description",
   "tags","pages":[{"name","file","tags","path"}],"chapters":[{"name",
   "description","tags","pages":[...]}]}}
description and tags are omitted when empty. An invalid template fails with
code template_invalid. There is no API access: no credentials are needed.`

const templateApplyLong = `Create a template's book, chapters and pages in BookStack
<name> is a template from "bsk template list". --var key=value, repeatable,
sets each variable of the template's vars: every declared var is required
and an undeclared one is a usage error (typo guard). The whole template is
validated and rendered before the first request.

Target:
  (none)          create a new book from [book] (book.name is required), then
                  its pages and chapters
  --book <id>     create the pages and chapters in that existing book; the
                  name, description and tags of [book] are ignored
  --chapter <id>  create the book pages in that existing chapter; a template
                  with chapters is a usage error (chapters cannot nest)
--book and --chapter are mutually exclusive.

Creation order: the book, then book.pages in order, then each chapter
followed by its pages. Pages use the Markdown editor; tags are sent sorted by
key. BookStack allows duplicate names: applying a template twice creates
everything twice.

--dry-run prints the plan and makes no request (no credentials needed):
  {"template":"<name>","target":{"type":"new_book"|"book"|"chapter","id":<id>|null},
   "operations":[{"op":"create_book"|"create_chapter"|"create_page","name":"...",
   "parent":"<name>"|<id>,"bytes":<n>}]}
parent is the name of a parent the plan creates, or the id of the --book or
--chapter target; create_book has no parent. bytes, on create_page only, is
the size of the rendered content.

Output:
  {"template":"<name>","complete":true,"created":[{"type":"book"|"chapter"|"page","id":N,"name":"..."}]}
Partial failure: the first failing request stops the apply and nothing else
is sent. stdout still has the object, with "complete":false and the entities
already created, stderr has the error and the exit code is the error's.
Nothing is rolled back: finish the structure with "chapters create" and
"pages create", or remove what was created in BookStack.`

func newTemplateCmd(d apiDeps) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "template",
		Short: "List, show and apply file-based templates",
		Long:  templateLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&dir, "templates-dir", "", "directory of templates, searched before the config directory")
	sources := func(cmd *cobra.Command) ([]template.Source, error) {
		return templateSources(*d.profile, dir, cmd.Flags().Changed("templates-dir"))
	}
	cmd.AddCommand(newTemplateListCmd(sources), newTemplateShowCmd(sources), newTemplateApplyCmd(d, sources))
	return cmd
}

type sourcesFunc func(cmd *cobra.Command) ([]template.Source, error)

// templateSources returns the template sources for the --profile value and
// the --templates-dir value, when set.
func templateSources(profileFlag, dir string, dirSet bool) ([]template.Source, error) {
	if dirSet {
		if dir == "" {
			return nil, usageErrorf("invalid --templates-dir %q: must not be empty", dir)
		}
		info, err := os.Stat(dir)
		if err != nil {
			return nil, usageErrorf("invalid --templates-dir %q: %s", dir, pathErrorReason(err))
		}
		if !info.IsDir() {
			return nil, usageErrorf("invalid --templates-dir %q: not a directory", dir)
		}
		if dir, err = filepath.Abs(dir); err != nil {
			return nil, err
		}
	}
	path, profile, err := selectedProfile(profileFlag)
	if err != nil {
		return nil, err
	}
	if profile == "." || profile == ".." || strings.ContainsAny(profile, `/\`) {
		return nil, usageErrorf("profile name %q cannot be used as a templates directory", profile)
	}
	return template.Sources(dir, filepath.Dir(path), profile), nil
}

// selectedProfile returns the config file path and the profile config.Load
// selects for the --profile value flag (see config.Load), without requiring
// its credentials: listing templates needs none. "" means no profile.
func selectedProfile(flag string) (path, name string, err error) {
	if path, err = config.Path(); err != nil {
		return "", "", err
	}
	f, err := config.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	if name, err = config.SelectProfile(path, f, flag); err != nil {
		return "", "", err
	}
	return path, name, nil
}

// templateItem is one element of "template list".
type templateItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Vars        []string `json:"vars"`
	Source      string   `json:"source"`
	Path        string   `json:"path"`
	Error       string   `json:"error,omitempty"`
}

func newTemplateListCmd(sources sourcesFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: strings.SplitN(templateListLong, "\n", 2)[0],
		Long:  templateListLong + templateReadExitHelp,
		Example: `  bsk template list
  bsk template list --templates-dir ./templates
  bsk --profile client template list`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			srcs, err := sources(cmd)
			if err != nil {
				return err
			}
			entries, err := template.Discover(srcs)
			if err != nil {
				return err
			}
			out := []templateItem{}
			for _, e := range entries {
				item := templateItem{Name: e.Name, Vars: []string{}, Source: e.Source, Path: e.Dir}
				if t, err := template.Load(e); err != nil {
					item.Error = err.Error()
				} else {
					item.Description, item.Vars = t.Description, t.Vars
				}
				out = append(out, item)
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
}

// loadTemplate finds and loads the template called name.
func loadTemplate(cmd *cobra.Command, sources sourcesFunc, name string) (*template.Template, error) {
	srcs, err := sources(cmd)
	if err != nil {
		return nil, err
	}
	e, err := template.Find(srcs, name)
	if err != nil {
		return nil, err
	}
	return template.Load(e)
}

func newTemplateShowCmd(sources sourcesFunc) *cobra.Command {
	return &cobra.Command{
		Use:     "show <name>",
		Short:   strings.SplitN(templateShowLong, "\n", 2)[0],
		Long:    templateShowLong + templateReadExitHelp,
		Example: `  bsk template show proyecto`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := loadTemplate(cmd, sources, args[0])
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Vars        []string       `json:"vars"`
				Source      string         `json:"source"`
				Path        string         `json:"path"`
				Book        *template.Book `json:"book"`
			}{t.Name, t.Description, t.Vars, t.Source, t.Dir, t.Book})
		},
	}
}

// varsFlag is a repeatable --var key=value flag.
type varsFlag map[string]string

func (f *varsFlag) String() string {
	parts := make([]string, 0, len(*f))
	for k, v := range *f {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (f *varsFlag) Set(s string) error {
	key, value, found := strings.Cut(s, "=")
	switch {
	case !found:
		return fmt.Errorf("must be key=value")
	case key == "":
		return fmt.Errorf("key must not be empty")
	}
	if _, dup := (*f)[key]; dup {
		return fmt.Errorf("%s is given twice", key)
	}
	if *f == nil {
		*f = varsFlag{}
	}
	(*f)[key] = value
	return nil
}

func (f *varsFlag) Type() string { return "key=value" }

// planOp is one operation of the --dry-run plan. Parent is the name of a
// parent created by the plan or the id of the target.
type planOp struct {
	Op     string `json:"op"`
	Name   string `json:"name"`
	Parent any    `json:"parent,omitempty"`
	Bytes  int    `json:"bytes,omitempty"`
}

type planTarget struct {
	Type string `json:"type"`
	ID   *int   `json:"id"`
}

type createdItem struct {
	Type string `json:"type"`
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type applyResult struct {
	Template string        `json:"template"`
	Complete bool          `json:"complete"`
	Created  []createdItem `json:"created"`
}

func newTemplateApplyCmd(d apiDeps, sources sourcesFunc) *cobra.Command {
	var (
		book, chapter idFlag
		vars          varsFlag
		dryRun        bool
	)
	cmd := &cobra.Command{
		Use:   "apply <name>",
		Short: strings.SplitN(templateApplyLong, "\n", 2)[0],
		Long:  templateApplyLong + templateApplyExitHelp,
		Example: `  bsk template apply proyecto --var proyecto=IRIS --dry-run
  bsk template apply proyecto --var proyecto=IRIS
  bsk template apply proyecto --var proyecto=IRIS --book 7
  bsk template apply pages --chapter 9 --templates-dir ./templates`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if book != 0 && chapter != 0 {
				return usageErrorf("--book and --chapter are mutually exclusive")
			}
			t, err := loadTemplate(cmd, sources, args[0])
			if err != nil {
				return err
			}
			if err := t.CheckVars(vars); err != nil {
				return err
			}
			switch {
			case chapter != 0 && len(t.Book.Chapters) > 0:
				return usageErrorf("template %q has chapters, which cannot be created inside a chapter; use --book or no target", t.Name)
			case book == 0 && chapter == 0 && t.Book.Name == "":
				return usageErrorf("template %q has no book.name; use --book or --chapter to apply it to an existing book or chapter", t.Name)
			}
			r, err := t.Render(vars)
			if err != nil {
				return err
			}
			if dryRun {
				return writeJSON(cmd.OutOrStdout(), templatePlan(t.Name, r, int(book), int(chapter)))
			}
			c, err := d.client()
			if err != nil {
				return err
			}
			return applyTemplate(cmd, c, t.Name, r, int(book), int(chapter))
		},
	}
	cmd.Flags().Var(&book, "book", "existing book id to create the pages and chapters in")
	cmd.Flags().Var(&chapter, "chapter", "existing chapter id to create the book pages in")
	cmd.Flags().Var(&vars, "var", "template variable as key=value, repeatable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan as JSON without any request")
	return cmd
}

func templatePlan(name string, r *template.Rendered, book, chapter int) any {
	target := planTarget{Type: "new_book"}
	var parent any = r.Name
	ops := []planOp{}
	switch {
	case chapter != 0:
		target, parent = planTarget{Type: "chapter", ID: &chapter}, chapter
	case book != 0:
		target, parent = planTarget{Type: "book", ID: &book}, book
	default:
		ops = append(ops, planOp{Op: "create_book", Name: r.Name})
	}
	for _, p := range r.Pages {
		ops = append(ops, planOp{Op: "create_page", Name: p.Name, Parent: parent, Bytes: len(p.Markdown)})
	}
	for _, ch := range r.Chapters {
		ops = append(ops, planOp{Op: "create_chapter", Name: ch.Name, Parent: parent})
		for _, p := range ch.Pages {
			ops = append(ops, planOp{Op: "create_page", Name: p.Name, Parent: ch.Name, Bytes: len(p.Markdown)})
		}
	}
	return struct {
		Template   string     `json:"template"`
		Target     planTarget `json:"target"`
		Operations []planOp   `json:"operations"`
	}{name, target, ops}
}

// applyTemplate creates the rendered template in order. On the first
// failing request it prints what was created with complete:false and
// returns the error.
func applyTemplate(cmd *cobra.Command, c *client.Client, name string, r *template.Rendered, book, chapter int) error {
	ctx, stdout := cmd.Context(), cmd.OutOrStdout()
	out := applyResult{Template: name, Created: []createdItem{}}
	fail := func(err error) error {
		_ = writeJSON(stdout, out)
		return err
	}
	created := func(typ string, id int, name string) {
		out.Created = append(out.Created, createdItem{Type: typ, ID: id, Name: name})
	}
	createPage := func(p template.RenderedPage, bookID, chapterID int) error {
		pg, err := c.CreatePage(ctx, client.NewPage{
			BookID: bookID, ChapterID: chapterID, Name: p.Name, Markdown: p.Markdown, Tags: clientTags(p.Tags),
		})
		if err != nil {
			return err
		}
		created("page", pg.ID, pg.Name)
		return nil
	}
	if book == 0 && chapter == 0 {
		b, err := c.CreateBook(ctx, client.NewBook{Name: r.Name, Description: r.Description, Tags: clientTags(r.Tags)})
		if err != nil {
			return fail(err)
		}
		created("book", b.ID, b.Name)
		book = b.ID
	}
	for _, p := range r.Pages {
		bookID := book
		if chapter != 0 {
			bookID = 0
		}
		if err := createPage(p, bookID, chapter); err != nil {
			return fail(err)
		}
	}
	for _, ch := range r.Chapters {
		created, err := c.CreateChapter(ctx, client.NewChapter{
			BookID: book, Name: ch.Name, Description: ch.Description, Tags: clientTags(ch.Tags),
		})
		if err != nil {
			return fail(err)
		}
		out.Created = append(out.Created, createdItem{Type: "chapter", ID: created.ID, Name: created.Name})
		for _, p := range ch.Pages {
			if err := createPage(p, 0, created.ID); err != nil {
				return fail(err)
			}
		}
	}
	out.Complete = true
	return writeJSON(stdout, out)
}

func clientTags(tags []template.Tag) []client.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]client.Tag, len(tags))
	for i, t := range tags {
		out[i] = client.Tag{Name: t.Name, Value: t.Value}
	}
	return out
}
