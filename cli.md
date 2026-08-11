# BookBrowser CLI design

## Status

This document is the implementation contract for the first BookBrowser CLI.
The **Phase 1** scope below is implemented by the separate `bookbrowser-cli`
executable and the server's `/api/v1` routes. Later phases remain documented so
that the API and command names do not need to be redesigned.

## Goal

Add a separate, scriptable, cross-platform CLI for browsing and administering
a running BookBrowser server while leaving the existing server executable and
invocation unchanged.

The CLI should make the most common remote tasks possible without automating
HTML forms:

- sign in with email/password or a browser-assisted Google flow and keep
  profiles for multiple servers/accounts;
- list, search, inspect, and download books;
- upload, recoverably remove, and rescan books as a manager;
- inspect and manage users and settings as an administrator;
- provide stable JSON output for scripts.

## What is borrowed from droppy

The droppy implementation at `~/others/droppy` on `dev.wetigu.com` provides a
useful model:

- explicit JSON APIs instead of scraping browser pages;
- revocable, user-bound bearer tokens whose raw values are shown once;
- named local profiles and an active-profile pointer;
- predictable flag/environment/profile precedence;
- human-readable output by default and `--json` for automation;
- hidden password prompts and profile files restricted to the current user.

BookBrowser should not copy droppy's implementation literally, but it should
keep the same clear server/client boundary. `BookBrowser` remains the server.
A second Go executable named `bookbrowser-cli` contains remote client commands.
Both are built from the same repository and share Go packages, API contracts,
version information, and tests. A menu/TUI wrapper and service start/stop
commands are not part of Phase 1.

## Executables and compatibility

The existing executable remains server-only. Its invocation and flags do not
change:

```sh
BookBrowser --addr :8090 --bookdir /srv/books
```

The remote CLI is a separate executable installed on an administrator's or
reader's client machine:

```sh
bookbrowser-cli login --url https://books.example.com
bookbrowser-cli books list
```

`BookBrowser` never interprets remote CLI commands, and `bookbrowser-cli` never
starts a server, opens a listening socket, scans local books, or initializes a
local server database. The CLI executable name in help should be derived from
`os.Args[0]`, so renamed release binaries still print useful examples.

```text
bookbrowser-cli login       ... authenticate and save a profile
bookbrowser-cli logout      ... revoke a token and remove its profile
bookbrowser-cli whoami      ... show the current server identity
bookbrowser-cli profiles    ... list/remove/rename saved profiles
bookbrowser-cli use         ... select the active profile
bookbrowser-cli tokens      ... list/revoke the current user's API tokens
bookbrowser-cli books       ... list/search/show/download catalog entries
bookbrowser-cli library     ... manager-only library operations
bookbrowser-cli users       ... administrator-only user operations
bookbrowser-cli settings    ... administrator-only server settings
bookbrowser-cli version     ... print the CLI/API client version
bookbrowser-cli help        ... print command help
```

`BookBrowser --version` remains supported by the server executable.
`bookbrowser-cli --version`, `bookbrowser-cli version`, and CLI help return exit
status 0. Invalid CLI syntax returns status 2. The server's legacy behavior of
rejecting positional arguments remains unchanged.

## Phase 1 command reference

### Authentication and profiles

```sh
# 1. Email/password (the default login method)
bookbrowser-cli login --url https://books.example.com
bookbrowser-cli login --method password --url https://books.example.com \
  --email reader@example.com

# 2. Print a short-lived Google sign-in URL, but do not open a browser
bookbrowser-cli login --method google-link --url https://books.example.com

# 3. Print the same URL and open it in the default browser
bookbrowser-cli login --method google-browser --url https://books.example.com

# Any login method can target a named profile without making it active
bookbrowser-cli login --url https://books.example.com --profile work --no-switch
bookbrowser-cli whoami
bookbrowser-cli logout
bookbrowser-cli logout --profile work
bookbrowser-cli profiles list
bookbrowser-cli profiles remove work
bookbrowser-cli profiles rename old-name new-name
bookbrowser-cli use work
bookbrowser-cli tokens list
bookbrowser-cli tokens revoke laptop
```

`--method` accepts exactly `password`, `google-link`, or `google-browser` and
defaults to `password`. Password login prompts for a missing email and password.
Password input is hidden when stdin is a terminal. `--password` is supported
for non-interactive use but is discouraged because process arguments may be
visible to other local users. `--token-name` controls the server-side token
name for every method; otherwise a safe name based on hostname and time is
generated.

Both Google methods use the same server-issued, one-time challenge. The CLI
always prints the short-lived verification URL, its expiry, and the target
profile. `google-link` never attempts to launch another process.
`google-browser` additionally asks the operating system to open the URL in the
default browser; if that fails, it leaves the printed URL available and keeps
waiting. The CLI polls until the browser approves the request, the user
cancels, or the challenge expires.

Google authentication itself happens in the browser through the existing
Google Identity Services integration. The CLI never accepts a Google password
or a Google ID token on its command line. The browser confirms the BookBrowser
server, requesting CLI hostname, token name, and signed-in Google account before
approval. Headless automation should use email/password once to create a
profile, or use `BOOKBROWSER_TOKEN`; it should not automate Google sign-in.

The server returns the raw API token only from the login/token-creation
response. BookBrowser stores only its hash in SQLite. `logout` revokes the
profile's token on the server before deleting the local profile. If revocation
cannot be completed, the command reports the failure and keeps the profile;
`--local-only` explicitly permits removal without server revocation.

### Catalog commands (reader or higher)

```sh
bookbrowser-cli books list
bookbrowser-cli books list --sort modified-desc --limit 50 --offset 0
bookbrowser-cli books search "ursula le guin"
bookbrowser-cli books search --author LeGuin --series Earthsea
bookbrowser-cli books show BOOK_ID
bookbrowser-cli books download BOOK_ID
bookbrowser-cli books download BOOK_ID --output ./earthsea.epub
bookbrowser-cli books download BOOK_ID --output ./downloads/
```

`books list` and `books search` print tabular rows containing ID, title,
author, series, format, size, and modified time. `books show` prints all public
metadata. `--json` prints the server response as JSON instead. Pagination is
bounded server-side; the initial default is 50 and maximum is 200.

Downloads are written to a temporary sibling file and atomically renamed on
success. Existing files are not overwritten unless `--force` is provided.
The server-provided download filename is sanitized before local use.

### Library commands (manager or administrator)

```sh
bookbrowser-cli library status
bookbrowser-cli library upload ./book.epub
bookbrowser-cli library upload ./books/       # recursive supported formats only
bookbrowser-cli library rescan
bookbrowser-cli library remove BOOK_ID
bookbrowser-cli library remove BOOK_ID --yes
```

Uploads reuse the same size, extension, filename, collision, and permission
rules as the web administration page. Directory upload is recursive, ignores
unsupported files, and reports a per-file result. It is not all-or-nothing.

`library remove` asks for confirmation on a terminal unless `--yes` is used.
It keeps the existing recoverable behavior: the file moves into
`.bookbrowser/trash`; it is not permanently deleted. `library rescan` starts a
scan and returns immediately. `library status` reports whether indexing is in
progress, progress, indexed book count, and the last completed scan result.

### User commands (administrator)

```sh
bookbrowser-cli users list
bookbrowser-cli users show USER_ID
bookbrowser-cli users update USER_ID --role manager --active
bookbrowser-cli users update USER_ID --inactive
bookbrowser-cli users password USER_ID
bookbrowser-cli users password USER_ID --password 'new long password'
```

Role and last-active-administrator protections remain enforced by the auth
store. Password reset revokes the user's browser sessions and API tokens.
Passwords are hidden when prompted. User output must never expose password
hashes, Google subjects, session values, or token hashes.

### Settings commands (administrator)

```sh
bookbrowser-cli settings show
bookbrowser-cli settings set --site-name "Home Library"
bookbrowser-cli settings set --pwa-name "Home Books"
bookbrowser-cli settings set --registration-open=false
bookbrowser-cli settings set --anonymous-book-links=true
```

Only explicitly supplied settings are changed. Existing validation and
defaults remain authoritative.

## Global options and configuration resolution

Remote commands accept:

```text
--url URL             server base URL
--token TOKEN         one-shot bearer token
--profile NAME        one-shot named profile
--env-file PATH       load BOOKBROWSER_* values from a chosen file
--json                machine-readable output
--timeout DURATION    HTTP timeout (default 30s; uploads/downloads use 10m)
```

Credentials and URL are resolved independently in this priority order:

```text
1. --url / --token flags
2. BOOKBROWSER_URL / BOOKBROWSER_TOKEN process environment
3. values in the explicitly selected --env-file
4. --profile NAME
5. BOOKBROWSER_PROFILE process environment
6. active local profile
```

There is no automatic upward search for `.env`; a surprising parent-directory
credential is worse than requiring `--env-file`. Flags and environment values
can intentionally override only one field from a profile.

`BOOKBROWSER_CLI_HOME` overrides the local CLI state directory. Otherwise the
CLI uses the operating system's user configuration directory, under
`bookbrowser/cli`. Its logical layout is:

```text
bookbrowser/cli/
├── current
└── profiles/
    └── NAME.json
```

Profile names match `[A-Za-z0-9._-]{1,64}`. Profile files contain the normalized
base URL, raw token, email, user ID, role, token name, and save time. Writes are
atomic and use mode `0600` where the platform supports Unix permissions. The
CLI must never print a saved token unless the user explicitly supplies
`--show-token`; JSON output also redacts it by default.

## HTTP API contract

The CLI uses a versioned JSON API under `/api/v1`. Browser form routes continue
to work unchanged. Bearer tokens authenticate API routes only; they are not
accepted as a substitute for browser cookies on HTML/form routes.

### Authentication

```text
POST   /api/v1/auth/login          email/password/token_name -> token + user
POST   /api/v1/auth/google/start   create a short-lived CLI login challenge
GET    /cli/google/:challenge      short-lived browser verification page
POST   /api/v1/auth/google/complete
                                   browser verifies Google and approves challenge
POST   /api/v1/auth/google/poll    CLI waits for approval and receives token once
POST   /api/v1/auth/google/cancel  CLI cancels an unfinished challenge
GET    /api/v1/me                  current user and server/build identity
GET    /api/v1/tokens              current user's tokens
DELETE /api/v1/tokens/:name        revoke one current-user token
DELETE /api/v1/token               revoke the calling token
```

Login shares the existing failed-attempt limiter. API tokens are random,
high-entropy values with a recognizable `bbk_` prefix. The database stores a
SHA-256 hash, token name, user ID, created time, last-used time, and optional
expiry. Token names are unique per user. Disabled users cannot authenticate.

The Google challenge flow is application-level browser assistance, not a
second Google OAuth client or a request for Google credentials in the terminal:

1. `google/start` returns a high-entropy polling secret, a separate public
   challenge identifier, a verification URL on the same BookBrowser server,
   an expiry timestamp, and a minimum polling interval.
2. The CLI prints the URL. Only `google-browser` asks the local OS to open it.
3. The verification page uses the already configured
   `BOOKBROWSER_GOOGLE_CLIENT_ID` and posts the Google ID credential, public
   challenge identifier, explicit approval, and browser CSRF token to
   `google/complete`.
4. The server reuses `googleTokenVerifier.Verify` and `Store.UpsertGoogle`, so
   issuer, audience, signature, expiry, verified-email, registration, identity
   linking, disabled-account, and first-administrator rules stay identical to
   browser sign-in.
5. `google/poll` authenticates the CLI with the polling secret. Once approved,
   it mints the user-bound API token and returns the raw value exactly once.

Pending polls return HTTP 202 plus the required retry interval. Challenges
expire after five minutes, are single-use, and are removed after success,
expiry, or cancellation. Challenge state may remain in server memory because a
server restart can safely invalidate it; polling secrets are retained only as
hashes. No raw API token is created or stored while the browser is pending.
If Google login is not configured, `google/start` returns a stable
`google_login_unavailable` error.

### Catalog and download

```text
GET /api/v1/books
GET /api/v1/books/:id
GET /api/v1/books/:id/download
```

The collection endpoint accepts `q`, `author`, `series`, `format`, `sort`,
`limit`, and `offset`. Responses contain `items`, `limit`, `offset`, and
`total`. Book JSON uses explicit DTOs rather than encoding the internal
`booklist.Book`, so server filesystem paths and unstable implementation fields
are never exposed.

### Manager library operations

```text
GET    /api/v1/library/status
POST   /api/v1/library/books       multipart field: book
DELETE /api/v1/library/books/:id
POST   /api/v1/library/rescan
```

### Administrator operations

```text
GET   /api/v1/users
GET   /api/v1/users/:id
PATCH /api/v1/users/:id
POST  /api/v1/users/:id/password
GET   /api/v1/settings
PATCH /api/v1/settings
```

Every JSON response uses `Content-Type: application/json; charset=utf-8`.
Success responses use 2xx status codes. Errors have one stable envelope:

```json
{
  "error": {
    "code": "book_not_found",
    "message": "Book not found."
  }
}
```

The CLI sends `Authorization: Bearer TOKEN`, a versioned `User-Agent`, and an
`Accept: application/json` header. It follows redirects only when they remain
on the same origin, preventing bearer-token leakage to another host.

## Server-side implementation design

The API and web administration handlers must share operations rather than
forking behavior. In particular, upload, recoverable removal, settings update,
and user update should be extracted into small server/service methods called by
both the HTML and JSON handlers.

Expected code organization:

```text
bookbrowser.go          existing server entry point; behavior remains unchanged
cmd/bookbrowser-cli/
  main.go               dedicated remote CLI executable entry point
cli/
  run.go                command parsing and dispatch
  client.go             HTTP/JSON, streaming upload/download
  browser.go            injectable cross-platform default-browser launcher
  config.go             flag/env/profile resolution
  profile.go            atomic profile storage
  output.go             human and JSON formatting
auth/
  tokens.go             token data and store behavior
server/
  api.go                v1 routing, JSON helpers, bearer middleware
  api_auth.go           login/me/token endpoints
  api_books.go          catalog/download endpoints
  api_admin.go          manager/admin endpoints
```

The project already uses `pflag`; Phase 1 should keep it and use dedicated
`FlagSet` instances rather than add a large command framework dependency.
Command runners return errors/status codes instead of calling `os.Exit`, which
makes parsing and output testable. `release.sh` builds both executables for the
same supported platforms, and the Docker image exposes both commands. Installing
the CLI alone does not require installing or running the server executable.

## Published CLI installer

Current binaries are published through the stable Droppy share
`https://tnas_d.micsapp.com/s/bookbrowser_cli`. Linux and macOS users can
install the matching x64 or arm64 build with:

```sh
curl -fsSL https://tnas_d.micsapp.com/s/bookbrowser_cli/install-bookbrowser-cli.sh | bash
```

The installer verifies `SHA256SUMS`, installs `bookbrowser-cli` into a suitable
system or user `bin` directory, supports `--prefix`, `--user`, and `--uninstall`,
and ad-hoc signs macOS binaries when `codesign` is available. The shared folder
also contains the Windows x64 CLI executable and portable Linux x64/arm64
server binaries.

## Database migration

Add the next SQLite migration for API tokens:

```sql
CREATE TABLE api_tokens (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL COLLATE NOCASE,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER,
    expires_at   INTEGER,
    UNIQUE (user_id, name)
);
CREATE INDEX api_tokens_user_idx ON api_tokens(user_id, created_at DESC);
CREATE INDEX api_tokens_expiry_idx ON api_tokens(expires_at);
```

The auth `Store` interface gains narrowly named token methods. Password reset
and user disable delete that user's API tokens in the same transaction as the
existing session revocation. Tests must verify that raw tokens never appear in
SQLite.

## Output and exit status

Human-readable results go to stdout. Diagnostics, warnings, and errors go to
stderr. Color is used only when stderr/stdout is a terminal and is disabled by
`NO_COLOR`.

```text
0  success
1  transport/server/local I/O failure
2  command-line usage error
3  authentication or authorization failure
4  not found or conflict
```

`--json` produces exactly one JSON value on stdout and no progress text there.
Failures still use a non-zero exit status and emit the API error envelope as
JSON, enabling reliable automation.

## Security requirements

- Use HTTPS for non-loopback URLs by default. Plain HTTP requires
  `--allow-http`; loopback HTTP remains allowed for local/container use.
- Never log authorization headers, passwords, raw tokens, or complete profile
  contents.
- Never accept a Google password or Google ID token through a CLI flag,
  environment variable, profile, URL, or log entry.
- Compare token hashes in constant time and store only token hashes server-side.
- Generate independent high-entropy public challenge IDs and polling secrets;
  keep the polling secret out of the browser URL and store only its hash.
- Expire Google CLI challenges after five minutes, consume them once, enforce a
  server-provided polling interval, and rate-limit start, complete, and poll.
- Require browser CSRF validation and an explicit confirmation page that shows
  the signed-in account, requesting CLI hostname, server, and token name.
- Enforce the same role checks on API routes as on the corresponding web pages.
- Keep upload limits, extension checks, path containment, collision handling,
  and recoverable removal identical between web and CLI callers.
- Do not send a bearer token across a cross-origin redirect.
- Cap response bodies used for JSON errors; stream book data instead of loading
  it into memory.
- Close response bodies and remove partial download files on every failure.

## Test and acceptance plan

Tests use `httptest.Server` and temporary directories; they must not require a
real deployed server.

1. Existing `go test ./...` remains green and legacy server flags still parse.
2. Auth migration is repeatable and raw API tokens are never stored.
3. Password and both Google login modes are covered, including hidden prompts,
   the no-browser guarantee, browser-launch success/fallback, pending polling,
   cancellation, expiry, single consumption, CSRF, rate limits, unconfigured
   Google login, and identity/registration rules.
4. Profile precedence, validation, atomic writes, permissions, switching,
   rename, logout failure behavior, and token redaction are covered.
5. Catalog filtering, sorting, pagination, DTO redaction, and download filename
   sanitization are covered.
6. Upload and removal tests prove parity with the browser administration path,
   including max size, unsupported extensions, collisions, path containment,
   and recoverable trash placement.
7. `--json` output can be decoded for every Phase 1 command and errors do not
   contaminate stdout with human progress messages.
8. Release builds contain separate server and CLI artifacts. Running the CLI
   never opens a listening socket, scans local books, or initializes a local
   server database; running `BookBrowser` retains its current server behavior.
9. Google challenge tests verify that the polling secret never enters the
   browser URL or server database, an approving browser never receives the API
   token, and a second poll cannot retrieve the token again.

## Later phases (not part of the first implementation)

- Personal-library commands for recent books, named lists, tags, bookmarks,
  and notes.
- Token creation separate from password login, expiry selection, and token
  rotation.
- Shell completion generation.
- An interactive terminal browser/TUI similar to droppy's optional wrapper.
- Batch upload concurrency and resumable transfers.
- Background service install/start/stop integration, which must remain separate
  from the portable CLI because service managers vary by platform.
