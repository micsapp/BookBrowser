# Current Ebook Deployment

Last verified: 2026-08-09

This document describes the production MicsBook (BookBrowser) deployment serving `https://ebook.micstec.com`, including the ebook reader, the text-to-speech (TTS) service, paragraph highlighting, and the operational commands used to maintain it. The same application build is also deployed at `https://ebook.micsapp.com` on `aws11.micsapp.com`.

The site is also an installable Progressive Web App (PWA) on supported desktop and mobile browsers.

## Architecture

```text
Browser
  |
  | HTTPS :443
  v
Nginx (ebook.micstec.com)
  |-- /tts/*  --> 127.0.0.1:8094  bookbrowser-tts.service
  |                                      |
  |                                      +--> Microsoft Edge neural TTS
  |                                           (only on an uncached request)
  |
  +-- /*      --> 127.0.0.1:8092  bookbrowser.service
                                         |
                                         +--> /home/mli/books
```

Both application services listen only on loopback. Nginx is the public entry point and terminates TLS.

## Production components

| Component | Current value |
| --- | --- |
| Public URL | `https://ebook.micstec.com` |
| Source repository | `/home/mli/projects/BookBrowser` |
| Git remote | `git@github.com:micsapp/BookBrowser.git` |
| Production branch | `master` |
| Book directory | `/home/mli/books` |
| BookBrowser listener | `127.0.0.1:8092` |
| TTS listener | `127.0.0.1:8094` |
| BookBrowser unit | `~/.config/systemd/user/bookbrowser.service` |
| TTS unit | `~/.config/systemd/user/bookbrowser-tts.service` |
| Nginx site | `/etc/nginx/sites-available/ebook.micstec.com.conf` |
| TLS certificate | `/etc/letsencrypt/live/ebook.micstec.com/fullchain.pem` |
| Installed binary | `/home/mli/projects/BookBrowser/build/BookBrowser` |
| Installed version | `micsbook-65acca8` |
| Authentication database | `/home/mli/books/.bookbrowser/bookbrowser.db` |
| Google login environment | `/home/mli/books/.bookbrowser/google.env` |
| TTS virtual environment | `/home/mli/ttsvenv` |
| TTS cache | `/tmp/bookbrowser-tts-cache` |

The second production instance has these verified application values:

| Component | `ebook.micsapp.com` / aws11 value |
| --- | --- |
| Host | `aws11.micsapp.com` |
| Working and book directory | `/home/mli/books` |
| BookBrowser listener | `localhost:8091` |
| Installed executable | `/home/mli/books/BookBrowser-linux-64bit` |
| Launcher | `/home/mli/books/runit` |
| Installed version | `micsbook-65acca8` |
| Authentication database | `/home/mli/books/.bookbrowser/bookbrowser.db` |
| Google login environment | `/home/mli/books/.bookbrowser/google.env` |

The aws11 launcher loads the Google environment file, then starts the
application with `./BookBrowser-linux-64bit -a localhost:8091`. The live
`/tts/ping` and `/tts/tts` routes are exposed through `ebook.micsapp.com` and
were verified with the release below.

## HTTP and Nginx routing

Port 80 redirects normal traffic to HTTPS while still allowing Let's Encrypt ACME challenges. On port 443, Nginx uses the Let's Encrypt certificate and routes requests as follows:

- `/tts/` is proxied to `http://127.0.0.1:8094/`.
- Every other path is proxied to `http://127.0.0.1:8092`.

The trailing slash on the TTS `proxy_pass` is significant: the reader sends `POST /tts/tts`, which Nginx translates to `POST /tts` on the local TTS service.

The live site configuration is:

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name ebook.micstec.com;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/html;
        default_type "text/plain";
        allow all;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name ebook.micstec.com;

    ssl_certificate /etc/letsencrypt/live/ebook.micstec.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ebook.micstec.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    location /tts/ {
        proxy_pass http://127.0.0.1:8094/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location / {
        proxy_pass http://127.0.0.1:8092;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## BookBrowser service

`bookbrowser.service` runs the compiled Go application from the repository and indexes books under `/home/mli/books`:

```ini
[Unit]
Description=BookBrowser ebook server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/home/mli/books
ExecStart=/home/mli/projects/BookBrowser/build/BookBrowser --addr 127.0.0.1:8092 --bookdir /home/mli/books
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

The service is enabled under the `mli` user's systemd instance. User lingering is enabled, so it starts at boot and remains available without an interactive login.

Authentication data is stored in SQLite under the book directory. A systemd
drop-in loads the public Google OAuth web client ID from
`/home/mli/books/.bookbrowser/google.env`; Google login uses an authorized
JavaScript origin and does not require a redirect callback or client secret.
The first email registration or verified Google registration becomes the
administrator. Later self-registered accounts receive the reader role.

Authentication schema migration v2 stores the browser site name and PWA app
name separately. The default browser brand is `MicsBook`; an administrator can
change the installed-app label independently on the Settings page. The
implementation guide at `/implementation.md` is restricted to administrators
and is rendered as styled, GFM-compatible HTML rather than raw Markdown.

Authentication schema migration v3 adds private reader-library data without
duplicating catalog metadata. It records stable book IDs for each user's recent
reads, named favorite lists, list membership, and private tags. The **My
Library** page resolves those IDs against the current in-memory catalog and
silently ignores stale IDs if a book has been removed. Selecting **Read** on an
authenticated book page updates that user's recent history; anonymous direct
book links continue to open the reader without creating account data.

Authentication schema migration v4 adds private, user-owned EPUB/PDF
bookmarks and notes in `user_reading_items`, with per-item tags in
`user_reading_item_tags`. Reader write APIs require both a signed-in session
and CSRF token, and every update/delete is scoped by the owning user ID.
Anonymous direct-book readers receive no saved-item data, CSRF token, or write
controls. Signed-in readers can save items inside EPUB and PDF readers or
manage everything at `/my-library/reading`.

The header and both readers expose an **About** control. The current deployment
reports build ID `65acca8`, build time `2026-08-10T04:15:18Z`, and build number
`65acca8-20260810T041518Z`; `/api/about` provides the same public metadata.

The installed drop-in is:

```ini
[Service]
EnvironmentFile=-/home/mli/books/.bookbrowser/google.env
```

The web assets are embedded into the Go binary through `public/public-packr.go`. Editing files under `public/static/` alone does not update production; the embedded asset file and binary must also be regenerated and rebuilt.

## TTS service

The TTS backend is implemented by `scripts/tts_server.py` using `aiohttp` and `edge-tts`. Its user service is:

```ini
[Unit]
Description=BookBrowser text-to-speech server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/home/mli/projects/BookBrowser
Environment=TTS_LISTEN=127.0.0.1
Environment=TTS_PORT=8094
Environment=TTS_CACHE=/tmp/bookbrowser-tts-cache
ExecStart=/home/mli/ttsvenv/bin/python /home/mli/projects/BookBrowser/scripts/tts_server.py
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

### TTS API

The local service exposes:

- `GET /ping` for a basic health check.
- `GET /voices` for the allowed voice list.
- `GET /tts` or `POST /tts` to synthesize audio.
- `POST /track` to synthesize a long MP3 with paragraph start offsets.

The current reader sends JSON to public `POST /tts/track` (proxied locally to
`POST /track`) with a `paragraphs` array and optional `voice` and `rate`. The
response is `audio/mpeg`; `X-TTS-Paragraph-Offsets` contains URL-safe base64
JSON millisecond offsets used for highlighting. The older URL-encoded
`POST /tts/tts` endpoint remains as a compatibility fallback. Input is capped
at 12,000 characters per backend request.

Supported English voices are:

- `en-US-JennyNeural` (default)
- `en-US-GuyNeural`
- `en-US-AriaNeural`
- `en-GB-SoniaNeural`

Supported Chinese voices are:

- `zh-CN-XiaoxiaoNeural` (default for CJK text)
- `zh-CN-YunxiNeural`
- `zh-CN-XiaoyiNeural`

Allowed rates are `-20%`, `-10%`, `-5%`, `+0%`, `+10%`, `+20%`, and `+30%`. Unsupported voices and rates are replaced with safe defaults. Japanese, Chinese, and other CJK characters are detected to select the Chinese default; other text uses the English default.

Generated MP3 files are cached by the SHA-256 hash of `voice|rate|text`. Cached responses are returned as `audio/mpeg` with a one-day browser cache header. A cache miss requires outbound Internet access to Microsoft's Edge neural TTS service.

## Reader TTS and paragraph highlighting

The EPUB reader's read-aloud implementation is in `public/static/reader/epub/script.js`, with its control markup in `index.html` and appearance in `style.css`.

The primary playback flow is:

1. The user selects the read-aloud button. The polished book-and-sound-wave icon changes to a stop icon, `aria-pressed` is updated, and an accessible live status reports playback state.
2. The reader gathers live block-level paragraphs from the current EPUB spine
   document, beginning at the visible paragraph, and records each CFI.
3. It groups the paragraphs into long tracks, requests the current five tracks,
   and waits for the first three complete MP3 blobs before playback begins.
   This avoids a background network request for every few hundred characters.
4. One persistent HTML audio element plays the blob tracks. Edge TTS word
   boundaries become millisecond offsets, so `timeupdate` highlights the live
   paragraph currently being spoken.
5. Media Session metadata, playback state, and position state are updated
   throughout playback. The reader continues downloading later tracks while
   the buffered tracks play, following the durable media pattern used by the
   working Vue music-player reference.
6. Returning to the foreground navigates to and highlights the paragraph
   currently playing. Chapter transitions load the next spine document and
   repeat the read-ahead process.
7. If long-track synthesis, timing metadata, or playback is unavailable, the
   older short-track and browser speech-synthesis fallbacks remain available.
8. Stopping playback or navigating manually cancels audio, browser speech,
   object URLs, callbacks, tracks, and highlights. A playback run ID prevents
   stale asynchronous work from restarting an earlier session.

The button is responsive on smaller screens and supports keyboard focus, screen-reader text, an ARIA live status, and reduced-motion preferences.

The floating TTS options panel provides two modes. **Continuous** is the
existing default and reads until stopped or the book ends. **Timed** accepts a
user-defined number of minutes and stops after that much active playback time.
Mode, duration, and the Keep screen on preference are saved in browser local
storage. TTS uses one reusable HTML audio element, preloads long speech tracks,
and registers Media Session play, pause, stop, and position state to improve
playback from the lock screen and other background media controls.

When **Keep screen on while reading** is selected, the reader requests the
Screen Wake Lock API only during active TTS and releases it on stop. Wake locks
are advisory, visible-document-only, and may be denied or released by the
browser or operating system, such as in low-power mode. The reader reports
support and lock status in the options panel and safely falls back when the API
is unavailable.

## Progressive Web App

The whole origin is covered by a root-scoped service worker served from `/sw.js`. The install manifest is generated at `/manifest.webmanifest` using the PWA app name saved in SQLite, starts the standalone app at `/books`, and supplies Books, Search, and Random Book launcher shortcuts. The manifest is fetched network-first so a renamed app is not hidden by an older service-worker cache.

PWA source assets are stored under `public/static/`:

- `manifest.webmanifest` is the packaged fallback manifest; the live route generates equivalent metadata with the configured PWA app name.
- `pwa.js` registers the root service worker and removes the obsolete reader-only worker.
- `sw.js` implements app-shell caching and offline navigation fallback.
- `offline.html` is shown when an uncached navigation is attempted without a network connection.
- `icons/` contains the 192 px and 512 px install icons, the Apple touch icon, and the favicon.

HTML pages use a deep ink theme color (`#061a36`) to match the book-and-flowing-page app icon. The generated icon keeps its important content in the mask-safe center so Android launchers can crop it into their preferred shape.

The service worker uses these policies:

- Page navigations are network-first, with the offline page used only when the network is unavailable.
- Static application assets are served stale-while-revalidate for fast repeat loads.
- Ebook downloads, book covers, API requests, TTS requests, and TTS audio are deliberately excluded from service-worker caching to avoid large or stale caches.
- Old BookBrowser PWA cache versions are removed during activation.

The PWA improves installation and resilience, but it does not currently download whole books for offline reading. Opening a new book, browsing the catalog, and TTS synthesis still require network access.

## Security and exposure

- BookBrowser and the TTS backend bind only to `127.0.0.1`; neither backend port is directly exposed publicly.
- Public traffic reaches the services only through Nginx over TLS.
- The TTS backend validates the voice and rate and caps request text length.
- The TTS endpoint does not currently have separate authentication or rate limiting. It inherits public access from `ebook.micstec.com`; add Nginx rate limiting if abuse becomes a concern.
- The TTS cache is temporary because it lives under `/tmp`. Clearing it is safe, but the next request for each text will require synthesis again.

## Operations and health checks

Check both services:

```bash
systemctl --user status bookbrowser.service bookbrowser-tts.service
systemctl --user is-enabled bookbrowser.service bookbrowser-tts.service
```

Restart both services:

```bash
systemctl --user restart bookbrowser.service bookbrowser-tts.service
```

Follow logs:

```bash
journalctl --user -u bookbrowser.service -f
journalctl --user -u bookbrowser-tts.service -f
```

Check the local TTS service:

```bash
curl http://127.0.0.1:8094/ping
```

Test public TTS end to end:

```bash
curl -X POST -d 'text=Deployment test.' \
  -o /tmp/bookbrowser-tts-test.mp3 \
  https://ebook.micstec.com/tts/tts
file /tmp/bookbrowser-tts-test.mp3
```

Validate Nginx before reloading it:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Updating the deployment

From `/home/mli/projects/BookBrowser`:

1. Update the source and test the reader JavaScript.

   ```bash
   node --check public/static/reader/epub/script.js
   git diff --check
   ```

2. Regenerate the Packr assets after changing anything under `public/`.

   ```bash
   cd public
   /home/mli/go/bin/packr -z
   cp a_public-packr.go public-packr.go
   rm a_public-packr.go
   cd ..
   ```

   Review the generated diff before committing. The temporary `a_public-packr.go` must not be committed.

3. Test and build the Go application.

   ```bash
   env GOCACHE=/tmp/bookbrowser-go-cache \
     GOMODCACHE=/tmp/bookbrowser-go-mod \
     /usr/local/go/bin/go test ./...

   env GOCACHE=/tmp/bookbrowser-go-cache \
     GOMODCACHE=/tmp/bookbrowser-go-mod \
     /usr/local/go/bin/go build \
     -ldflags '-X main.curversion=dev-pwa' \
     -o /tmp/BookBrowser.deploy .
   ```

4. Install the binary and restart the application. Installing to the repository's `build` directory may require the current user only; do not change the service paths without also updating the unit.

   ```bash
   install -m 755 /tmp/BookBrowser.deploy \
     /home/mli/projects/BookBrowser/build/BookBrowser
   systemctl --user restart bookbrowser.service bookbrowser-tts.service
   ```

5. Repeat the health checks above and open the public EPUB reader to confirm that audio plays, the current paragraph is highlighted, the control switches to its stop state, and page-to-page reading continues.

6. For PWA changes, also verify `/manifest.webmanifest` and `/sw.js`, confirm the worker controls the `/` scope in browser developer tools, and run the browser's installability audit. Increment `CACHE_NAME` in `public/static/sw.js` whenever the pre-cached app shell changes.

## Current verification notes

At the last deployment verification:

- Both public readers served the timed/continuous controls, persistent audio,
  Media Session position state, Wake Lock option, five-track read-ahead, and
  PWA cache generation v6.
- Public long-track TTS synthesis on both domains returned HTTP 200
  `audio/mpeg` with two paragraph offsets. The identical deployed application
  binaries have SHA-256
  `1802cb592482dd202a4c6937b8df49745205b2c075940f8d99c7aaf1736ce185`.
- Desktop and mobile reader layouts showed the improved TTS control correctly.
- A Chrome browser check at a 390 by 844 mobile viewport confirmed saved timed
  preferences, timer pause/resume accounting, one persistent audio element,
  Media Session availability, and successful Wake Lock acquisition/release.
- The JavaScript syntax check, Git whitespace check, and full Go test suite passed.
- The local user services were enabled and active with user lingering enabled;
  the aws11 launcher had an active BookBrowser process on port 8091.
- SQLite schema v4, the previous private-library tables, and both private
  reading-item tables were present on both hosts. Automated route tests also
  verified that anonymous context exposes no CSRF token or saved items.
- Anonymous `/my-library` requests redirected to login, while the deployed CSS
  contained the responsive personal-library and book metadata controls.
- The catalog completed indexing with three errors reported for individual books; this did not prevent the service or reader from operating.
- Before replacement, the local executable and stopped SQLite database were
  backed up as `build/BookBrowser.backup-20260810T040401Z` and
  `/home/mli/books/.bookbrowser/bookbrowser.db.backup-20260810T040401Z`.
  Aws11 uses the same timestamp under `/home/mli/books` for
  `BookBrowser-linux-64bit.backup-20260810T040401Z`,
  `.bookbrowser/bookbrowser.db.backup-20260810T040401Z`, and
  `tts_server.py.backup-20260810T040401Z`; its database was also copied while
  the old app was stopped.
