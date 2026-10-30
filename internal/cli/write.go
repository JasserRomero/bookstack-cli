package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/JasserRomero/bookstack-cli/internal/client"
	"github.com/spf13/cobra"
)

// tagsFlag is a repeatable --tag key[=value] flag.
type tagsFlag []client.Tag

func (f *tagsFlag) String() string {
	parts := make([]string, len(*f))
	for i, t := range *f {
		parts[i] = t.Name + "=" + t.Value
	}
	return strings.Join(parts, ",")
}

func (f *tagsFlag) Set(s string) error {
	name, value, _ := strings.Cut(s, "=")
	if strings.TrimSpace(name) == "" {
		return errors.New("key must not be empty")
	}
	*f = append(*f, client.Tag{Name: name, Value: value})
	return nil
}

func (f *tagsFlag) Type() string { return "key[=value]" }

const tagFlagUsage = "tag as key=value or key, repeatable"

// writeHelp is the part of the create and update help shared by both.
const writeHelp = `

Content is Markdown read from a file, or from stdin with "-"; it must be valid
UTF-8, at most 10 MiB and not blank. Only one flag can read stdin.

--tag is repeatable, key=value or key (empty value); the key must not be
empty and the value may contain "=".

Prints the resulting page metadata as JSON: the same fields as "pages get"
without markdown, html and raw_html.`

const pagesCreateLong = `Create a page in a book or a chapter
Exactly one of --book and --chapter is required, and --name and --file:
BookStack rejects a page without content. The page uses the Markdown editor
and the content is sent as given: no title heading is added. --tag sets the
tags of the new page.` + writeHelp

const pagesUpdateLong = `Update a page's name, content or tags
<id> is the page id, a positive integer. Only the given fields are sent;
at least one of --name, --file, --append-file, --tag is required.

--file replaces the content and --append-file appends to it; they are
mutually exclusive. The input is Markdown, stored in the page's current
editor format so the editor never changes:
  markdown editor  the Markdown is sent as is; --append-file adds it after the
                   existing Markdown (trailing newlines trimmed) and one blank
                   line, or alone when the page is empty
  WYSIWYG editor   the Markdown is converted to HTML (GFM: tables,
                   strikethrough, task lists, autolinks; raw HTML is kept);
                   --append-file adds it after the existing raw_html, which
                   keeps includes such as {{@3}} unexpanded
Writing content first reads the page (GET /api/pages/<id>) to know its editor.

--tag replaces the page's whole tag set with the given tags; without --tag the
tags are not sent and stay unchanged.

--if-unmodified-since is an optional guard against overwriting someone else's
edit, for any change. The value is an RFC 3339 timestamp, normally the
updated_at printed by "pages get"; it is compared as an instant, so
2026-10-20T08:30:15.000000Z equals 2026-10-20T08:30:15Z. The page is read
first (one GET, the same read --append-file appends to); when its updated_at
is a different instant nothing is written and the command fails with code
conflict (exit 5), naming the current updated_at and who made the change.
Recommended pattern: "pages get <id>", keep its updated_at, then
"pages update <id> --if-unmodified-since <updated_at> ...".
The check is not atomic: BookStack offers no optimistic locking, so a change
made between the read and the write is not detected. Page revisions in
BookStack remain the way to recover an overwritten edit.` + writeHelp

func newPagesCreateCmd(d apiDeps) *cobra.Command {
	var (
		book, chapter idFlag
		name, file    string
		tags          tagsFlag
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: strings.SplitN(pagesCreateLong, "\n", 2)[0],
		Long:  pagesCreateLong + exitCodesHelp,
		Example: `  bsk pages create --book 7 --name "Deploy" --file deploy.md
  generate-doc | bsk pages create --chapter 9 --name "Report" --file - --tag env=prod --tag draft`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (book == 0) == (chapter == 0) {
				return usageErrorf("exactly one of --book or --chapter is required")
			}
			if !cmd.Flags().Changed("name") {
				return usageErrorf("--name is required")
			}
			if err := checkName(name); err != nil {
				return err
			}
			if file == "" {
				return usageErrorf("--file is required: BookStack rejects a page without content")
			}
			md, err := readContent("--file", file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			c, err := d.client()
			if err != nil {
				return err
			}
			p, err := c.CreatePage(cmd.Context(), client.NewPage{
				BookID: int(book), ChapterID: int(chapter), Name: name, Markdown: md, Tags: tags,
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), pageMetadata(p))
		},
	}
	cmd.Flags().Var(&book, "book", "book id to create the page in")
	cmd.Flags().Var(&chapter, "chapter", "chapter id to create the page in")
	cmd.Flags().StringVar(&name, "name", "", "page name (required)")
	cmd.Flags().StringVar(&file, "file", "", `Markdown content file, or "-" for stdin (required)`)
	cmd.Flags().Var(&tags, "tag", tagFlagUsage)
	return cmd
}

func newPagesUpdateCmd(d apiDeps) *cobra.Command {
	var (
		name, file, appendFile, since string
		tags                          tagsFlag
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: strings.SplitN(pagesUpdateLong, "\n", 2)[0],
		Long:  pagesUpdateLong + exitCodesHelp,
		Example: `  bsk pages update 5 --file deploy.md
  bsk pages update 5 --name "Deploy (v2)" --tag env=prod --tag reviewed
  printf '## Changelog\n\n- fixed\n' | bsk pages update 5 --append-file -
  bsk pages get 5    # note its updated_at, e.g. 2026-10-20T08:30:15.000000Z
  bsk pages update 5 --if-unmodified-since 2026-10-20T08:30:15.000000Z --file deploy.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			flags := cmd.Flags()
			setName, setTags := flags.Changed("name"), flags.Changed("tag")
			if !setName && !setTags && file == "" && appendFile == "" {
				return usageErrorf("nothing to update: give at least one of --name, --file, --append-file, --tag")
			}
			guard := flags.Changed("if-unmodified-since")
			var expected time.Time
			if guard {
				if expected, err = time.Parse(time.RFC3339Nano, since); err != nil {
					return usageErrorf(`invalid --if-unmodified-since %q: must be an RFC 3339 timestamp such as the updated_at printed by "pages get"`, since)
				}
			}
			var u client.PageUpdate
			if setName {
				if err := checkName(name); err != nil {
					return err
				}
				u.Name = &name
			}
			if setTags {
				t := []client.Tag(tags)
				u.Tags = &t
			}
			if file == "-" && appendFile == "-" {
				return usageErrorf(`stdin ("-") can be read by only one flag; --file and --append-file both use it`)
			}
			if file != "" && appendFile != "" {
				return usageErrorf("--file and --append-file are mutually exclusive")
			}
			mdFlag, mdPath := "--file", file
			if appendFile != "" {
				mdFlag, mdPath = "--append-file", appendFile
			}
			var md string
			if mdPath != "" {
				if md, err = readContent(mdFlag, mdPath, cmd.InOrStdin()); err != nil {
					return err
				}
			}
			c, err := d.client()
			if err != nil {
				return err
			}
			// One read serves both the guard and the content, so the checked
			// version is the one appended to.
			if guard || mdPath != "" {
				current, err := c.GetPage(cmd.Context(), id)
				if err != nil {
					return err
				}
				if guard {
					if err := checkUnmodified(id, current, expected, since); err != nil {
						return err
					}
				}
				if mdPath != "" {
					if err := setContent(id, current, md, mdFlag == "--append-file", &u); err != nil {
						return err
					}
				}
			}
			p, err := c.UpdatePage(cmd.Context(), id, u)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), pageMetadata(p))
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new page name")
	cmd.Flags().StringVar(&file, "file", "", `replace the content with this Markdown file, or "-" for stdin`)
	cmd.Flags().StringVar(&appendFile, "append-file", "", `append this Markdown file, or "-" for stdin`)
	cmd.Flags().Var(&tags, "tag", tagFlagUsage+"; replaces the page's tags")
	cmd.Flags().StringVar(&since, "if-unmodified-since", "", "write only if the page's updated_at is still this RFC 3339 time (not atomic)")
	return cmd
}

func checkName(name string) error {
	if strings.TrimSpace(name) == "" {
		return usageErrorf("invalid --name: must not be blank")
	}
	return nil
}

// checkUnmodified returns a conflict error when page p, read just before the
// write, was updated at an instant other than expected (given as raw).
func checkUnmodified(id int, p *client.Page, expected time.Time, raw string) error {
	current, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
	if err != nil {
		return &Error{
			Code:    client.CodeInvalidResponse,
			Message: fmt.Sprintf("cannot check --if-unmodified-since: page %d has an invalid updated_at %q; nothing was written", id, p.UpdatedAt),
			Exit:    ExitError,
		}
	}
	if current.Equal(expected) {
		return nil
	}
	by := "an unknown user"
	switch {
	case p.UpdatedBy.Name != "":
		by = p.UpdatedBy.Name
	case p.UpdatedBy.ID != 0:
		by = "user " + strconv.Itoa(p.UpdatedBy.ID)
	}
	return &Error{
		Code:    CodeConflict,
		Message: fmt.Sprintf("page %d was modified at %s by %s; expected %s", id, p.UpdatedAt, by, raw),
		Exit:    ExitConflict,
	}
}

// setContent sets the content of u from the Markdown md in the current
// editor format of page p, read just before, replacing the content or
// appending to it.
func setContent(id int, p *client.Page, md string, appendMD bool, u *client.PageUpdate) error {
	if p.Editor == "markdown" {
		if appendMD {
			if existing := strings.TrimRight(p.Markdown, "\r\n"); existing != "" {
				md = existing + "\n\n" + md
			}
		}
		u.Markdown = &md
		return nil
	}
	h, err := convertMarkdown(md)
	if err != nil {
		return err
	}
	if appendMD {
		// raw_html keeps includes such as {{@3}}; html has them expanded.
		// Without it, appending would replace the page with only the new part.
		if p.RawHTML == "" && strings.TrimSpace(p.HTML) != "" {
			return &Error{
				Code:    CodeUnsafeWrite,
				Message: fmt.Sprintf("page %d has HTML content but BookStack returned no raw_html; refusing to append because it would replace the page. Use --file to replace the content explicitly", id),
				Exit:    ExitError,
			}
		}
		h = p.RawHTML + h
	}
	u.HTML = &h
	return nil
}

// pageMetadata is a written page as printed: without its content.
func pageMetadata(p *client.Page) client.Page {
	out := *p
	out.HTML, out.RawHTML, out.Markdown = "", "", ""
	return out
}
