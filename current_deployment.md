# Current Ebook Deployment

Last verified: 2026-10-28 (both `ebook.micstec.com` and `ebook.micsapp.com`)

This document describes the production MicsBook (BookBrowser) deployment serving `https://ebook.micstec.com`, including the ebook reader, the text-to-speech (TTS) service, paragraph highlighting, and the operational commands used to maintain it. A second instance is deployed at `https://ebook.micsapp.com` on `aws11.micsapp.com`; its last verified build is recorded separately below.

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
| Production branch | `main` |
| Book directory | `/home/mli/books` |
| BookBrowser listener | `127.0.0.1:8092` |
| TTS listener | `127.0.0.1:8094` |
| BookBrowser unit | `~/.config/systemd/user/bookbrowser.service` |
| TTS unit | `~/.config/systemd/user/bookbrowser-tts.service` |
| Nginx site | `/etc/nginx/sites-available/ebook.micstec.com.conf` |
| TLS certificate | `/etc/letsencrypt/live/ebook.micstec.com/fullchain.pem` |
| Installed binary | `/home/mli/projects/BookBrowser/build/BookBrowser` |
| Installed version | `cover-fix-20261028` |
| Authentication database | `/home/mli/books/.bookbrowser/bookbrowser.db` |
| Google login environment | `/home/mli/books/.bookbrowser/google.env` |
| TTS virtual environment | `/home/mli/ttsvenv` |
| TTS cache | `/tmp/bookbrowser-tts-cache` |

The second production instance has these verified application values:

| Component | `ebook.micsapp.com` / aws11 value |
| --- | --- |
| Host | `aws11.micsapp.com` |
| Working and book directory | `/home/mli/books` |
| BookBrowser listener | `127.0.0.1:8091` |
| Installed executable | `/home/mli/books/BookBrowser-linux-64bit` |
| Launcher | `/home/mli/books/runit` |
| Installed version | `cover-fix-20261028` |
| Authentication database | `/home/mli/books/.bookbrowser/bookbrowser.db` |
| Launcher environment | `/home/mli/books/.bookbrowser/app.env` |

The aws11 launcher loads `.bookbrowser/app.env`, then starts the
application with `./BookBrowser-linux-64bit -a 127.0.0.1:8091`. The live
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
reports build ID `d9e6add`, build time `2026-08-10T21:55:54Z`, and build number
`d9e6add-20260810T215554Z`; `/api/about` provides the same public metadata.

The installed drop-in is:

```ini
[Service]
EnvironmentFile=-/home/mli/books/.bookbrowser/google.env
```

The web assets are embedded into the Go binary through the tracked `public/a_public-packr.go`. Editing files under `public/static/` alone does not update production; the embedded asset file and binary must also be regenerated and rebuilt.

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
3. It groups paragraphs into a shorter initial track of at most 2,400
   characters and later tracks of at most 6,000 characters. The reader requests
   only the initial track while the user is waiting, begins playback as soon as
   it completes, and then prefetches the next three tracks in the background.
   This prioritizes time-to-first-audio while retaining long background-safe
   tracks and avoiding request-per-paragraph traffic.
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

The floating TTS options panel provides a **Stop after play time** checkbox
that stops reading aloud after a user-defined number of minutes of active
playback; the timer is otherwise always on reading until stopped or the book
ends. The duration and the Keep screen on preference are saved in browser local
storage. TTS uses one reusable HTML audio element, preloads long speech tracks,
and registers Media Session play, pause, stop, and position state to improve
playback from the lock screen and other background media controls.

When **Keep screen on while reading** is selected, the reader requests the
Screen Wake Lock API only during active TTS and releases it on stop. Wake locks
are advisory, visible-document-only, and may be denied or released by the
browser or operating system, such as in low-power mode. The reader reports
support and lock status in the options panel and safely falls back when the API
is unavailable.

## System alerts

Administrators receive service failures through an in-app **System alerts**
channel at `/admin/system`, alongside book requests and copyright claims. While
an alert is unread, the navigation shows a **System alerts** link with a badge,
and the admin dashboard card is highlighted while any alert is ongoing.

Alerts are stored in the `system_alerts` table (authentication schema
migration v11). Each kind of problem has a stable key, and the channel never
floods:

- At most one alert per key is open. Repeated detections refresh its details
  and last-seen time in place.
- An alert closes automatically when its check passes again.
- A problem that returns within 24 hours of recovering reopens the same alert,
  increments its occurrence count, and marks it unread again. A later
  recurrence starts a new alert.
- Marking an alert read clears the badge. An alert that stays open after being
  read is not flagged again.
- Resolved alerts are deleted after 90 days.

BookBrowser polls the TTS service's `GET /health` endpoint once a minute
(`--tts-health-url`, default `http://127.0.0.1:8094/health`; set it to an empty
value to disable) and maintains two alerts:

| Key | Raised when | Closes when |
| --- | --- | --- |
| `tts.upstream` | `/health` reports `failing`: at least two consecutive Edge TTS synthesis failures spanning a minute or more | a synthesis succeeds |
| `tts.service` | three consecutive health checks cannot reach the TTS service | the service answers |

The TTS service records the outcome of every real synthesis. When it has been
idle for 15 minutes (`TTS_HEALTH_PROBE_INTERVAL`), it runs a tiny uncached
probe. It probes one minute after an unconfirmed failure to confirm it, and
every five minutes while failing to detect recovery. A burst of failures in
one moment, such as one network blip across parallel sections, stays
`suspect` and does not raise an alert. A TTS server without `/health` is
tolerated and raises no alerts.

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

Test public TTS end to end. `/ping` does not contact Microsoft, and a repeated
text is served from the cache, so use unique text to force a real synthesis:

```bash
curl -X POST --data-urlencode "text=Deployment test $(date +%s)." \
  -d voice=en-US-GuyNeural \
  -o /tmp/bookbrowser-tts-test.mp3 \
  https://ebook.micstec.com/tts/tts
file /tmp/bookbrowser-tts-test.mp3
```

If the reader falls back to a robotic browser voice that ignores the voice
setting, the backend is failing and only cached audio still plays. The
**System alerts** page reports this as "Neural text-to-speech is failing",
with the last error. `curl http://127.0.0.1:8094/health` shows the same
status. Check the TTS log for `WSServerHandshakeError: 403` from `speech.platform.bing.com`.
That means Microsoft changed the Edge TTS token scheme, and the fix is to
upgrade `edge-tts` and restart the service on both hosts. In October 2026,
edge-tts 7.2.8 (`Sec-MS-GEC-Version=1-...`) began failing every request, and
7.3.1 (`2-...`) fixed it:

```bash
/home/mli/ttsvenv/bin/pip install --upgrade edge-tts
systemctl --user restart bookbrowser-tts.service         # ebook.micstec.com
ssh aws11.micsapp.com '/home/mli/ttsvenv/bin/pip install --upgrade edge-tts &&
  sudo systemctl restart bookbrowser-tts'               # ebook.micsapp.com
```

Keep `deploy/aws11/requirements-tts.txt` pinned to the working version.

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
   cd ..
   ```

   Review the generated diff in `public/a_public-packr.go` before committing.

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

### 2026-10-28 reader cover compatibility

Both public sites run `cover-fix-20261028` (build ID
`6cfe161-cover-fix`). The newly added *100 Books You Must Read Before You Die —
Volume 1* (`0180b7ecdd`) declares EPUB 3 but identifies its cover using EPUB 2
metadata. The reader previously missed the cover and displayed a blocking
`Cannot read properties of undefined (reading 'substr')` error.

The reader now falls back to legacy cover metadata for either EPUB version,
recognizes `cover-image` within a list of manifest properties, and treats absent
or unavailable covers as optional. PWA cache v26 refreshes the changed assets.
The original book file and its ID are unchanged. The Go suite and eight browser
cover fixtures passed; the new regression test reproduced the original failure
against the previous deployment. Run `scripts/test_epub_covers.cjs` against a
running server with Chrome remote debugging, as described in that script.
After deployment, the public reader was verified to load the original cover,
open the first chapter through its 50-entry table of contents, and turn pages
without showing an error. The previous executable is retained at
`/tmp/bookbrowser-cover-fix-k2cb5hhv/BookBrowser.previous` for rollback.

The same binary was subsequently deployed to `ebook.micsapp.com`, and the
newly added anthology was copied there with its original contents and book ID.
All eight browser cover fixtures and the live book's cover, chapter navigation,
and page turns were also verified on that public origin.
The aws11 pre-update executable is retained at
`/home/mli/books/BookBrowser-linux-64bit.backup-cover-fix-20261028T205352Z`.

At the last deployment verification (release `micsbook-d9e6add`):

- Both public domains serve `micsbook-d9e6add` with build ID `d9e6add` and
  identical binary SHA-256
  `358a01c98de84ab5fc49a184a0cd462269308aef830db2f99babf4feebc1961a`.
- Authentication schema is now v5, adding `users.last_ip` and
  `users.allow_create_share_links` (default on). Email and Google registration
  both default to share links enabled; unchecking the register checkbox sets
  them off. The admin Users page reports each account's share-links status,
  recent books read (resolved against the catalog), and last login IP, and adds
  an admin password-reset action that also deletes the account's sessions.
- The book page shows a share section (native share, WhatsApp/Telegram/Facebook/X
  links, and a copy-link field) only for signed-in users who allow share links.
  The copy field uses the origin-aware `PageURL` saved in the page context.
- A help (`?`) button in the header and both readers opens a rendered user
  guide served by `GET /api/help` (`public/docs/user_guide.md` via goldmark).
  The service worker pre-caches `/static/help.js` in app-shell generation v11.
- The EPUB TTS options panel now offers a **Stop after play time** checkbox
  instead of timed/continuous radio modes; independent timer state
  (`ttsStopAfter`, `ttsDurationMinutes`) is saved in local storage.
- The JavaScript syntax checks (`node --check`), Git whitespace check, and the
  full `go test ./... -count=1` suite passed before deployment.
- The local and aws11 services restarted cleanly on the new binary and the
  public `/api/help`, `/sw.js` (v11), login help button, and reader stop-after
  markup were verified on both domains.
- Local binary backups taken at `build/BookBrowser.backup-20260810T215608Z`;
  aws11 backup at
  `/home/mli/books/BookBrowser-linux-64bit.backup-20260810T215806Z`.

Latest fast-start verification (release `deploy-ff2002a`, 2026-08-31):

- The reader no longer starts five synthesis requests and waits for three
  complete tracks. It prioritizes a 2,400-character initial track, plays it as
  soon as its own blob is ready, then prefetches the next three tracks (up to
  6,000 characters each). This avoids backend synthesis-slot contention during
  the user-visible preparation period and stays within the aws11 Nginx burst
  allowance.
- JavaScript syntax checks, Git whitespace checks, the focused EPUB TTS test,
  the first/later track-size behavior check, and `go test ./... -count=1` all
  passed before deployment. The PWA cache generation is `v25`.
- Local `ebook.micstec.com` and aws11 `ebook.micsapp.com` serve the identical
  binary SHA-256
  `cdec715f8c1c1b5b803584ef6e2a1e670684a3698a5cb7a4389ec5dbb8bb0088`.
  Both public readers contain the fast-start markers and omit the old
  three-track `Promise.all` gate; both `/tts/ping` endpoints returned healthy.
- Cold public 2,400-character synthesis completed in 9.34 seconds locally and
  17.31 seconds on aws11, each with exact 15-paragraph timing metadata. Repeated
  cache-hit requests completed in 0.030 and 0.038 seconds respectively.

Admin UI verification (release `deploy-99d9874`, 2026-09-06):

- The manage-library page (`/admin/library`) gained an instant client-side
  filter (title/author), a type filter, and an order-by selector whose default
  "Newest added" is backed by a new Added column showing each file's added date.
  The page is also rendered newest-first server-side for no-JavaScript clients.
- The manage-users page (`/admin/users`) shows a Latest activity column: a
  dated feed merging each user's last sign-in with their most recently opened
  books (resolved to catalog titles), newest first, with time-ago labels.
- New `TestAdminLibraryFiltersAndNewestFirstDefault` and
  `TestAdminUsersShowsLatestActivityFeed` tests, plus updated boundary tests,
  passed with the full `go test ./... -count=1` suite.
- Local `ebook.micstec.com` and aws11 `ebook.micsapp.com` serve the identical
  binary SHA-256
  `091c9926fef603e67686548b500fc779ccd08151d06deaf1ab4637d8ad40fbd7`
  (build `deploy-99d9874`); both public `/login` pages advertise the new build
  and both `/tts/ping` endpoints returned healthy.

System alerts and edge-tts recovery (release `deploy-3e49e2c`, 2026-10-17):

- From 2026-10-13, Edge TTS rejected edge-tts 7.2.8 with HTTP 403 on every
  synthesis (3,755 consecutive failures locally), so readers heard the browser
  fallback voice. Both hosts now run edge-tts 7.3.1, which uses the new
  `Sec-MS-GEC-Version=2-...` token, and `deploy/aws11/requirements-tts.txt`
  pins it.
- The system alerts channel (`/admin/system`, schema v11) and TTS `/health`
  monitor are live on both domains. Both databases migrated from v10 to v11
  with users intact (7 local, 77 on aws11), and both `/health` endpoints
  report `ok` with edge-tts 7.3.1.
- Before deployment, `go test ./... -count=1` passed, the TTS health state
  machine was exercised against a stubbed edge-tts, and the script compiled
  under aws11's Python 3.8. An end-to-end scratch run against real Edge 403s
  (edge-tts 7.2.8) opened exactly one `tts.upstream` alert after two minutes,
  which resolved itself a minute after upgrading to 7.3.1.
- Both domains serve binary SHA-256
  `f4c6ad2094a579e4ecde4d7d112ba902ed613ad6d8c586d51ea139176cc48158`, and
  public `/tts/tts` returned `audio/mpeg` for `en-US-GuyNeural` on both.
- Rollback: the old binary refuses schema v11, so restore the database
  together with the binary. The pre-migration database backups are
  `/home/mli/books/.bookbrowser/bookbrowser.db.backup-20261017T150255Z-pre-system-alerts-v11`
  on each host. The binaries are `build/BookBrowser.pre-v11` (local) and
  `/home/mli/books/BookBrowser-linux-64bit.pre-v11` (aws11), and aws11's
  previous TTS script is `/home/mli/books/tts_server.py.backup-20261017T150255Z`.

Prior verification (release `micsbook-c28e1b0`):

- The TTS service now splits each long `/track` request into
  character-budgeted paragraph sections (`TTS_PARALLEL_CHARS=1200`) that are
  synthesized on separate edge-tts connections in parallel, capped by
  `TTS_MAX_CONCURRENCY`/`TTS_PARALLEL_SLOTS` (default 8). A single edge-tts
  connection only sustains ~120-200 characters/second; the parallel split makes
  a 12000-character track take about 5-15 seconds instead of 30-60 seconds, and
  the reader's initial five-track buffer completed in 17.5s vs 53.1s in the
  before/after benchmark. Short tracks and the legacy `/tts` endpoint keep the
  original single-connection path. Concatenated MP3 frame streams were verified
  with ffmpeg (no decode errors) and paragraph offsets stay monotonic and
  count-exact.
- Both public readers served the persistent audio, Media Session position
  state, Wake Lock option, and five-track read-ahead under PWA cache
  generation v9 (subsequently bumped to v11 with the help asset).
- Public long-track TTS synthesis on both domains returned HTTP 200
  `audio/mpeg` with two paragraph offsets. The identical deployed application
  binaries have SHA-256
  `09b76034e87991f56e83138d556463575e81f0b2266ce94dfdd43ac03a25537c`.
- A live EPUB pagination check confirmed that advancing TTS to an off-screen
  paragraph moved the rendition from page 1 to page 3, caused zero audio-track
  restarts, kept long-track mode active, and left the highlight attached to the
  newly displayed EPUB document.
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
