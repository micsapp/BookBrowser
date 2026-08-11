# BookBrowser
[![Build Status](https://travis-ci.org/geek1011/BookBrowser.svg?branch=master)](https://travis-ci.org/geek1011/BookBrowser)

**Note:** This project is no longer maintained, as I haven't had the time or motivation to continue working on it. BookBrowser will still work as-is (I still use it myself occasionally), but is unlikely to receive any new features or bugfixes.

An easy-to-use tool to generate a web-based ePub and PDF ebook browser. All you need to do is [download it](https://github.com/geek1011/BookBrowser/releases/latest) into the folder with your ebooks, and run it. There is also a [demo](https://bookbrowser-demo.geek1011.net/books/).

## Features
- Multiple book formats
    - epub
    - pdf
    - mobi (basic support)
- Search
- Advanced Search
    - Search any combination of fields
    - View all information in the results
- List view
- Responsive web interface
- Update notifications
- Browse by:
    - Author
    - Series (from calibre metadata)
- Sorted by:
    - Last added
    - Alphabetically
    - Series
- Web based reader
    - Custom fonts, colors, sizing, spacing
    - Remembers your position
    - Book search
    - And more
- Search
- User accounts and role-based access
    - Email/password registration
    - Optional Google Identity Services login
    - Admin, manager, and reader roles
- Private personal library for every user
    - Recently read books
    - User-named favorite lists
    - Private per-book tags and tag browsing
- Administration panel
    - User and role management
    - Ebook upload, recoverable removal, and rescan
    - Registration and anonymous-link settings
- And more
- Easy-to-use
- Fast
- No extra dependencies

## Screenshots

| ![](docs/screenshots/books-mobile.png) | ![](docs/screenshots/books-list-mobile.png) | ![](docs/screenshots/authors-mobile.png) | ![](docs/screenshots/book-mobile.png) |
| --- | --- | --- | --- |
| ![](docs/screenshots/books-desktop.png) | ![](docs/screenshots/books-list-desktop.png) | ![](docs/screenshots/authors-desktop.png) | ![](docs/screenshots/book-desktop.png) |

## Reader Screenshots

| Desktop | Mobile |
| --- | --- |
| ![](docs/screenshots/reader-desktop.png) | ![](docs/screenshots/reader-mobile.png) |

## Advanced Search

| ![](docs/screenshots/list-desktop.png) |
| --- |
| |

## System Requirements
The server works on all platforms.

The web interface works on IE 9+, Edge, Firefox 3+, Chrome, Safari 5.1+, Opera 17+, and Android browser 4.4+.

The web-based reader works on IE 10+, Edge, Firefox 28+, Chrome 21+, Safari 9+, Opera 17+, and Android browser 4.4+.

## Usage

```
Usage: BookBrowser [OPTIONS]

Options:
  -a, --addr string      the address to bind the server to ([IP]:PORT) (default ":8090")
  -b, --bookdir string   the directory to load books from (must exist) (default "/home/patrick/src/BookBrowser")
  -h, --help             Show this help text
  -n, --nocovers         do not index covers
  -t, --tempdir string   the directory to store temp files such as cover thumbnails (created on start, deleted on exit unless already exists) (default "/tmp/bookbrowser946254949")
      --version          Show the version
```

## Remote CLI

Releases also include a separate `bookbrowser-cli` executable. `BookBrowser`
remains the server command; the CLI connects to a running server through the
versioned JSON API.

Install the current Linux or macOS CLI (x64 or arm64) from the published
Droppy release folder:

```sh
curl -fsSL https://tnas_d.micsapp.com/s/bookbrowser_cli/install-bookbrowser-cli.sh | bash
```

Use `--user` to install under `~/.local/bin`, or `--uninstall` to remove it.
Windows users can download `bookbrowser-cli-windows-x64.exe` directly from
`https://tnas_d.micsapp.com/s/bookbrowser_cli/`.

Sign in with email/password:

```sh
bookbrowser-cli login --url https://books.example.com
```

Or use Google sign-in by printing a five-minute browser link, with or without
opening the default browser automatically:

```sh
bookbrowser-cli login --method google-link --url https://books.example.com
bookbrowser-cli login --method google-browser --url https://books.example.com
```

Then browse or administer the server according to the signed-in user's role:

```sh
bookbrowser-cli books list
bookbrowser-cli books search "earthsea"
bookbrowser-cli books download BOOK_ID
bookbrowser-cli library upload ./book.epub
bookbrowser-cli users list
```

Run `bookbrowser-cli help` for the command reference. The full design, API,
credential precedence, security behavior, and administration examples are in
[`cli.md`](cli.md).

## Authentication

Authentication is enabled by default. Account, session, role, and setting data
is stored in SQLite at `<bookdir>/.bookbrowser/bookbrowser.db`. The first email
registration or verified Google registration becomes the administrator; later
registrations become readers. Anonymous users cannot browse the catalog, but
direct `/books/:id` links and the downloads needed by those links remain
available by default.

Signed-in users get a private **My Library** area. Selecting **Read** records a
recent book; named favorite lists and book tags are stored per user and are not
visible to other accounts.

To enable the no-callback Google button, create a Google OAuth web client, add
the ebook site URL as an authorized JavaScript origin, and set its public client
ID before starting BookBrowser:

```sh
export BOOKBROWSER_GOOGLE_CLIENT_ID='your-client-id.apps.googleusercontent.com'
```

No Google client secret or redirect URI is required. Set
`BOOKBROWSER_DATA_DIR` to place the SQLite database somewhere other than the
book directory. See the administrator-served `/implementation.md` guide for the
full access matrix and deployment checklist.
