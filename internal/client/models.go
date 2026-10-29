package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// UserRef is a user reference. List endpoints return it as a plain id while
// single-item endpoints return an object {id,name,slug}; both decode here.
type UserRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// UnmarshalJSON accepts an integer id, an object, or null.
func (u *UserRef) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*u = UserRef{}
		return nil
	case len(b) > 0 && b[0] == '{':
		type plain UserRef
		var p plain
		if err := json.Unmarshal(b, &p); err != nil {
			return err
		}
		*u = UserRef(p)
		return nil
	}
	id, err := strconv.Atoi(string(b))
	if err != nil {
		return fmt.Errorf("user reference: want an id or an object, got %s", b)
	}
	*u = UserRef{ID: id}
	return nil
}

// MarshalJSON keeps the server shape: a bare id when only the id is known.
func (u UserRef) MarshalJSON() ([]byte, error) {
	if u.Name == "" && u.Slug == "" {
		return []byte(strconv.Itoa(u.ID)), nil
	}
	type plain UserRef
	return json.Marshal(plain(u))
}

// Tag is a name/value tag attached to an item.
type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// System is the response of GET /api/system.
type System struct {
	Version    string `json:"version"`
	InstanceID string `json:"instance_id"`
	AppName    string `json:"app_name"`
}

// Shelf is a bookshelf. Books is only filled by GetShelf.
type Shelf struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
	CreatedBy   UserRef `json:"created_by"`
	UpdatedBy   UserRef `json:"updated_by"`
	OwnedBy     UserRef `json:"owned_by"`
	Tags        []Tag   `json:"tags,omitempty"`
	Books       []Book  `json:"books,omitempty"`
}

// Book is a book. Contents is only filled by GetBook.
type Book struct {
	ID          int           `json:"id"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Description string        `json:"description,omitempty"`
	CreatedAt   string        `json:"created_at,omitempty"`
	UpdatedAt   string        `json:"updated_at,omitempty"`
	CreatedBy   UserRef       `json:"created_by"`
	UpdatedBy   UserRef       `json:"updated_by"`
	OwnedBy     UserRef       `json:"owned_by"`
	Tags        []Tag         `json:"tags,omitempty"`
	Contents    []BookContent `json:"contents,omitempty"`
}

// BookContent is an entry of a book's contents: a chapter (with its pages)
// or a top-level page.
type BookContent struct {
	ID        int           `json:"id"`
	Name      string        `json:"name"`
	Slug      string        `json:"slug"`
	Type      string        `json:"type,omitempty"`
	BookID    int           `json:"book_id"`
	ChapterID int           `json:"chapter_id,omitempty"`
	Priority  int           `json:"priority"`
	URL       string        `json:"url,omitempty"`
	Draft     bool          `json:"draft,omitempty"`
	Template  bool          `json:"template,omitempty"`
	CreatedAt string        `json:"created_at,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
	Pages     []BookContent `json:"pages,omitempty"`
}

// Chapter is a chapter. Pages is only filled by GetChapter.
type Chapter struct {
	ID          int     `json:"id"`
	BookID      int     `json:"book_id"`
	BookSlug    string  `json:"book_slug,omitempty"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description,omitempty"`
	Priority    int     `json:"priority"`
	CreatedAt   string  `json:"created_at,omitempty"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
	CreatedBy   UserRef `json:"created_by"`
	UpdatedBy   UserRef `json:"updated_by"`
	OwnedBy     UserRef `json:"owned_by"`
	Tags        []Tag   `json:"tags,omitempty"`
	Pages       []Page  `json:"pages,omitempty"`
}

// Page is a page. HTML, RawHTML, Markdown and Tags are only filled by
// GetPage; Markdown is empty for pages edited with the WYSIWYG editor.
type Page struct {
	ID            int     `json:"id"`
	BookID        int     `json:"book_id"`
	BookSlug      string  `json:"book_slug,omitempty"`
	ChapterID     int     `json:"chapter_id"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	Editor        string  `json:"editor,omitempty"`
	Priority      int     `json:"priority"`
	Draft         bool    `json:"draft"`
	Template      bool    `json:"template"`
	RevisionCount int     `json:"revision_count"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	CreatedBy     UserRef `json:"created_by"`
	UpdatedBy     UserRef `json:"updated_by"`
	OwnedBy       UserRef `json:"owned_by"`
	HTML          string  `json:"html,omitempty"`
	RawHTML       string  `json:"raw_html,omitempty"`
	Markdown      string  `json:"markdown,omitempty"`
	Tags          []Tag   `json:"tags,omitempty"`
}

// PreviewHTML is the highlighted preview of a search result.
type PreviewHTML struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SearchResult is one item of GET /api/search. Type is page, chapter, book
// or bookshelf; BookID and ChapterID are set only where they apply.
type SearchResult struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	Type        string      `json:"type"`
	URL         string      `json:"url"`
	BookID      int         `json:"book_id,omitempty"`
	ChapterID   int         `json:"chapter_id,omitempty"`
	PreviewHTML PreviewHTML `json:"preview_html"`
	Tags        []Tag       `json:"tags"`
}

// SearchResults is the response of GET /api/search.
type SearchResults struct {
	Data  []SearchResult `json:"data"`
	Total int            `json:"total"`
}
