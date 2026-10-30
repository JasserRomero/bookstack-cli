package client

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"sync"
	"testing"
)

// bodyLog keeps the method, path and body of every request received.
type bodyLog struct {
	mu   sync.Mutex
	reqs []string
}

func (b *bodyLog) add(r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reqs = append(b.reqs, r.Method+" "+r.URL.Path+" "+string(data))
}

func (b *bodyLog) got() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.reqs...)
}

func ptr[T any](v T) *T { return &v }

const writtenPage = `{"id":12,"book_id":7,"chapter_id":9,"name":"New","slug":"new","editor":"markdown","markdown":"body","html":"<p>body</p>","tags":[{"name":"env","value":"prod"}]}`

var writtenPageWant = &Page{ID: 12, BookID: 7, ChapterID: 9, Name: "New", Slug: "new", Editor: "markdown",
	Markdown: "body", HTML: "<p>body</p>", Tags: []Tag{{Name: "env", Value: "prod"}}}

// S9: CreatePage posts only the set fields to /api/pages.
func TestCreatePageBodies(t *testing.T) {
	cases := []struct {
		name string
		in   NewPage
		want string
	}{
		{"book markdown", NewPage{BookID: 7, Name: "New", Markdown: "body"},
			`POST /api/pages {"book_id":7,"name":"New","markdown":"body"}`},
		{"chapter html tags", NewPage{ChapterID: 9, Name: "New", HTML: "<p>x</p>", Tags: []Tag{{Name: "env", Value: "prod"}, {Name: "draft"}}},
			`POST /api/pages {"chapter_id":9,"name":"New","html":"<p>x</p>","tags":[{"name":"env","value":"prod"},{"name":"draft","value":""}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &bodyLog{}
			c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				log.add(r)
				writeJSON(w, 200, writtenPage)
			})
			got, err := c.CreatePage(context.Background(), tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, writtenPageWant) {
				t.Errorf("page = %+v", got)
			}
			if reqs := log.got(); !reflect.DeepEqual(reqs, []string{tc.want}) {
				t.Errorf("requests = %q\nwant       %q", reqs, []string{tc.want})
			}
		})
	}
}

// S9: UpdatePage puts only the set fields; nil tags are absent, an empty
// slice is sent as [] (clears the tags).
func TestUpdatePageBodies(t *testing.T) {
	cases := []struct {
		name string
		in   PageUpdate
		want string
	}{
		{"name only", PageUpdate{Name: ptr("Renamed")}, `PUT /api/pages/12 {"name":"Renamed"}`},
		{"markdown", PageUpdate{Markdown: ptr("# x")}, `PUT /api/pages/12 {"markdown":"# x"}`},
		{"html and name", PageUpdate{Name: ptr("N"), HTML: ptr("<p>x</p>")}, `PUT /api/pages/12 {"name":"N","html":"<p>x</p>"}`},
		{"empty markdown is sent", PageUpdate{Markdown: ptr("")}, `PUT /api/pages/12 {"markdown":""}`},
		{"tags replace", PageUpdate{Tags: &[]Tag{{Name: "k", Value: "v"}}}, `PUT /api/pages/12 {"tags":[{"name":"k","value":"v"}]}`},
		{"tags cleared", PageUpdate{Tags: &[]Tag{}}, `PUT /api/pages/12 {"tags":[]}`},
		{"nothing", PageUpdate{}, `PUT /api/pages/12 {}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &bodyLog{}
			c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				log.add(r)
				writeJSON(w, 200, writtenPage)
			})
			got, err := c.UpdatePage(context.Background(), 12, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, writtenPageWant) {
				t.Errorf("page = %+v", got)
			}
			if reqs := log.got(); !reflect.DeepEqual(reqs, []string{tc.want}) {
				t.Errorf("requests = %q\nwant       %q", reqs, []string{tc.want})
			}
		})
	}
}

// S9 (T3 S3): writes are not retried on 5xx; 429 is retried with the body resent.
func TestWritesRetryPolicy(t *testing.T) {
	ctx := context.Background()
	writes := map[string]func(c *Client) (*Page, error){
		"create": func(c *Client) (*Page, error) { return c.CreatePage(ctx, NewPage{BookID: 7, Name: "N", Markdown: "m"}) },
		"update": func(c *Client) (*Page, error) { return c.UpdatePage(ctx, 12, PageUpdate{Name: ptr("N")}) },
	}
	for name, write := range writes {
		t.Run(name+" 503 not retried", func(t *testing.T) {
			c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				writeJSON(w, 503, `{"error":{"message":"down"}}`)
			})
			_, err := write(c)
			asAPIError(t, err, CodeServer, 503)
			if got := len(rec.all()); got != 1 {
				t.Errorf("attempts = %d, want 1", got)
			}
			if got := sl.got(); len(got) != 0 {
				t.Errorf("waits = %v, want none", got)
			}
		})
		t.Run(name+" 429 retried with body", func(t *testing.T) {
			log := &bodyLog{}
			c, _, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				log.add(r)
				if n == 1 {
					w.Header().Set("Retry-After", "2")
					writeJSON(w, 429, `{}`)
					return
				}
				writeJSON(w, 200, writtenPage)
			})
			if _, err := write(c); err != nil {
				t.Fatal(err)
			}
			reqs := log.got()
			if len(reqs) != 2 || reqs[0] != reqs[1] {
				t.Errorf("requests = %q, want the same request twice", reqs)
			}
			if got := sl.got(); !equalDurations(got, durations(2)) {
				t.Errorf("waits = %v, want [2s]", got)
			}
		})
	}
}

// A 2xx write response that is not JSON is an invalid_response error.
func TestWriteInvalidResponse(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, `<html>login</html>`)
	})
	_, err := c.UpdatePage(context.Background(), 12, PageUpdate{Name: ptr("N")})
	apiErr := asAPIError(t, err, CodeInvalidResponse, 200)
	if want := "invalid JSON response from PUT /api/pages/12: invalid character '<' looking for beginning of value"; apiErr.Message != want {
		t.Errorf("Message = %q, want %q", apiErr.Message, want)
	}
}

const (
	writtenBook    = `{"id":7,"name":"Ops","slug":"ops","description":"Run <b>it</b>","description_html":"<p>Run &lt;b&gt;it&lt;/b&gt;</p>","tags":[{"name":"env","value":"prod"}]}`
	writtenChapter = `{"id":9,"book_id":7,"book_slug":"ops","name":"Runbooks","slug":"runbooks","description":"d","description_html":"<p>d</p>","priority":1}`
)

// T8 S5: CreateBook posts only the set fields to /api/books.
func TestCreateBookBodies(t *testing.T) {
	cases := []struct {
		name string
		in   NewBook
		want string
	}{
		{"name only", NewBook{Name: "Ops"}, `POST /api/books {"name":"Ops"}`},
		{"all fields", NewBook{Name: "Ops", Description: "Run <b>it</b>", Tags: []Tag{{Name: "env", Value: "prod"}, {Name: "draft"}}},
			`POST /api/books {"name":"Ops","description":"Run <b>it</b>","tags":[{"name":"env","value":"prod"},{"name":"draft","value":""}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &bodyLog{}
			c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				log.add(r)
				writeJSON(w, 200, writtenBook)
			})
			got, err := c.CreateBook(context.Background(), tc.in)
			if err != nil {
				t.Fatal(err)
			}
			want := &Book{ID: 7, Name: "Ops", Slug: "ops", Description: "Run <b>it</b>", Tags: []Tag{{Name: "env", Value: "prod"}}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("book = %+v", got)
			}
			if reqs := log.got(); !reflect.DeepEqual(reqs, []string{tc.want}) {
				t.Errorf("requests = %q\nwant       %q", reqs, []string{tc.want})
			}
		})
	}
}

// T8 S5: CreateChapter posts only the set fields to /api/chapters.
func TestCreateChapterBodies(t *testing.T) {
	cases := []struct {
		name string
		in   NewChapter
		want string
	}{
		{"name only", NewChapter{BookID: 7, Name: "Runbooks"}, `POST /api/chapters {"book_id":7,"name":"Runbooks"}`},
		{"all fields", NewChapter{BookID: 7, Name: "Runbooks", Description: "d", Tags: []Tag{{Name: "k", Value: "a=b"}}},
			`POST /api/chapters {"book_id":7,"name":"Runbooks","description":"d","tags":[{"name":"k","value":"a=b"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &bodyLog{}
			c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				log.add(r)
				writeJSON(w, 200, writtenChapter)
			})
			got, err := c.CreateChapter(context.Background(), tc.in)
			if err != nil {
				t.Fatal(err)
			}
			want := &Chapter{ID: 9, BookID: 7, BookSlug: "ops", Name: "Runbooks", Slug: "runbooks", Description: "d", Priority: 1}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("chapter = %+v", got)
			}
			if reqs := log.got(); !reflect.DeepEqual(reqs, []string{tc.want}) {
				t.Errorf("requests = %q\nwant       %q", reqs, []string{tc.want})
			}
		})
	}
}

// T8 S5 (T3 S3): book and chapter creates are not retried on 5xx.
func TestStructureCreatesNotRetried(t *testing.T) {
	ctx := context.Background()
	writes := map[string]func(c *Client) error{
		"book": func(c *Client) error { _, err := c.CreateBook(ctx, NewBook{Name: "N"}); return err },
		"chapter": func(c *Client) error {
			_, err := c.CreateChapter(ctx, NewChapter{BookID: 7, Name: "N"})
			return err
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			c, rec, sl := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				writeJSON(w, 503, `{"error":{"message":"down"}}`)
			})
			asAPIError(t, write(c), CodeServer, 503)
			if got := len(rec.all()); got != 1 {
				t.Errorf("attempts = %d, want 1", got)
			}
			if got := sl.got(); len(got) != 0 {
				t.Errorf("waits = %v, want none", got)
			}
		})
	}
}
