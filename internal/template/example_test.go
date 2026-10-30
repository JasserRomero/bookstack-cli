package template

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// S9: the example template in the repository loads, validates and renders
// a project documentation convention.
func TestExampleProyecto(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "examples", "templates", "proyecto"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := Load(Entry{Name: "proyecto", Source: SourceShared, Dir: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(tpl.Vars, []string{"proyecto"}) {
		t.Errorf("vars = %q", tpl.Vars)
	}
	r, err := tpl.Render(map[string]string{"proyecto": "IRIS"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if r.Name != "Proyecto IRIS" {
		t.Errorf("book name = %q", r.Name)
	}
	var names []string
	for _, p := range r.Pages {
		names = append(names, p.Name)
	}
	for _, ch := range r.Chapters {
		names = append(names, ch.Name)
		for _, p := range ch.Pages {
			names = append(names, "  "+p.Name)
		}
	}
	want := []string{
		"Ficha del documento",
		"Historial y control de cambios",
		"  Changelog",
		"  Registro de incidencias y soluciones",
		"  Lista de mejoras pendientes y backlog",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("structure = %q\nwant        %q", names, want)
	}
	// BookStack shows the page name as its title, so the content must not
	// repeat it as a leading heading.
	all := append([]RenderedPage{}, r.Pages...)
	for _, ch := range r.Chapters {
		all = append(all, ch.Pages...)
	}
	for _, p := range all {
		if strings.HasPrefix(p.Markdown, "# "+p.Name) {
			t.Errorf("page %q repeats its name as a leading heading", p.Name)
		}
	}
	if !strings.Contains(r.Pages[0].Markdown, "IRIS") || strings.Contains(r.Pages[0].Markdown, "{{") {
		t.Errorf("ficha not rendered:\n%s", r.Pages[0].Markdown)
	}
}
