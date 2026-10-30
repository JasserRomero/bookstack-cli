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
