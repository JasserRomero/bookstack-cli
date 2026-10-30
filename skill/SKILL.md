---
name: bsk
description: Read, search, create and update pages, chapters and books in a BookStack documentation wiki with the bsk CLI; use when a task mentions BookStack, the documentation wiki, a BookStack page URL, or bsk.
---

`bsk` is the command-line client for BookStack. It talks to the BookStack REST API with a token and is built for agents: JSON on stdout, JSON errors on stderr, meaningful exit codes.

This is a reference, not a workflow: decide what to read or write from the task you were given.

## Before you start

```sh
bsk test                 # checks url, token and API permission; prints version
bsk config show          # effective config and the source of each value (secret never shown)
bsk config profiles      # configured profile names
```

If `bsk test` fails with `config_missing`, ask the human to configure credentials (`bsk auth`, see the README). Never ask for or print the token secret.

Profiles: add `--profile <name>` to any command (it goes before or after the subcommand) to target another configured instance. Without it, `BOOKSTACK_PROFILE`, then the config's `default_profile` is used.

## Reading

| Command | Output |
|---|---|
| `bsk search <query> [--type page\|chapter\|book\|bookshelf] [--count N]` | `{"total":N,"data":[...]}`; query uses BookStack search syntax, `--count` 1-100 (default 20) |
| `bsk shelves list` / `bsk shelves get <id>` | array / one shelf with its `books` |
| `bsk books list` / `bsk books get <id>` | array / one book with `contents` (chapters with their pages, and pages outside chapters) |
| `bsk chapters list [--book <id>]` / `bsk chapters get <id>` | array / one chapter with its `pages` |
| `bsk pages list [--book <id>] [--chapter <id>]` | array of page metadata, no content |
| `bsk pages get <id>` | page metadata plus `markdown` (works for WYSIWYG pages too); `html`/`raw_html` omitted |
| `bsk pages get <id> --raw` | only the Markdown, no trailing newline added |
| `bsk pages resolve-url <url> [--raw]` | same as `pages get` for a page web URL (`.../books/<book>/page/<page>`); query, fragment and `/edit` are ignored |

`list` commands return every item (pagination is handled). Ids are positive integers.

## Writing

```sh
bsk pages create --book <id> --name "Deploy" --file deploy.md
generate-doc | bsk pages create --chapter <id> --name "Report" --file - --tag env=prod --tag draft
bsk pages update <id> --file deploy.md
bsk pages update <id> --name "Deploy (v2)" --tag env=prod --tag reviewed
printf '## Changelog\n\n- fixed\n' | bsk pages update <id> --append-file -
bsk books create --name "Operations" --description "Runbooks" --tag team=infra
bsk chapters create --book <id> --name "Runbooks" --description "Procedures"
```

- Content is always Markdown, from a file or from stdin with `-` (only one flag may read stdin). It must be valid UTF-8, non-blank, at most 10 MiB.
- `pages create` needs exactly one of `--book`/`--chapter`, plus `--name` and `--file`. New pages use the Markdown editor.
- `pages update` sends only the given fields; `--file` replaces the content and `--append-file` appends; they are mutually exclusive.
- Editor preservation: content is stored in the page's current editor format. Markdown pages get Markdown; WYSIWYG pages get the Markdown converted to HTML. The editor never changes. Appending to a WYSIWYG page keeps includes such as `{{@3}}`.
- Tags: `--tag key=value` or `--tag key`, repeatable. On `create` they set the tags. On `update` they REPLACE the whole tag set; without `--tag` the tags stay unchanged. To add one tag, read the current tags with `pages get` and pass all of them.
- `--description` is plain text (HTML or Markdown is shown literally).
- Write commands print the resulting entity as JSON (pages without content).

### Do not start content with the page title

BookStack shows the page name as the title. Do not begin page content with `# <page name>`: it would appear twice. `pages get` already removes the title heading that BookStack's export adds, so its Markdown can be edited and written back as is.

### Safe update pattern

Use it whenever someone else may edit the page:

```sh
bsk pages get <id>                    # keep its "updated_at"
bsk pages update <id> --if-unmodified-since <updated_at> --file new.md
```

If the page changed since that read, nothing is written and the command exits 5 with code `conflict`, naming the current `updated_at` and who changed it. Then read the page again, merge, and retry with the new `updated_at`. The guard works with every update flag. It is not atomic: a change landing between bsk's read and write is not detected (BookStack revisions are the recovery path).

### Look up before creating

BookStack allows duplicate names: `create` run twice creates two entities. Before `books create`, `chapters create` or `pages create`, check with `bsk books list`, `bsk chapters list --book <id>`, `bsk pages list --book <id>` or `bsk search`, and reuse what exists.

## Templates

Templates are file-based structures (book, chapters, pages) that the human installs; bsk only reads them.

```sh
bsk template list                                         # [{"name","description","vars","source","path"}]
bsk template show <name>                                  # parsed manifest
bsk template apply <name> --var key=value --dry-run       # plan, no request
bsk template apply <name> --var key=value                 # create a new book
bsk template apply <name> --var key=value --book <id>     # fill an existing book
bsk template apply <name> --chapter <id>                  # pages only, into a chapter
```

- Always run `--dry-run` first and check the plan; every declared var is required and an undeclared `--var` is a usage error.
- Applying twice creates everything twice.
- Output: `{"template":"<name>","complete":true,"created":[{"type","id","name"}]}`. On failure `complete` is `false`, `created` lists what already exists, and nothing is rolled back: finish with `chapters create` / `pages create` instead of re-applying.
- `--templates-dir <dir>` adds a template directory with the highest precedence.

## Errors and exit codes

Errors are one JSON object on stderr; stdout stays empty (except a partial `template apply`):

```json
{"error":{"code":"not_found","status":404,"message":"..."}}
```

`status` is present only when BookStack answered with an HTTP status.

| Exit | Meaning | Typical codes |
|---|---|---|
| 0 | ok | |
| 1 | error | `config_missing`, `config_invalid`, `template_invalid`, `rate_limited`, `server`, `http_error`, `network`, `invalid_response`, `ambiguous`, `unsafe_write` |
| 2 | usage: invalid arguments or flags; no request was made | `usage` |
| 3 | auth: invalid token or missing permission (HTTP 401/403) | `auth` |
| 4 | not found (HTTP 404, or no page for a `resolve-url`) | `not_found` |
| 5 | conflict: page changed since `--if-unmodified-since` | `conflict` |
| 6 | validation (HTTP 422); the message names the fields | `validation` |

Exit 3 usually means the token's role lacks permission for that item or action: report it, do not retry. `ambiguous` (resolve-url) lists the candidate page ids; use `pages get <id>`.
