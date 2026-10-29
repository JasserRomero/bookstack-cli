package client

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestSystem(t *testing.T) {
	c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, `{"version":"v25.11.6","instance_id":"abc-123","app_name":"Team Docs"}`)
	})
	sys, err := c.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if *sys != (System{Version: "v25.11.6", InstanceID: "abc-123", AppName: "Team Docs"}) {
		t.Errorf("System = %+v", sys)
	}
	if r := rec.all()[0]; r.URL.RawQuery != "" {
		t.Errorf("query = %q, want none", r.URL.RawQuery)
	}
}

func TestGetMethodsHitTheRightPaths(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		path string
		body string
		run  func(c *Client) (any, error)
		want any
	}{
		{"shelf", "/api/shelves/4", `{"id":4,"name":"S","slug":"s","books":[{"id":1,"name":"B","slug":"b"}],"created_by":{"id":1,"name":"A","slug":"a"}}`,
			func(c *Client) (any, error) { return c.GetShelf(ctx, 4) },
			&Shelf{ID: 4, Name: "S", Slug: "s", Books: []Book{{ID: 1, Name: "B", Slug: "b"}}, CreatedBy: UserRef{ID: 1, Name: "A", Slug: "a"}}},
		{"book", "/api/books/62", `{"id":62,"name":"B","slug":"b","contents":[{"id":3,"name":"C","slug":"c","type":"chapter","book_id":62,"pages":[{"id":8,"name":"P","slug":"p","book_id":62,"chapter_id":3,"draft":false,"template":false}]},{"id":9,"name":"Q","slug":"q","type":"page","book_id":62}]}`,
			func(c *Client) (any, error) { return c.GetBook(ctx, 62) },
			&Book{ID: 62, Name: "B", Slug: "b", Contents: []BookContent{
				{ID: 3, Name: "C", Slug: "c", Type: "chapter", BookID: 62, Pages: []BookContent{{ID: 8, Name: "P", Slug: "p", BookID: 62, ChapterID: 3}}},
				{ID: 9, Name: "Q", Slug: "q", Type: "page", BookID: 62},
			}}},
		{"chapter", "/api/chapters/3", `{"id":3,"book_id":62,"book_slug":"b","name":"C","slug":"c","pages":[{"id":8,"name":"P"}]}`,
			func(c *Client) (any, error) { return c.GetChapter(ctx, 3) },
			&Chapter{ID: 3, BookID: 62, BookSlug: "b", Name: "C", Slug: "c", Pages: []Page{{ID: 8, Name: "P"}}}},
		{"page", "/api/pages/8", `{"id":8,"name":"P","editor":"markdown","markdown":"# P","html":"<h1>P</h1>"}`,
			func(c *Client) (any, error) { return c.GetPage(ctx, 8) },
			&Page{ID: 8, Name: "P", Editor: "markdown", Markdown: "# P", HTML: "<h1>P</h1>"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				writeJSON(w, 200, tc.body)
			})
			got, err := tc.run(c)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			r := rec.all()[0]
			if r.Method != http.MethodGet || r.URL.Path != tc.path || r.URL.RawQuery != "" {
				t.Errorf("request = %s %s?%s, want GET %s", r.Method, r.URL.Path, r.URL.RawQuery, tc.path)
			}
		})
	}
}

func TestExportPageMarkdownReturnsRawBody(t *testing.T) {
	const md = "# Título\n\nBody with {\"json\": \"looking\"} text.\n"
	c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(md))
	})
	got, err := c.ExportPageMarkdown(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if got != md {
		t.Errorf("markdown = %q, want %q", got, md)
	}
	if r := rec.all()[0]; r.Method != http.MethodGet || r.URL.Path != "/api/pages/12/export/markdown" {
		t.Errorf("request = %s %s", r.Method, r.URL.Path)
	}
}

func TestExportPageMarkdownNotFound(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 404, `{"error":{"code":404,"message":"Page not found"}}`)
	})
	_, err := c.ExportPageMarkdown(context.Background(), 12)
	asAPIError(t, err, CodeNotFound, 404)
}

func TestSearch(t *testing.T) {
	body := `{"data":[{"id":5,"name":"P","slug":"p","type":"page","url":"https://d/books/b/page/p","book_id":62,"chapter_id":3,"preview_html":{"name":"<b>P</b>","content":"x"},"tags":[{"name":"k","value":"v"}]},{"id":62,"name":"B","slug":"b","type":"book","url":"https://d/books/b","preview_html":{"name":"B","content":""},"tags":[]}],"total":2}`
	cases := []struct {
		name string
		opts SearchOptions
		want url.Values
	}{
		{"query only", SearchOptions{Query: "plan de marco"}, url.Values{"query": {"plan de marco"}}},
		{"type and count", SearchOptions{Query: "plan", Type: "page", Count: 20}, url.Values{"query": {"plan {type:page}"}, "count": {"20"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				writeJSON(w, 200, body)
			})
			res, err := c.Search(context.Background(), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			r := rec.all()[0]
			if r.Method != http.MethodGet || r.URL.Path != "/api/search" || !reflect.DeepEqual(r.URL.Query(), tc.want) {
				t.Errorf("request = %s %s?%v, want query %v", r.Method, r.URL.Path, r.URL.Query(), tc.want)
			}
			want := &SearchResults{Total: 2, Data: []SearchResult{
				{ID: 5, Name: "P", Slug: "p", Type: "page", URL: "https://d/books/b/page/p", BookID: 62, ChapterID: 3, PreviewHTML: PreviewHTML{Name: "<b>P</b>", Content: "x"}, Tags: []Tag{{Name: "k", Value: "v"}}},
				{ID: 62, Name: "B", Slug: "b", Type: "book", URL: "https://d/books/b", PreviewHTML: PreviewHTML{Name: "B"}, Tags: []Tag{}},
			}}
			if !reflect.DeepEqual(res, want) {
				t.Errorf("results = %+v, want %+v", res, want)
			}
		})
	}
}

func TestNonJSONSuccessBodyIsInvalidResponse(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>login page</html>"))
	})
	_, err := c.GetPage(context.Background(), 1)
	apiErr := asAPIError(t, err, CodeInvalidResponse, 200)
	if apiErr.Message == "" {
		t.Error("empty message")
	}
}
