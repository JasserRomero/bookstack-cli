package cli

import (
	"context"
	"io"
	"strings"

	"github.com/JasserRomero/bookstack-cli/internal/client"
	"github.com/spf13/cobra"
)

// apiDeps is what every API command needs to build its client: the
// --profile value and the build version sent in the User-Agent.
type apiDeps struct {
	profile *string
	version string
}

func (d apiDeps) client() (*client.Client, error) {
	c, _, err := newAPIClient(*d.profile, d.version)
	return c, err
}

func newGroupCmd(use, short string, subs ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(subs...)
	return cmd
}

type testOutput struct {
	OK      bool    `json:"ok"`
	Profile *string `json:"profile"`
	URL     string  `json:"url"`
	TokenID string  `json:"token_id"`
	Version string  `json:"version"`
	AppName string  `json:"app_name"`
}

func newTestCmd(d apiDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Check the URL, the token and its API permission",
		Long: `Check the effective configuration against BookStack by calling /api/system,
which needs a valid token whose role has API access. Prints the profile (null
when only environment variables are used), url, token_id and the instance
version and app_name. The token secret is never printed.` + exitCodesHelp,
		Example: `  bsk test
  bsk --profile client test`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, cfg, err := newAPIClient(*d.profile, d.version)
			if err != nil {
				return err
			}
			sys, err := c.System(cmd.Context())
			if err != nil {
				return err
			}
			out := testOutput{OK: true, URL: cfg.URL, TokenID: cfg.TokenID, Version: sys.Version, AppName: sys.AppName}
			if cfg.Profile != "" {
				out.Profile = &cfg.Profile
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
}

var searchTypes = []string{"page", "chapter", "book", "bookshelf"}

type searchOutput struct {
	Total int                   `json:"total"`
	Data  []client.SearchResult `json:"data"`
}

func newSearchCmd(d apiDeps) *cobra.Command {
	var typ string
	var count int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search pages, chapters, books and shelves",
		Long: `Run a BookStack search and print {"total":N,"data":[...]}. The query uses
BookStack's search syntax; --type appends {type:<type>} to it, so
"bsk search deploy --type page" searches "deploy {type:page}".

--type is one of page, chapter, book, bookshelf. --count is the number of
results, 1-100 (default 20).` + exitCodesHelp,
		Example: `  bsk search deploy
  bsk search "backup policy" --type page --count 5
  bsk search '[env=prod] {updated_by:me}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			if strings.TrimSpace(query) == "" {
				return usageErrorf("empty search query")
			}
			if typ != "" && !contains(searchTypes, typ) {
				return usageErrorf("invalid --type %q: must be one of %s", typ, strings.Join(searchTypes, ", "))
			}
			if count < 1 || count > 100 {
				return usageErrorf("invalid --count %d: must be between 1 and 100", count)
			}
			c, err := d.client()
			if err != nil {
				return err
			}
			res, err := c.Search(cmd.Context(), client.SearchOptions{Query: query, Type: typ, Count: count})
			if err != nil {
				return err
			}
			out := searchOutput{Total: res.Total, Data: res.Data}
			if out.Data == nil {
				out.Data = []client.SearchResult{}
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "restrict results to one type: page, chapter, book or bookshelf")
	cmd.Flags().IntVar(&count, "count", 20, "number of results, 1-100")
	return cmd
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

const listLong = ` as a JSON array, following
every page of results.`

// newListCmd builds "<group> list"; run fetches every item.
func newListCmd(noun string, d apiDeps, run func(cmd *cobra.Command, c *client.Client) (any, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every " + noun,
		Long:  "Print every " + noun + " visible to the token" + listLong + exitCodesHelp,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := d.client()
			if err != nil {
				return err
			}
			v, err := run(cmd, c)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), v)
		},
	}
}

// newGetCmd builds "<group> get <id>"; run fetches the item.
func newGetCmd(long, example string, d apiDeps, run func(cmd *cobra.Command, c *client.Client, id int) (any, error)) *cobra.Command {
	return &cobra.Command{
		Use:     "get <id>",
		Short:   strings.SplitN(long, "\n", 2)[0],
		Long:    long + exitCodesHelp,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			c, err := d.client()
			if err != nil {
				return err
			}
			v, err := run(cmd, c, id)
			if err != nil {
				return err
			}
			return writeResult(cmd.OutOrStdout(), v)
		},
	}
}

// shelfOutput always reports books, even when the shelf is empty.
type shelfOutput struct {
	client.Shelf
	Books []client.Book `json:"books"`
}

func newShelvesCmd(d apiDeps) *cobra.Command {
	list := newListCmd("shelf", d, func(cmd *cobra.Command, c *client.Client) (any, error) {
		return c.ListShelves(cmd.Context(), client.ShelfFilter{})
	})
	list.Example = "  bsk shelves list"
	get := newGetCmd(`Print one shelf with its books
<id> is the shelf id, a positive integer.`, "  bsk shelves get 3", d,
		func(cmd *cobra.Command, c *client.Client, id int) (any, error) {
			s, err := c.GetShelf(cmd.Context(), id)
			if err != nil {
				return nil, err
			}
			out := shelfOutput{Shelf: *s, Books: s.Books}
			if out.Books == nil {
				out.Books = []client.Book{}
			}
			return out, nil
		})
	return newGroupCmd("shelves", "List and read shelves", list, get)
}

// bookOutput always reports contents, even when the book is empty.
type bookOutput struct {
	client.Book
	Contents []client.BookContent `json:"contents"`
}

func newBooksCmd(d apiDeps) *cobra.Command {
	list := newListCmd("book", d, func(cmd *cobra.Command, c *client.Client) (any, error) {
		return c.ListBooks(cmd.Context(), client.BookFilter{})
	})
	list.Example = "  bsk books list"
	get := newGetCmd(`Print one book with its contents
<id> is the book id, a positive integer. contents lists the chapters (each
with its pages) and the pages outside chapters.`, "  bsk books get 7", d,
		func(cmd *cobra.Command, c *client.Client, id int) (any, error) {
			b, err := c.GetBook(cmd.Context(), id)
			if err != nil {
				return nil, err
			}
			out := bookOutput{Book: *b, Contents: b.Contents}
			if out.Contents == nil {
				out.Contents = []client.BookContent{}
			}
			return out, nil
		})
	return newGroupCmd("books", "List and read books", list, get)
}

// chapterOutput always reports pages, even when the chapter is empty.
type chapterOutput struct {
	client.Chapter
	Pages []client.Page `json:"pages"`
}

func newChaptersCmd(d apiDeps) *cobra.Command {
	var book idFlag
	list := newListCmd("chapter", d, func(cmd *cobra.Command, c *client.Client) (any, error) {
		return c.ListChapters(cmd.Context(), client.ChapterFilter{BookID: int(book)})
	})
	list.Long = "Print every chapter visible to the token, or only those of --book," + listLong + `
The filter is applied server-side.` + exitCodesHelp
	list.Example = `  bsk chapters list
  bsk chapters list --book 7`
	list.Flags().Var(&book, "book", "only chapters of this book id")
	get := newGetCmd(`Print one chapter with its pages
<id> is the chapter id, a positive integer.`, "  bsk chapters get 9", d,
		func(cmd *cobra.Command, c *client.Client, id int) (any, error) {
			ch, err := c.GetChapter(cmd.Context(), id)
			if err != nil {
				return nil, err
			}
			out := chapterOutput{Chapter: *ch, Pages: ch.Pages}
			if out.Pages == nil {
				out.Pages = []client.Page{}
			}
			return out, nil
		})
	return newGroupCmd("chapters", "List and read chapters", list, get)
}

// pageOutput is a page with its Markdown export instead of its HTML.
type pageOutput struct {
	client.Page
	Markdown string `json:"markdown"`
}

const pagesGetLong = `Print one page with its content as Markdown
<id> is the page id, a positive integer.

The JSON object has the page metadata (id, book_id, chapter_id, name, slug,
editor, draft, revision_count, created_at, updated_at, created_by, updated_by,
owned_by, tags, ...) and markdown, the page exported through
/api/pages/<id>/export/markdown, which also works for WYSIWYG pages. html and
raw_html are omitted. updated_at is the server value, unchanged.

With --raw only the Markdown is printed, exactly as exported, with no added
trailing newline.`

func newPagesCmd(d apiDeps) *cobra.Command {
	var book, chapter idFlag
	list := newListCmd("page", d, func(cmd *cobra.Command, c *client.Client) (any, error) {
		return c.ListPages(cmd.Context(), client.PageFilter{BookID: int(book), ChapterID: int(chapter)})
	})
	list.Long = "Print every page visible to the token, or only those of --book and/or\n--chapter," + listLong + `
The filters are applied server-side; pages are never downloaded to be
filtered locally. The list has no content: use "pages get".` + exitCodesHelp
	list.Example = `  bsk pages list --book 7
  bsk pages list --chapter 9`
	list.Flags().Var(&book, "book", "only pages of this book id")
	list.Flags().Var(&chapter, "chapter", "only pages of this chapter id")

	var raw bool
	get := newGetCmd(pagesGetLong, `  bsk pages get 5
  bsk pages get 5 --raw > page.md`, d,
		func(cmd *cobra.Command, c *client.Client, id int) (any, error) {
			return getPage(cmd.Context(), c, id, raw)
		})
	get.Flags().BoolVar(&raw, "raw", false, "print only the Markdown, not JSON")
	return newGroupCmd("pages", "List and read pages", list, get, newPagesResolveURLCmd(d))
}

// getPage is the result of "pages get <id>": the page with its Markdown
// export, or with raw only the Markdown.
func getPage(ctx context.Context, c *client.Client, id int, raw bool) (any, error) {
	if raw {
		md, err := c.ExportPageMarkdown(ctx, id)
		if err != nil {
			return nil, err
		}
		return rawOutput(md), nil
	}
	p, err := c.GetPage(ctx, id)
	if err != nil {
		return nil, err
	}
	md, err := c.ExportPageMarkdown(ctx, id)
	if err != nil {
		return nil, err
	}
	p.HTML, p.RawHTML = "", ""
	return pageOutput{Page: *p, Markdown: md}, nil
}

// rawOutput is a command result written verbatim instead of as JSON.
type rawOutput string

func writeResult(w io.Writer, v any) error {
	if r, ok := v.(rawOutput); ok {
		_, err := io.WriteString(w, string(r))
		return err
	}
	return writeJSON(w, v)
}
