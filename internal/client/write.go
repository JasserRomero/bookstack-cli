package client

import (
	"context"
	"fmt"
	"net/http"
)

// NewPage is the body of CreatePage. Exactly one of BookID and ChapterID
// should be set, and one of HTML and Markdown: BookStack rejects a page
// without content. Zero values are not sent; nil Tags sets no tags.
type NewPage struct {
	BookID    int    `json:"book_id,omitempty"`
	ChapterID int    `json:"chapter_id,omitempty"`
	Name      string `json:"name"`
	HTML      string `json:"html,omitempty"`
	Markdown  string `json:"markdown,omitempty"`
	Tags      []Tag  `json:"tags,omitempty"`
}

// PageUpdate is the body of UpdatePage. Only non-nil fields are sent.
// Sending Markdown switches the page to the Markdown editor and HTML to the
// WYSIWYG editor. Tags replaces the whole tag set; a pointer to an empty
// slice clears it, nil leaves it unchanged.
type PageUpdate struct {
	Name     *string `json:"name,omitempty"`
	HTML     *string `json:"html,omitempty"`
	Markdown *string `json:"markdown,omitempty"`
	Tags     *[]Tag  `json:"tags,omitempty"`
}

// CreatePage creates a page (POST /api/pages) and returns it. Like every
// write it is retried only on 429, never on 5xx or network errors.
func (c *Client) CreatePage(ctx context.Context, p NewPage) (*Page, error) {
	var out Page
	if err := c.sendJSON(ctx, http.MethodPost, "pages", nil, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePage updates a page (PUT /api/pages/<id>) and returns it. Like every
// write it is retried only on 429, never on 5xx or network errors.
func (c *Client) UpdatePage(ctx context.Context, id int, u PageUpdate) (*Page, error) {
	var out Page
	if err := c.sendJSON(ctx, http.MethodPut, fmt.Sprintf("pages/%d", id), nil, u, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// NewBook is the body of CreateBook. Description is plain text; BookStack
// escapes it into description_html. Zero values are not sent; nil Tags sets
// no tags.
type NewBook struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tags        []Tag  `json:"tags,omitempty"`
}

// NewChapter is the body of CreateChapter, like NewBook plus the book the
// chapter is created in.
type NewChapter struct {
	BookID      int    `json:"book_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tags        []Tag  `json:"tags,omitempty"`
}

// CreateBook creates a book (POST /api/books) and returns it. Like every
// write it is retried only on 429, never on 5xx or network errors.
func (c *Client) CreateBook(ctx context.Context, b NewBook) (*Book, error) {
	var out Book
	if err := c.sendJSON(ctx, http.MethodPost, "books", nil, b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateChapter creates a chapter (POST /api/chapters) and returns it. Like
// every write it is retried only on 429, never on 5xx or network errors.
func (c *Client) CreateChapter(ctx context.Context, ch NewChapter) (*Chapter, error) {
	var out Chapter
	if err := c.sendJSON(ctx, http.MethodPost, "chapters", nil, ch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
