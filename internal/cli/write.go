package cli

import (
	"context"
	"errors"
	"strings"

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
tags are not sent and stay unchanged.` + writeHelp

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
		name, file, appendFile string
		tags                   tagsFlag
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: strings.SplitN(pagesUpdateLong, "\n", 2)[0],
		Long:  pagesUpdateLong + exitCodesHelp,
		Example: `  bsk pages update 5 --file deploy.md
  bsk pages update 5 --name "Deploy (v2)" --tag env=prod --tag reviewed
  printf '## Changelog\n\n- fixed\n' | bsk pages update 5 --append-file -`,
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
			if mdPath != "" {
				if err := setContent(cmd.Context(), c, id, md, mdFlag == "--append-file", &u); err != nil {
					return err
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
	return cmd
}

func checkName(name string) error {
	if strings.TrimSpace(name) == "" {
		return usageErrorf("invalid --name: must not be blank")
	}
	return nil
}

// setContent sets the content of u from the Markdown md in the current
// editor format of page id, replacing the content or appending to it.
func setContent(ctx context.Context, c *client.Client, id int, md string, appendMD bool, u *client.PageUpdate) error {
	p, err := c.GetPage(ctx, id)
	if err != nil {
		return err
	}
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
