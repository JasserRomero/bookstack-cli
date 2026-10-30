// Package template loads, validates and renders the file-based templates
// applied by "bsk template". A template is a directory holding template.toml
// and the Markdown files it references.
//
// The package only reads template files: it has no cobra, no stdout and no
// HTTP, so the CLI owns output and the client owns requests.
package template

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	gotemplate "text/template"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// ManifestFile is the manifest of a template directory.
const ManifestFile = "template.toml"

// MaxFileBytes bounds a page file, before and after rendering.
const MaxFileBytes = 10 << 20

// Kinds of template source, highest precedence first.
const (
	SourceFlag    = "flag"
	SourceProfile = "profile"
	SourceShared  = "shared"
)

// Source is a directory of templates.
type Source struct {
	Kind string
	Dir  string
}

// Sources returns the template directories in precedence order: flagDir
// when set, then the profile's templates when profile is set, then the
// shared templates of configDir.
func Sources(flagDir, configDir, profile string) []Source {
	var s []Source
	if flagDir != "" {
		s = append(s, Source{SourceFlag, flagDir})
	}
	if profile != "" {
		s = append(s, Source{SourceProfile, filepath.Join(configDir, "profiles", profile, "templates")})
	}
	return append(s, Source{SourceShared, filepath.Join(configDir, "templates")})
}

// Entry is a template found in a source, not yet loaded.
type Entry struct {
	Name   string
	Source string
	Dir    string
}

// Discover returns the templates of sources sorted by name. A name found in
// a higher source shadows the same name in lower ones. Only directories
// holding a template.toml are templates; missing source directories are
// skipped.
func Discover(sources []Source) ([]Entry, error) {
	seen := map[string]bool{}
	var out []Entry
	for _, src := range sources {
		dirents, err := os.ReadDir(src.Dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read templates directory %s: %w", src.Dir, err)
		}
		for _, d := range dirents {
			name := d.Name()
			if seen[name] || !isTemplate(filepath.Join(src.Dir, name)) {
				continue
			}
			seen[name] = true
			out = append(out, Entry{Name: name, Source: src.Kind, Dir: filepath.Join(src.Dir, name)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func isTemplate(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ManifestFile))
	return err == nil && info.Mode().IsRegular()
}

// NotFoundError reports a template name that no source holds.
type NotFoundError struct {
	Name      string
	Available []string
}

func (e *NotFoundError) Error() string {
	available := "none"
	if len(e.Available) > 0 {
		available = strings.Join(e.Available, ", ")
	}
	return fmt.Sprintf("unknown template %q; available: %s", e.Name, available)
}

// NameError reports a template name that is not a plain directory name.
type NameError struct{ Name string }

func (e *NameError) Error() string {
	return fmt.Sprintf("invalid template name %q: must be a directory name", e.Name)
}

// Find returns the template called name from the highest source holding it.
func Find(sources []Source, name string) (Entry, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return Entry{}, &NameError{Name: name}
	}
	for _, src := range sources {
		dir := filepath.Join(src.Dir, name)
		if isTemplate(dir) {
			return Entry{Name: name, Source: src.Kind, Dir: dir}, nil
		}
	}
	entries, err := Discover(sources)
	if err != nil {
		return Entry{}, err
	}
	nf := &NotFoundError{Name: name}
	for _, e := range entries {
		nf.Available = append(nf.Available, e.Name)
	}
	return Entry{}, nf
}

// Manifest is the content of template.toml. Names, descriptions, tag values
// and page files are text/template text rendered with the vars.
type Manifest struct {
	Description string   `toml:"description" json:"description"`
	Vars        []string `toml:"vars" json:"vars"`
	Book        *Book    `toml:"book" json:"book"`
}

// Book is the [book] table. Name is required only to create a new book.
type Book struct {
	Name        string            `toml:"name" json:"name"`
	Description string            `toml:"description" json:"description,omitempty"`
	Tags        map[string]string `toml:"tags" json:"tags,omitempty"`
	Pages       []Page            `toml:"pages" json:"pages"`
	Chapters    []Chapter         `toml:"chapters" json:"chapters"`
}

// Chapter is a [[book.chapters]] entry.
type Chapter struct {
	Name        string            `toml:"name" json:"name"`
	Description string            `toml:"description" json:"description,omitempty"`
	Tags        map[string]string `toml:"tags" json:"tags,omitempty"`
	Pages       []Page            `toml:"pages" json:"pages"`
}

// Page is a [[book.pages]] or [[book.chapters.pages]] entry. Path is File
// resolved in the template directory.
type Page struct {
	Name string            `toml:"name" json:"name"`
	File string            `toml:"file" json:"file"`
	Tags map[string]string `toml:"tags" json:"tags,omitempty"`
	Path string            `toml:"-" json:"path"`

	content string
}

// Template is a loaded and validated template.
type Template struct {
	Name   string
	Source string
	Dir    string
	Manifest
}

// InvalidError reports a template that fails validation or rendering,
// naming the file and the problem.
type InvalidError struct {
	Template string
	File     string
	Problem  string
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("invalid template %q: %s: %s", e.Template, e.File, e.Problem)
}

// VarsError reports --var values that do not match the declared vars.
type VarsError struct{ msg string }

func (e *VarsError) Error() string { return e.msg }

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// loader validates one template, building errors that name its files.
type loader struct {
	name     string
	dir      string
	realDir  string
	manifest string
}

func (l *loader) invalid(file, format string, args ...any) error {
	return &InvalidError{Template: l.name, File: file, Problem: fmt.Sprintf(format, args...)}
}

// Load reads and validates the template of e: the manifest (unknown keys
// are an error), its texts and every page file. Nothing is rendered.
func Load(e Entry) (*Template, error) {
	l := &loader{name: e.Name, dir: e.Dir, manifest: filepath.Join(e.Dir, ManifestFile)}
	data, err := os.ReadFile(l.manifest)
	if err != nil {
		return nil, l.invalid(l.manifest, "cannot read: %s", pathErrorReason(err))
	}
	var m Manifest
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, l.invalid(l.manifest, "invalid TOML: %v", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, l.invalid(l.manifest, "unknown keys: %s", strings.Join(keys, ", "))
	}
	if m.Book == nil {
		return nil, l.invalid(l.manifest, "missing [book] table")
	}
	if m.Vars == nil {
		m.Vars = []string{}
	}
	declared := map[string]bool{}
	for _, v := range m.Vars {
		if !varName.MatchString(v) {
			return nil, l.invalid(l.manifest, "vars: %q must be a letter or underscore followed by letters, digits or underscores", v)
		}
		if declared[v] {
			return nil, l.invalid(l.manifest, "vars: %q is declared twice", v)
		}
		declared[v] = true
	}
	if l.realDir, err = filepath.EvalSymlinks(l.dir); err != nil {
		return nil, l.invalid(l.dir, "cannot read: %s", pathErrorReason(err))
	}

	b := m.Book
	if md.IsDefined("book", "name") && strings.TrimSpace(b.Name) == "" {
		return nil, l.invalid(l.manifest, "book.name: must not be blank")
	}
	if err := l.checkTexts("book", b.Name, b.Description, b.Tags); err != nil {
		return nil, err
	}
	if b.Pages, err = l.loadPages("book.pages", b.Pages); err != nil {
		return nil, err
	}
	if b.Chapters == nil {
		b.Chapters = []Chapter{}
	}
	for i := range b.Chapters {
		ch := &b.Chapters[i]
		label := fmt.Sprintf("book.chapters[%d]", i+1)
		if strings.TrimSpace(ch.Name) == "" {
			return nil, l.invalid(l.manifest, "%s.name: must not be blank", label)
		}
		if err := l.checkTexts(label, ch.Name, ch.Description, ch.Tags); err != nil {
			return nil, err
		}
		if ch.Pages, err = l.loadPages(label+".pages", ch.Pages); err != nil {
			return nil, err
		}
	}
	return &Template{Name: e.Name, Source: e.Source, Dir: e.Dir, Manifest: m}, nil
}

// checkTexts parses the name, description and tag values of an item.
func (l *loader) checkTexts(label, name, description string, tags map[string]string) error {
	if err := l.parse(label+".name", name); err != nil {
		return err
	}
	if err := l.parse(label+".description", description); err != nil {
		return err
	}
	for _, k := range sortedKeys(tags) {
		if strings.TrimSpace(k) == "" {
			return l.invalid(l.manifest, "%s.tags: key must not be blank", label)
		}
		if err := l.parse(label+".tags."+k, tags[k]); err != nil {
			return err
		}
	}
	return nil
}

func (l *loader) parse(label, text string) error {
	if _, err := newText(label, text); err != nil {
		return l.invalid(l.manifest, "%v", err)
	}
	return nil
}

func (l *loader) loadPages(label string, pages []Page) ([]Page, error) {
	if pages == nil {
		pages = []Page{}
	}
	for i := range pages {
		p := &pages[i]
		item := fmt.Sprintf("%s[%d]", label, i+1)
		if strings.TrimSpace(p.Name) == "" {
			return nil, l.invalid(l.manifest, "%s.name: must not be blank", item)
		}
		if err := l.parse(item+".name", p.Name); err != nil {
			return nil, err
		}
		for _, k := range sortedKeys(p.Tags) {
			if strings.TrimSpace(k) == "" {
				return nil, l.invalid(l.manifest, "%s.tags: key must not be blank", item)
			}
			if err := l.parse(item+".tags."+k, p.Tags[k]); err != nil {
				return nil, err
			}
		}
		if err := l.readPage(item, p); err != nil {
			return nil, err
		}
	}
	return pages, nil
}

// readPage resolves and reads the file of page p: relative, inside the
// template directory once symlinks are resolved, readable, at most
// MaxFileBytes, valid UTF-8, not blank and a valid text/template.
func (l *loader) readPage(item string, p *Page) error {
	if p.File == "" {
		return l.invalid(l.manifest, "%s.file: is required", item)
	}
	if filepath.IsAbs(p.File) || filepath.VolumeName(p.File) != "" || strings.HasPrefix(p.File, "/") || strings.HasPrefix(p.File, `\`) {
		return l.invalid(l.manifest, "%s.file: %q must be a relative path", item, p.File)
	}
	p.Path = filepath.Join(l.dir, p.File)
	if !inside(l.dir, p.Path) {
		return l.invalid(l.manifest, "%s.file: %q is outside the template directory", item, p.File)
	}
	real, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return l.invalid(p.Path, "cannot read: %s", pathErrorReason(err))
	}
	if !inside(l.realDir, real) {
		return l.invalid(p.Path, "resolves outside the template directory")
	}
	f, err := os.Open(real)
	if err != nil {
		return l.invalid(p.Path, "cannot read: %s", pathErrorReason(err))
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return l.invalid(p.Path, "cannot read: %s", pathErrorReason(err))
	}
	switch {
	case len(data) > MaxFileBytes:
		return l.invalid(p.Path, "larger than 10 MiB")
	case !utf8.Valid(data):
		return l.invalid(p.Path, "not valid UTF-8")
	case len(bytes.TrimSpace(data)) == 0:
		return l.invalid(p.Path, "empty or only whitespace")
	}
	p.content = string(data)
	if _, err := newText(p.File, p.content); err != nil {
		return l.invalid(p.Path, "%v", err)
	}
	return nil
}

// inside reports whether path is strictly inside dir.
func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pathErrorReason drops the path from err, already named by the caller.
func pathErrorReason(err error) string {
	var perr *fs.PathError
	if errors.As(err, &perr) {
		return perr.Err.Error()
	}
	return err.Error()
}

func newText(name, text string) (*gotemplate.Template, error) {
	return gotemplate.New(name).Option("missingkey=error").Parse(text)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CheckVars reports a *VarsError when vars has a name the template does not
// declare (a typo guard) or lacks a declared one.
func (t *Template) CheckVars(vars map[string]string) error {
	declared := "no vars"
	if len(t.Vars) > 0 {
		declared = strings.Join(t.Vars, ", ")
	}
	isDeclared := map[string]bool{}
	for _, v := range t.Vars {
		isDeclared[v] = true
	}
	var unknown []string
	for _, k := range sortedKeys(vars) {
		if !isDeclared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return &VarsError{fmt.Sprintf("unknown --var %s: template %q declares %s", strings.Join(unknown, ", "), t.Name, declared)}
	}
	var missing []string
	for _, v := range t.Vars {
		if _, ok := vars[v]; !ok {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return &VarsError{fmt.Sprintf("missing --var %s: template %q declares %s", strings.Join(missing, ", "), t.Name, declared)}
	}
	return nil
}

// Tag is a rendered tag; an empty Value is a tag without value.
type Tag struct {
	Name  string
	Value string
}

// Rendered is the [book] of a template with every text rendered. Tags are
// sorted by key; an empty Name means the manifest has no book.name.
type Rendered struct {
	Name        string
	Description string
	Tags        []Tag
	Pages       []RenderedPage
	Chapters    []RenderedChapter
}

// RenderedChapter is a rendered [[book.chapters]] entry.
type RenderedChapter struct {
	Name        string
	Description string
	Tags        []Tag
	Pages       []RenderedPage
}

// RenderedPage is a rendered page with its Markdown content.
type RenderedPage struct {
	Name     string
	Markdown string
	Tags     []Tag
}

// Render checks vars (see CheckVars) and renders every name, description,
// tag value and page file with them; a reference to anything else is an
// error. Rendered page content must stay non-blank and at most
// MaxFileBytes, and rendered names non-blank.
func (t *Template) Render(vars map[string]string) (*Rendered, error) {
	if err := t.CheckVars(vars); err != nil {
		return nil, err
	}
	if vars == nil {
		vars = map[string]string{}
	}
	r := &renderer{
		loader: loader{name: t.Name, dir: t.Dir, manifest: filepath.Join(t.Dir, ManifestFile)},
		vars:   vars,
	}
	b := t.Book
	out := &Rendered{}
	var err error
	if out.Name, err = r.text("book.name", b.Name, b.Name != ""); err != nil {
		return nil, err
	}
	if out.Description, err = r.text("book.description", b.Description, false); err != nil {
		return nil, err
	}
	if out.Tags, err = r.tags("book", b.Tags); err != nil {
		return nil, err
	}
	if out.Pages, err = r.pages("book.pages", b.Pages); err != nil {
		return nil, err
	}
	for i, ch := range b.Chapters {
		label := fmt.Sprintf("book.chapters[%d]", i+1)
		rc := RenderedChapter{}
		if rc.Name, err = r.text(label+".name", ch.Name, true); err != nil {
			return nil, err
		}
		if rc.Description, err = r.text(label+".description", ch.Description, false); err != nil {
			return nil, err
		}
		if rc.Tags, err = r.tags(label, ch.Tags); err != nil {
			return nil, err
		}
		if rc.Pages, err = r.pages(label+".pages", ch.Pages); err != nil {
			return nil, err
		}
		out.Chapters = append(out.Chapters, rc)
	}
	return out, nil
}

type renderer struct {
	loader
	vars map[string]string
}

func (r *renderer) execute(name, text string) (string, error) {
	tpl, err := newText(name, text)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, r.vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// text renders a manifest text; required texts must not render blank.
func (r *renderer) text(label, text string, required bool) (string, error) {
	s, err := r.execute(label, text)
	if err != nil {
		return "", r.invalid(r.manifest, "%v", err)
	}
	if required && strings.TrimSpace(s) == "" {
		return "", r.invalid(r.manifest, "%s: blank after rendering", label)
	}
	return s, nil
}

func (r *renderer) tags(label string, tags map[string]string) ([]Tag, error) {
	var out []Tag
	for _, k := range sortedKeys(tags) {
		v, err := r.text(label+".tags."+k, tags[k], false)
		if err != nil {
			return nil, err
		}
		out = append(out, Tag{Name: k, Value: v})
	}
	return out, nil
}

func (r *renderer) pages(label string, pages []Page) ([]RenderedPage, error) {
	var out []RenderedPage
	for i, p := range pages {
		item := fmt.Sprintf("%s[%d]", label, i+1)
		rp := RenderedPage{}
		var err error
		if rp.Name, err = r.text(item+".name", p.Name, true); err != nil {
			return nil, err
		}
		if rp.Tags, err = r.tags(item, p.Tags); err != nil {
			return nil, err
		}
		md, err := r.execute(p.File, p.content)
		switch {
		case err != nil:
			return nil, r.invalid(p.Path, "%v", err)
		case len(md) > MaxFileBytes:
			return nil, r.invalid(p.Path, "larger than 10 MiB after rendering")
		case strings.TrimSpace(md) == "":
			return nil, r.invalid(p.Path, "empty or only whitespace after rendering")
		}
		rp.Markdown = md
		out = append(out, rp)
	}
	return out, nil
}
