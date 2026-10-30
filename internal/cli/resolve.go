package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/JasserRomero/bookstack-cli/internal/client"
	"github.com/spf13/cobra"
)

const pagesResolveURLLong = `Print the page of a BookStack web URL, exactly like "pages get <id>"
<url> is a page URL, <base>/books/<book-slug>/page/<page-slug>, where <base>
is the configured url or resolve_url, including its path prefix. Its scheme,
host (in any case) and port must be those of that base.

The query string and fragment (e.g. #bkmrk-...) are ignored, and so are the
segments after the page slug (e.g. /edit, /revisions). Segments are
percent-decoded, so non-ASCII slugs work.

The page is looked up with one /api/pages?filter[slug]=<page-slug> request
and kept only when its book slug matches; then the requests of "pages get"
follow. --raw prints only the Markdown.

Besides the codes below: a URL that is not a page URL under url or
resolve_url is a usage error (exit 2, no request is made); no page with that
slug in that book is not_found (exit 4, without status); several pages with
that slug in that book is ambiguous (exit 1) and lists their ids.`

func newPagesResolveURLCmd(d apiDeps) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "resolve-url <url>",
		Short: "Print the page of a BookStack web URL",
		Long:  pagesResolveURLLong + exitCodesHelp,
		Example: `  bsk pages resolve-url https://docs.example.com/books/ops/page/deploy
  bsk pages resolve-url 'https://docs.example.com/books/ops/page/deploy#bkmrk-step-1' --raw`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, err := newAPIClient(*d.profile, d.version)
			if err != nil {
				return err
			}
			ref, err := parsePageURL(args[0], cfg.URL, cfg.ResolveURL)
			if err != nil {
				return err
			}
			id, err := findPage(cmd.Context(), c, ref)
			if err != nil {
				return err
			}
			v, err := getPage(cmd.Context(), c, id, raw)
			if err != nil {
				return err
			}
			return writeResult(cmd.OutOrStdout(), v)
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print only the Markdown, not JSON")
	return cmd
}

// pageRef is the book and page slugs of a page URL, percent-decoded.
type pageRef struct {
	book, page string
}

// parsePageURL extracts the slugs of raw, a page URL under one of bases.
func parsePageURL(raw string, bases ...string) (pageRef, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return pageRef{}, usageErrorf("invalid url %q: must be an absolute http or https URL", raw)
	}
	segs, err := pathSegments(u)
	if err != nil {
		return pageRef{}, usageErrorf("invalid url %q: must be an absolute http or https URL", raw)
	}
	var known []string
	under := false
	for _, b := range bases {
		if !contains(known, b) {
			known = append(known, b)
		}
		base, err := url.Parse(b)
		if err != nil || !sameOrigin(u, base) {
			continue
		}
		prefix, err := pathSegments(base)
		if err != nil || !hasPrefix(segs, prefix) {
			continue
		}
		under = true
		rest := segs[len(prefix):]
		if len(rest) >= 4 && rest[0] == "books" && rest[1] != "" && rest[2] == "page" && rest[3] != "" {
			return pageRef{book: rest[1], page: rest[3]}, nil
		}
	}
	if !under {
		return pageRef{}, usageErrorf("url %q is not under the configured url or resolve_url (%s)", raw, strings.Join(known, ", "))
	}
	return pageRef{}, usageErrorf("url %q is not a page URL: expected <base>/books/<book-slug>/page/<page-slug>", raw)
}

// sameOrigin reports whether a and b have the same scheme, host (in any
// case) and port, an omitted port being the scheme's default.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// pathSegments splits the path of u into percent-decoded segments; an empty
// path has none.
func pathSegments(u *url.URL) ([]string, error) {
	p := strings.TrimPrefix(u.EscapedPath(), "/")
	if p == "" {
		return nil, nil
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		dec, err := url.PathUnescape(s)
		if err != nil {
			return nil, err
		}
		segs[i] = dec
	}
	return segs, nil
}

func hasPrefix(segs, prefix []string) bool {
	if len(segs) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if segs[i] != p {
			return false
		}
	}
	return true
}

// findPage returns the id of the only page with ref's slug in ref's book,
// looked up with one filter[slug] list (slugs repeat across books).
func findPage(ctx context.Context, c *client.Client, ref pageRef) (int, error) {
	pages, err := c.ListPages(ctx, client.PageFilter{Slug: ref.page})
	if err != nil {
		return 0, err
	}
	var ids []string
	id := 0
	for _, p := range pages {
		if p.BookSlug == ref.book && p.Slug == ref.page {
			ids = append(ids, strconv.Itoa(p.ID))
			id = p.ID
		}
	}
	switch len(ids) {
	case 0:
		return 0, &Error{
			Code:    client.CodeNotFound,
			Message: fmt.Sprintf("page %q not found in book %q", ref.page, ref.book),
			Exit:    ExitNotFound,
		}
	case 1:
		return id, nil
	default:
		return 0, &Error{
			Code:    CodeAmbiguous,
			Message: fmt.Sprintf("page %q matches several pages in book %q: ids %s", ref.page, ref.book, strings.Join(ids, ", ")),
			Exit:    ExitError,
		}
	}
}
