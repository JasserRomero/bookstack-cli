package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// pagedHandler serves total items with ids 1..total honoring count/offset.
func pagedHandler(total int) func(w http.ResponseWriter, r *http.Request, n int) {
	return func(w http.ResponseWriter, r *http.Request, n int) {
		q := r.URL.Query()
		count, _ := strconv.Atoi(q.Get("count"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		var items []string
		for id := offset + 1; id <= total && id <= offset+count; id++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"item %d","created_by":1}`, id, id))
		}
		writeJSON(w, 200, `{"data":[`+strings.Join(items, ",")+`],"total":`+strconv.Itoa(total)+`,"per_page":null}`)
	}
}

func TestPaginationIteratesWithCountAndOffset(t *testing.T) {
	c, rec, _ := newTestClient(t, pagedHandler(5))
	c.pageSize = 2
	pages, err := c.ListPages(context.Background(), PageFilter{})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	var ids []int
	for _, p := range pages {
		ids = append(ids, p.ID)
	}
	if !reflect.DeepEqual(ids, []int{1, 2, 3, 4, 5}) {
		t.Errorf("ids = %v", ids)
	}
	reqs := rec.all()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	for i, r := range reqs {
		want := url.Values{"count": {"2"}, "offset": {strconv.Itoa(2 * i)}}
		if r.Method != http.MethodGet || r.URL.Path != "/api/pages" || !reflect.DeepEqual(r.URL.Query(), want) {
			t.Errorf("request %d = %s %s?%s, want GET /api/pages?%s", i, r.Method, r.URL.Path, r.URL.RawQuery, want.Encode())
		}
	}
}

func TestPaginationDefaultPageSizeIs500(t *testing.T) {
	c, rec, _ := newTestClient(t, pagedHandler(3))
	books, err := c.ListBooks(context.Background(), BookFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 3 {
		t.Errorf("books = %d, want 3", len(books))
	}
	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if got := reqs[0].URL.Query(); got.Get("count") != "500" || got.Get("offset") != "0" || got.Has("page") {
		t.Errorf("query = %v, want count=500 offset=0 and no page", got)
	}
}

func TestPaginationStopsOnEmptyPage(t *testing.T) {
	// The server claims more items than it returns; an empty page ends iteration.
	c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.URL.Query().Get("offset") == "0" {
			writeJSON(w, 200, `{"data":[{"id":1},{"id":2}],"total":10,"per_page":null}`)
			return
		}
		writeJSON(w, 200, `{"data":[],"total":10,"per_page":null}`)
	})
	c.pageSize = 2
	chapters, err := c.ListChapters(context.Background(), ChapterFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 2 {
		t.Errorf("chapters = %d, want 2", len(chapters))
	}
	if got := len(rec.all()); got != 2 {
		t.Errorf("requests = %d, want 2", got)
	}
}

func TestPaginationErrorMidwayIsReturned(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			writeJSON(w, 200, `{"data":[{"id":1}],"total":3}`)
			return
		}
		writeJSON(w, 403, `{"error":{"message":"denied"}}`)
	})
	c.pageSize = 1
	shelves, err := c.ListShelves(context.Background(), ShelfFilter{})
	asAPIError(t, err, CodeAuth, 403)
	if shelves != nil {
		t.Errorf("shelves = %v, want nil on error", shelves)
	}
}

func TestTypedFiltersProduceExactParams(t *testing.T) {
	type call struct {
		name string
		path string
		run  func(c *Client) error
		want url.Values
	}
	ctx := context.Background()
	calls := []call{
		{"pages all filters", "/api/pages", func(c *Client) error {
			_, err := c.ListPages(ctx, PageFilter{BookID: 62, ChapterID: 7, Slug: "marco-del-plan"})
			return err
		}, url.Values{"filter[book_id]": {"62"}, "filter[chapter_id]": {"7"}, "filter[slug]": {"marco-del-plan"}}},
		{"pages book only", "/api/pages", func(c *Client) error {
			_, err := c.ListPages(ctx, PageFilter{BookID: 62})
			return err
		}, url.Values{"filter[book_id]": {"62"}}},
		{"chapters", "/api/chapters", func(c *Client) error {
			_, err := c.ListChapters(ctx, ChapterFilter{BookID: 3, Slug: "intro"})
			return err
		}, url.Values{"filter[book_id]": {"3"}, "filter[slug]": {"intro"}}},
		{"books", "/api/books", func(c *Client) error {
			_, err := c.ListBooks(ctx, BookFilter{Slug: "manual"})
			return err
		}, url.Values{"filter[slug]": {"manual"}}},
		{"shelves", "/api/shelves", func(c *Client) error {
			_, err := c.ListShelves(ctx, ShelfFilter{Slug: "área"})
			return err
		}, url.Values{"filter[slug]": {"área"}}},
		{"no filters", "/api/shelves", func(c *Client) error {
			_, err := c.ListShelves(ctx, ShelfFilter{})
			return err
		}, url.Values{}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			c, rec, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request, n int) {
				writeJSON(w, 200, `{"data":[],"total":0}`)
			})
			if err := tc.run(c); err != nil {
				t.Fatal(err)
			}
			reqs := rec.all()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d", len(reqs))
			}
			got := reqs[0].URL.Query()
			if reqs[0].URL.Path != tc.path {
				t.Errorf("path = %q, want %q", reqs[0].URL.Path, tc.path)
			}
			want := url.Values{"count": {"500"}, "offset": {"0"}}
			for k, v := range tc.want {
				want[k] = v
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("query = %v, want %v", got, want)
			}
		})
	}
}
