package client

import (
	"encoding/json"
	"testing"
)

func TestUserRefAcceptsIntOrObject(t *testing.T) {
	cases := map[string]UserRef{
		`1`:                                  {ID: 1},
		`{"id":2,"name":"Ana","slug":"ana"}`: {ID: 2, Name: "Ana", Slug: "ana"},
		`null`:                               {},
	}
	for in, want := range cases {
		var got UserRef
		if err := json.Unmarshal([]byte(in), &got); err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", in, got, want)
		}
	}
	var bad UserRef
	if err := json.Unmarshal([]byte(`"x"`), &bad); err == nil {
		t.Error("expected error for a string user reference")
	}
}

func TestUserRefMarshalKeepsServerShape(t *testing.T) {
	cases := map[string]UserRef{
		`3`:                                  {ID: 3},
		`{"id":2,"name":"Ana","slug":"ana"}`: {ID: 2, Name: "Ana", Slug: "ana"},
	}
	for want, in := range cases {
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Errorf("Marshal(%+v) = %s, want %s", in, b, want)
		}
	}
}

func TestPageDecodesListAndGetShapes(t *testing.T) {
	list := `{"book_id":62,"book_slug":"b","chapter_id":0,"created_at":"2025-01-01T00:00:00.000000Z","created_by":1,"draft":false,"editor":"wysiwyg","id":5,"name":"P","owned_by":1,"priority":3,"revision_count":2,"slug":"p","template":false,"updated_at":"2025-01-02T00:00:00.000000Z","updated_by":4}`
	var p Page
	if err := json.Unmarshal([]byte(list), &p); err != nil {
		t.Fatal(err)
	}
	if p.ID != 5 || p.BookID != 62 || p.BookSlug != "b" || p.Editor != "wysiwyg" || p.CreatedBy.ID != 1 || p.UpdatedBy.ID != 4 || p.OwnedBy.ID != 1 || p.RevisionCount != 2 || p.UpdatedAt != "2025-01-02T00:00:00.000000Z" {
		t.Errorf("list page = %+v", p)
	}
	get := `{"id":5,"created_by":{"id":1,"name":"Admin","slug":"admin"},"updated_by":{"id":4,"name":"Bot","slug":"bot"},"owned_by":{"id":1,"name":"Admin","slug":"admin"},"html":"<p>x</p>","raw_html":"<p>x</p>","markdown":"","tags":[{"name":"k","value":"v"}]}`
	p = Page{}
	if err := json.Unmarshal([]byte(get), &p); err != nil {
		t.Fatal(err)
	}
	if p.CreatedBy != (UserRef{ID: 1, Name: "Admin", Slug: "admin"}) || p.UpdatedBy.Name != "Bot" || p.HTML != "<p>x</p>" || p.RawHTML != "<p>x</p>" || len(p.Tags) != 1 || p.Tags[0] != (Tag{Name: "k", Value: "v"}) {
		t.Errorf("get page = %+v", p)
	}
}
