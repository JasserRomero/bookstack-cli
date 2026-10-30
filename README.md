# bookstack-cli (`bsk`)

`bsk` is a command-line client for the [BookStack](https://www.bookstackapp.com/) REST API, built for coding agents. It can search, read, create and update shelves, books, chapters and pages without overwriting other people's work.

- JSON on stdout (or raw Markdown with `pages get --raw`), JSON errors on stderr, documented exit codes.
- Pages are read and written as Markdown, whatever editor the page uses.
- Named profiles for several BookStack instances, and file-based templates for company conventions.
- It only uses the API (`/api/*`) and never modifies BookStack itself. There is no delete command in v1.

An agent-facing reference is in [`skill/SKILL.md`](skill/SKILL.md).

## Install

With Go (1.27 or newer):

```sh
go install github.com/JasserRomero/bookstack-cli/cmd/bsk@latest
```

Or download a binary for Linux, macOS or Windows (amd64/arm64) from the [GitHub releases](https://github.com/JasserRomero/bookstack-cli/releases) of the repository, published for each version tag, and put `bsk` on your `PATH`.

Check it with `bsk --version`.

## Set up a BookStack API token

1. In BookStack, the user that owns the token needs a role with the **Access System API** permission (Settings → Roles).
2. Open that user's profile (Settings → Users → the user → API Tokens) and create a token. Copy the token ID and secret; the secret is shown only once.
3. The token can do exactly what its user's role allows. Give the role only the permissions the agents need (for example, read everything but edit only some books).

**Recommendation (not enforced):** create one bot user per team, with a role holding the minimal permissions that team's agents need, and use its token. Edits then show up in BookStack as that bot, and a token can be revoked without affecting people. `bsk` itself imposes nothing: each profile uses whatever token its owner configures.

## Configure

Store the credentials with `bsk auth`. There is no flag for the secret, so it never lands in shell history or the process list:

```sh
# Interactive: prompts for anything missing, the secret with a hidden prompt
bsk auth

# Non-interactive: the secret is the first line of stdin
printf '%s\n' "$SECRET" | bsk auth --url https://docs.example.com --token-id <token-id>
```

`auth` writes `$XDG_CONFIG_HOME/bookstack-cli/config.toml` (default `~/.config/bookstack-cli/config.toml`) atomically, with mode `0600` in a `0700` directory. It does not contact BookStack; check the connection with:

```sh
bsk test          # {"ok":true,...,"version":"...","app_name":"..."}
bsk config show   # effective values and their source; token_secret is always "set"
```

If the web address users see differs from the API address (for example behind a proxy), pass it with `--resolve-url`; `bsk pages resolve-url` then accepts URLs from either address.

### Environment variables

| Variable | Purpose |
|---|---|
| `BOOKSTACK_URL` | BookStack base URL |
| `BOOKSTACK_TOKEN_ID` | API token ID |
| `BOOKSTACK_TOKEN_SECRET` | API token secret |
| `BOOKSTACK_RESOLVE_URL` | Public URL, when it differs from `BOOKSTACK_URL` (optional) |
| `BOOKSTACK_PROFILE` | Profile to use when `--profile` is not given |
| `XDG_CONFIG_HOME` | Base directory of the config file |

Each connection variable overrides only its own key of the selected profile; the three of `URL`, `TOKEN_ID` and `TOKEN_SECRET` together work without any config file (useful in CI).

Precedence, highest first:

1. `--profile <name>`: use that profile; connection environment variables are then ignored.
2. Connection environment variables, per key, over the selected profile.
3. `BOOKSTACK_PROFILE`, then `default_profile` of the config file, selects the profile.

### Profiles

Profiles let one machine work with several BookStack instances (for example, several companies or clients). A single profile needs no flag.

```sh
bsk auth --profile client                 # create or update the "client" profile
bsk config profiles                       # {"default_profile":"default","profiles":["client","default"]}
bsk --profile client test                 # any command accepts --profile
BOOKSTACK_PROFILE=client bsk books list
```

The first profile written becomes `default_profile`. An unknown profile is a usage error that lists the available ones.

The config file looks like this:

```toml
default_profile = "default"

[profiles.default]
url = "https://docs.example.com"
token_id = "..."
token_secret = "..."

[profiles.client]
url = "https://wiki.client.example.com"
resolve_url = "https://docs.client.example.com"   # optional
token_id = "..."
token_secret = "..."
```

## Commands

| Command | What it does |
|---|---|
| `bsk auth` | Store credentials in a profile |
| `bsk config show` / `bsk config profiles` | Effective configuration / profile names |
| `bsk test` | Check URL, token and API permission |
| `bsk search <query> [--type ...] [--count N]` | Search with BookStack's syntax |
| `bsk shelves list` / `get <id>` | Shelves, a shelf with its books |
| `bsk books list` / `get <id>` / `create` | Books, a book with its contents, create a book |
| `bsk chapters list [--book <id>]` / `get <id>` / `create` | Chapters, a chapter with its pages, create a chapter |
| `bsk pages list [--book <id>] [--chapter <id>]` / `get <id> [--raw]` | Pages, a page with its Markdown |
| `bsk pages create` / `update <id>` | Create a page, update its name, content or tags |
| `bsk pages resolve-url <url> [--raw]` | Read the page behind a BookStack web URL |
| `bsk template list` / `show <name>` / `apply <name>` | File-based templates |

Every command has detailed `--help`, including its output shape and exit codes.

### Examples

```sh
bsk search "backup policy" --type page --count 5
bsk books get 7
bsk pages get 5 --raw > page.md
bsk pages resolve-url 'https://docs.example.com/books/ops/page/deploy#bkmrk-step-1'

bsk books create --name "Operations" --description "Runbooks and on-call notes" --tag team=infra
bsk chapters create --book 7 --name "Runbooks"
generate-doc | bsk pages create --chapter 9 --name "Report" --file - --tag env=prod
printf '## Changelog\n\n- fixed\n' | bsk pages update 5 --append-file -
```

### Output and exit codes

Errors are printed to stderr as `{"error":{"code":"<code>","status":<http>,"message":"..."}}`; `status` appears only when BookStack answered with an HTTP status.

| Exit | Meaning |
|---|---|
| 0 | ok |
| 1 | error (configuration, network, server, invalid template, ...) |
| 2 | usage: invalid arguments or flags; no request is made |
| 3 | auth: invalid token or missing permission (HTTP 401/403) |
| 4 | not found (HTTP 404) |
| 5 | conflict: the page changed since `--if-unmodified-since` |
| 6 | validation (HTTP 422) |

## Write safety

- **Editor preservation.** Input is always Markdown, but it is stored in the page's current editor format: Markdown pages receive Markdown, WYSIWYG pages receive the Markdown converted to HTML (GitHub-flavored: tables, strikethrough, task lists, autolinks). A page never switches editor. New pages use the Markdown editor.
- **Append keeps includes.** `--append-file` on a WYSIWYG page appends to the page's stored `raw_html`, not the rendered `html`, so includes such as `{{@3}}` are kept instead of being expanded. If a WYSIWYG page has no `raw_html`, the append is refused (`unsafe_write`) instead of replacing the page.
- **No duplicated title.** BookStack's Markdown export starts with `# <page name>`; `pages get` removes that heading so content can be written back unchanged. Do not start new content with the page name as a heading.
- **Tags.** On `update`, `--tag` replaces the page's whole tag set; without `--tag`, tags are untouched.
- **Concurrency guard.** `pages update <id> --if-unmodified-since <updated_at>` writes only if the page's `updated_at` still matches the value from your earlier `pages get`; otherwise it writes nothing and exits 5, naming who changed the page and when. The check is **not atomic**: BookStack has no optimistic locking, so `bsk` reads and then writes, and a change landing between the two is not detected. BookStack's page revisions remain the way to recover an overwritten edit.
- **Duplicates.** BookStack allows duplicate names: `create` or `template apply` run twice creates everything twice. Look up first (`books list`, `chapters list --book <id>`, `search`).
- Writes are not retried on server or network errors, so a write is never sent twice by accident; only a rate limit (HTTP 429) is retried, honoring `Retry-After`.

## Templates

A template is a directory with a `template.toml` manifest and the Markdown files it references. It describes a book with pages and chapters, with variables such as `{{.proyecto}}`. `bsk` only reads templates; the full manifest format is in `bsk template --help`.

Templates are searched in, highest precedence first:

| Location | `source` |
|---|---|
| `--templates-dir <dir>` | `flag` |
| `<config-dir>/profiles/<profile>/templates/` | `profile` |
| `<config-dir>/templates/` | `shared` |

`<config-dir>` is the directory of the config file (default `~/.config/bookstack-cli/`). A name in a higher location shadows the same name below it.

The repository includes an example project book (a document sheet and a change-log chapter) in [`examples/templates/proyecto/`](examples/templates/proyecto/). It is not installed automatically; copy it:

```sh
mkdir -p ~/.config/bookstack-cli/templates
cp -r examples/templates/proyecto ~/.config/bookstack-cli/templates/

bsk template list
bsk template apply proyecto --var proyecto=IRIS --dry-run   # plan only, no request
bsk template apply proyecto --var proyecto=IRIS             # create the book
bsk template apply proyecto --var proyecto=IRIS --book 7    # fill an existing book
```

The whole template is validated and rendered before the first request. If a request fails midway, the output lists what was created with `"complete":false`; nothing is rolled back.

## Development

```sh
make build   # bin/bsk, version from git describe
make test    # unit and CLI tests; never contact a BookStack
make lint    # gofmt check and go vet
make e2e     # end-to-end tests against a local Docker BookStack
```

`make e2e` needs Docker. It runs `test/e2e/up.sh`, which starts a disposable BookStack (pinned version, MariaDB) bound to `127.0.0.1` only, generates its secrets locally into git-ignored files, and issues an API token into `test/e2e/.env.local`. The suite runs only with `BSK_E2E=1` and the `e2e` build tag, and refuses any `BOOKSTACK_URL` whose host is not `127.0.0.1` or `localhost`. Never point it at a real instance.

```sh
source test/e2e/.env.local   # use the sandbox from your shell after up.sh
test/e2e/down.sh             # destroy the sandbox, its volumes and secrets
```

The port defaults to `6875` (`BSK_E2E_PORT` to change it).

## Known limits

- Rewriting the config file (`bsk auth`) drops TOML comments and unknown keys.
- Two `bsk auth` runs at the same time can lose one update: the last write wins.
- `--if-unmodified-since` is not atomic (see [Write safety](#write-safety)).
- `template apply` does not roll back on partial failure, and templates cannot assign a book to a shelf.
- Out of scope in v1: delete, attachments, images, users and roles, move/copy.
