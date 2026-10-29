package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Filters are typed per endpoint because BookStack silently ignores unknown
// filter fields and returns the full list. Zero values are not sent.

// PageFilter filters ListPages.
type PageFilter struct {
	BookID    int
	ChapterID int
	Slug      string
}

// ChapterFilter filters ListChapters.
type ChapterFilter struct {
	BookID int
	Slug   string
}

// BookFilter filters ListBooks.
type BookFilter struct {
	Slug string
}

// ShelfFilter filters ListShelves.
type ShelfFilter struct {
	Slug string
}

// SearchOptions configures Search. Type (page, chapter, book or bookshelf)
// is appended to the query as {type:<Type>}; Count is sent when positive.
type SearchOptions struct {
	Query string
	Type  string
	Count int
}

func (f PageFilter) values() url.Values {
	q := url.Values{}
	setInt(q, "book_id", f.BookID)
	setInt(q, "chapter_id", f.ChapterID)
	setString(q, "slug", f.Slug)
	return q
}

func (f ChapterFilter) values() url.Values {
	q := url.Values{}
	setInt(q, "book_id", f.BookID)
	setString(q, "slug", f.Slug)
	return q
}

func (f BookFilter) values() url.Values {
	q := url.Values{}
	setString(q, "slug", f.Slug)
	return q
}

func (f ShelfFilter) values() url.Values {
	q := url.Values{}
	setString(q, "slug", f.Slug)
	return q
}

func setInt(q url.Values, field string, v int) {
	if v != 0 {
		q.Set("filter["+field+"]", strconv.Itoa(v))
	}
}

func setString(q url.Values, field, v string) {
	if v != "" {
		q.Set("filter["+field+"]", v)
	}
}

// listAll pages through a list endpoint with count/offset until offset
// reaches total or a page comes back empty. per_page is ignored (it may be null).
func listAll[T any](ctx context.Context, c *Client, path string, filters url.Values) ([]T, error) {
	all := []T{}
	for offset := 0; ; {
		q := url.Values{}
		for k, v := range filters {
			q[k] = v
		}
		q.Set("count", strconv.Itoa(c.pageSize))
		q.Set("offset", strconv.Itoa(offset))
		var page struct {
			Data  []T `json:"data"`
			Total int `json:"total"`
		}
		if err := c.getJSON(ctx, path, q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		offset += len(page.Data)
		if len(page.Data) == 0 || offset >= page.Total {
			return all, nil
		}
	}
}

func getOne[T any](ctx context.Context, c *Client, path string, q url.Values) (*T, error) {
	var v T
	if err := c.getJSON(ctx, path, q, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// System returns the instance version and name (GET /api/system).
func (c *Client) System(ctx context.Context) (*System, error) {
	return getOne[System](ctx, c, "system", nil)
}

// ListShelves returns every shelf matching f.
func (c *Client) ListShelves(ctx context.Context, f ShelfFilter) ([]Shelf, error) {
	return listAll[Shelf](ctx, c, "shelves", f.values())
}

// GetShelf returns a shelf with its books.
func (c *Client) GetShelf(ctx context.Context, id int) (*Shelf, error) {
	return getOne[Shelf](ctx, c, fmt.Sprintf("shelves/%d", id), nil)
}

// ListBooks returns every book matching f.
func (c *Client) ListBooks(ctx context.Context, f BookFilter) ([]Book, error) {
	return listAll[Book](ctx, c, "books", f.values())
}

// GetBook returns a book with its contents.
func (c *Client) GetBook(ctx context.Context, id int) (*Book, error) {
	return getOne[Book](ctx, c, fmt.Sprintf("books/%d", id), nil)
}

// ListChapters returns every chapter matching f.
func (c *Client) ListChapters(ctx context.Context, f ChapterFilter) ([]Chapter, error) {
	return listAll[Chapter](ctx, c, "chapters", f.values())
}

// GetChapter returns a chapter with its pages.
func (c *Client) GetChapter(ctx context.Context, id int) (*Chapter, error) {
	return getOne[Chapter](ctx, c, fmt.Sprintf("chapters/%d", id), nil)
}

// ListPages returns every page matching f.
func (c *Client) ListPages(ctx context.Context, f PageFilter) ([]Page, error) {
	return listAll[Page](ctx, c, "pages", f.values())
}

// GetPage returns a page with its HTML, Markdown source and tags.
func (c *Client) GetPage(ctx context.Context, id int) (*Page, error) {
	return getOne[Page](ctx, c, fmt.Sprintf("pages/%d", id), nil)
}

// ExportPageMarkdown returns the page rendered as Markdown. It works for
// WYSIWYG pages, whose Markdown source is empty.
func (c *Client) ExportPageMarkdown(ctx context.Context, id int) (string, error) {
	res, err := c.do(ctx, http.MethodGet, fmt.Sprintf("pages/%d/export/markdown", id), nil, nil)
	if err != nil {
		return "", err
	}
	return string(res.body), nil
}

// Search runs a BookStack search query.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*SearchResults, error) {
	query := opts.Query
	if opts.Type != "" {
		query += " {type:" + opts.Type + "}"
	}
	q := url.Values{"query": {query}}
	if opts.Count > 0 {
		q.Set("count", strconv.Itoa(opts.Count))
	}
	return getOne[SearchResults](ctx, c, "search", q)
}
