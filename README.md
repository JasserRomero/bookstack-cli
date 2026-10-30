<div align="center">

# bookstack-cli (`bsk`)

**A safe, JSON-first BookStack client for coding agents.**

<a href="https://github.com/JasserRomero/bookstack-cli/releases"><img src="https://img.shields.io/badge/release-v0.1.1-E0115F?style=for-the-badge&labelColor=1A1A1A" alt="Release v0.1.1"></a>
<a href="go.mod"><img src="https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white&labelColor=1A1A1A" alt="Go 1.27"></a>
<a href="https://www.bookstackapp.com/"><img src="https://img.shields.io/badge/BookStack-REST%20API-0288D1?style=for-the-badge&labelColor=1A1A1A" alt="BookStack REST API"></a>

</div>

`bsk` is a command-line client for the [BookStack](https://www.bookstackapp.com/) REST API, built for coding agents. It can search, read, create and update shelves, books, chapters and pages without overwriting other people's work.

- JSON on stdout (or raw Markdown with `pages get --raw`), JSON errors on stderr, documented exit codes.
- Pages are read and written as Markdown, whatever editor the page uses.
- Named profiles for several BookStack instances, and file-based templates for company conventions.
- It only uses the API (`/api/*`) and never modifies BookStack itself. There is no delete command in v1.

**For agents:** an agent-facing reference (commands, output shapes, write rules) is in [`skill/SKILL.md`](skill/SKILL.md).

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Set up a BookStack API token](#set-up-a-bookstack-api-token)
- [Configure](#configure)
- [Commands](#commands)
- [Write safety](#write-safety)
- [Templates](#templates)
- [Development](#development)
- [Known limits](#known-limits)
- [Built with Gentle-AI](#-built-with-gentle-ai)

## Install

Binaries are published for Linux, macOS and Windows (amd64/arm64) on the [GitHub releases](https://github.com/JasserRomero/bookstack-cli/releases) of the repository, one release per version tag. The installers pick the right one for your machine.

### Installer (recommended)

The repository is currently private, so the installer is fetched with the [GitHub CLI](https://cli.github.com/) (`gh auth login` first):

```sh
# Linux / macOS
gh api repos/JasserRomero/bookstack-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh
```

```powershell
# Windows (PowerShell 5.1+ or 7)
gh api repos/JasserRomero/bookstack-cli/contents/install.ps1 -H "Accept: application/vnd.github.raw" | Out-String | iex
```

Once the repository is public, no GitHub CLI is needed:

```sh
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/JasserRomero/bookstack-cli/main/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/JasserRomero/bookstack-cli/main/install.ps1 | iex
```

What the installers do:

- **Download**, in this order: with `gh` if it is installed and authenticated; else through the GitHub API with `GITHUB_TOKEN` or `GH_TOKEN` if set; else anonymously over HTTPS (works only while the repository is public). On a private repository without `gh` or a token they fail and explain both options. The token is never printed.
- **Verify** the archive against the release's `checksums.txt` (SHA-256) and refuse to install on a mismatch or a missing entry.
- **Install** `bsk` without sudo, replacing an existing binary atomically, then print `bsk --version`:

| | Linux / macOS (`install.sh`) | Windows (`install.ps1`) |
|---|---|---|
| Default directory | `$HOME/.local/bin` | `$env:LOCALAPPDATA\Programs\bsk` |
| Other directory | `BSK_INSTALL_DIR` | `-InstallDir` or `BSK_INSTALL_DIR` |
| Pin a version | `BSK_VERSION=v0.1.0` (or `0.1.0`); default: latest release | `-Version v0.1.0` or `BSK_VERSION` |
| `PATH` | prints the `export PATH=...` line to add when the directory is not in `PATH` | adds the directory to the user `PATH` (not the machine's) if missing; restart the terminal |
| Uninstall | `--uninstall` | `-Uninstall` |

Environment variables apply to piped one-liners too, for example `... | BSK_VERSION=v0.1.0 sh`, or `$env:BSK_VERSION = 'v0.1.0'` before the PowerShell line. Releases after v0.1.0 also attach `install.sh` and `install.ps1`, so a pinned installer matches its version.

### Alternatives

**Manual download.** Download the archive for your platform and `checksums.txt` from a release, verify, extract, and put `bsk` (`bsk.exe` on Windows) on your `PATH`. Archives are named `bsk_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows):

```sh
gh release download v0.1.1 -R JasserRomero/bookstack-cli -p 'bsk_0.1.1_linux_amd64.tar.gz' -p checksums.txt
grep ' bsk_0.1.1_linux_amd64.tar.gz$' checksums.txt | sha256sum -c -    # macOS: shasum -a 256 -c -
tar -xzf bsk_0.1.1_linux_amd64.tar.gz bsk
```

**With Go** (1.27 or newer). While the repository is private, set `GOPRIVATE` so Go fetches it directly with your Git credentials instead of the public module proxy:

```sh
GOPRIVATE=github.com/JasserRomero go install github.com/JasserRomero/bookstack-cli/cmd/bsk@latest
```

A `go install` build reports its version as `dev`.

**From source.** `make build` writes `bin/bsk`, versioned from `git describe` (see [Development](#development)).

Check any of them with `bsk --version`.

### Update and uninstall

| Installed with | Update | Uninstall |
|---|---|---|
| `install.sh` | run the one-liner again (or with `BSK_VERSION` to pin) | the one-liner with `sh -s -- --uninstall` instead of `sh` (below) |
| `install.ps1` | run the one-liner again (or with `$env:BSK_VERSION`) | the script with `-Uninstall` (below) |
| `go install` | run `go install ...@latest` again | delete `bsk` from `$(go env GOPATH)/bin` (or `GOBIN`) |
| Manual / `make build` | replace the binary | delete the binary |

```sh
# Linux / macOS (public: curl -fsSL <install.sh URL> | sh -s -- --uninstall)
gh api repos/JasserRomero/bookstack-cli/contents/install.sh -H "Accept: application/vnd.github.raw" | sh -s -- --uninstall
```

```powershell
# Windows (public: replace the gh api command with irm <install.ps1 URL>)
& ([scriptblock]::Create((gh api repos/JasserRomero/bookstack-cli/contents/install.ps1 -H "Accept: application/vnd.github.raw" | Out-String))) -Uninstall
```

Uninstalling removes only the binary. The configuration directory (default `~/.config/bookstack-cli/`), which holds the credentials, is kept; the installers print where it is so you can delete it yourself.

## Quick start

```sh
bsk auth                                   # store URL, token ID and secret (prompts; the secret is hidden)
bsk test                                   # check URL, token and API permission
bsk search "backup policy" --type page --count 5
bsk pages get 5 --raw > page.md            # read a page as Markdown
```

You need a BookStack API token first (next section). Every command has detailed `--help`.

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

Installer tests live under `test/install/`.

### Release

1. Tag the commit `vX.Y.Z` and push the tag (`git tag vX.Y.Z && git push origin vX.Y.Z`).
2. The Release workflow runs `make lint` and `make test`, then GoReleaser builds the six archives (Linux, macOS, Windows × amd64, arm64) and `checksums.txt`, and attaches `install.sh` and `install.ps1` to the GitHub release.
3. Update the release badge at the top of this README to the new version (it is static while the repository is private).

## Known limits

- Rewriting the config file (`bsk auth`) drops TOML comments and unknown keys.
- Two `bsk auth` runs at the same time can lose one update: the last write wins.
- `--if-unmodified-since` is not atomic (see [Write safety](#write-safety)).
- `template apply` does not roll back on partial failure, and templates cannot assign a book to a shelf.
- Out of scope in v1: delete, attachments, images, users and roles, move/copy.

---

## 🌹 Built with Gentle-AI

`bookstack-cli` was crafted and engineered with the **[Gentle-AI](https://github.com/Gentleman-Programming/gentle-ai)** ecosystem—powered by Organic Driven Development (ODD), persistent Engram context, and strict verification discipline.

<p align="center">
  <a href="https://github.com/Gentleman-Programming/gentle-ai">
    <img width="220" src="https://raw.githubusercontent.com/Gentleman-Programming/gentle-ai/main/docs/assets/brand/built-with-gentle-ai.png" alt="Built with Gentle-AI" />
  </a>
</p>

---

<p align="center">
  <sub>Crafted by <a href="https://github.com/JasserRomero">JasserRomero</a>.</sub>
</p>
