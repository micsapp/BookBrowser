# BookBrowser authentication and RBAC implementation guide

Status legend: `[ ]` pending, `[~]` in progress, `[x]` complete.

This document is the implementation contract and phase checklist for adding
accounts, Google sign-in, role-based access control, library administration,
and private reader collections to BookBrowser. The running application serves
it at `/implementation.md` to administrators.

## Goals

- Support email/password registration and login.
- Support optional Google OpenID Connect login.
- Provide `admin`, `manager`, and `reader` roles.
- Preserve anonymous direct-book links, including the reader and book download
  required by those links.
- Give administrators user, library, and settings management pages.
- Let administrators configure the browser site name separately from the PWA
  app name shown when the library is installed on a device.
- Give managers library management pages.
- Show each signed-in user their recently read books.
- Let each user create named favorite lists and add or remove books.
- Let each user add private tags to books and browse books by those tags.
- Give EPUB read-aloud a continuous mode and a user-defined timed mode.
- Keep TTS attached to a persistent media session for screen-off listening and
  optionally request a screen wake lock while the user follows highlighting.
- Keep timed and continuous TTS alive in an installed PWA by synthesizing long
  read-ahead tracks with paragraph timing metadata instead of performing a new
  background network request for every short paragraph.
- Show an About item in the application header with a build number containing
  both the source build ID and UTC build timestamp.
- Let signed-in readers create, edit, tag, revisit, and delete private EPUB or
  PDF bookmarks and notes. Anonymous direct-book readers remain read-only and
  never receive the writing controls.
- Keep persistence embedded in BookBrowser while isolating storage behind a
  repository interface for a future PostgreSQL or managed-database adapter.

## Authorization matrix

| Capability | Anonymous | Reader | Manager | Admin |
| --- | --- | --- | --- | --- |
| Login and register | Yes | Yes | Yes | Yes |
| Open a direct `/books/:id` link | Yes | Yes | Yes | Yes |
| Read/download a directly linked book | Yes | Yes | Yes | Yes |
| Browse/search the library | No | Yes | Yes | Yes |
| Use recent reads, named lists, and private tags | No | Yes | Yes | Yes |
| Manage library books | No | No | Yes | Yes |
| Manage users and roles | No | No | No | Yes |
| Manage application settings | No | No | No | Yes |

Anonymous book access is controlled by the `anonymous_book_links` setting and
is enabled by default. It does not expose the catalog, author list, series list,
search, random-book route, or download index.

## Persistence and security model

- Persistent state lives in SQLite at `.bookbrowser/bookbrowser.db` under the
  configured book directory unless `BOOKBROWSER_DATA_DIR` overrides it.
- Handlers depend on an authentication repository interface rather than SQLite
  directly. A future PostgreSQL adapter can implement the same interface.
- Schema changes use numbered, transactional migrations. SQLite runs with
  foreign keys, a busy timeout, and WAL mode enabled.
- Migration v2 adds the configurable PWA name and updates the original default
  browser brand to `MicsBook` without overwriting a custom site name.
- Migration v3 adds per-user reading activity, named book lists, list items,
  and book tags. It stores stable catalog book IDs rather than duplicating book
  metadata, and user deletion cascades to all private collection data.
- Migration v4 adds `user_reading_items` for private bookmarks and notes plus
  `user_reading_item_tags` for normalized per-item tags. Every lookup and
  mutation includes the authenticated user ID; deleting a user or item cascades
  to its reading data and tags.
- Passwords use PBKDF2-HMAC-SHA256 with a unique random salt; plaintext
  passwords are never stored.
- Session tokens are cryptographically random. Only their SHA-256 hashes are
  persisted. Sessions expire after 30 days.
- State-changing forms use CSRF tokens and POST requests.
- Login attempts are rate limited in memory.
- The first successfully registered account becomes the administrator whether
  it uses email or Google. Later self-registered accounts receive the `reader`
  role.
- Administrators cannot deactivate or demote the last active administrator.
- Deleted books are moved to `.bookbrowser/trash` with a non-ebook suffix so
  they can be recovered and are not re-indexed.

## Google login configuration

Google Identity Services is shown when this environment variable exists:

- `BOOKBROWSER_GOOGLE_CLIENT_ID`

The sign-in screen renders Google's official responsive button as the primary
account action when configured. Email login remains available beneath it.

The Google OAuth web client needs the ebook site URL as an authorized JavaScript
origin; no client secret or redirect callback is used. Google Identity Services
returns an ID token to browser JavaScript, which posts it to BookBrowser over
HTTPS. The backend verifies its signature and `iss`, `aud`, `exp`, `sub`, and
verified-email claims before creating a session. Google accounts are matched by
verified email address, and the first verified Google account may bootstrap the
administrator.

## Routes

Public authentication routes:

- `GET|POST /login`
- `GET|POST /register`
- `POST /logout`
- `POST /auth/google`

Administration routes:

- `GET /admin`
- `GET /admin/users`
- `POST /admin/users/:id`
- `GET /admin/library`
- `POST /admin/library/upload`
- `POST /admin/library/delete/:id`
- `POST /admin/library/rescan`
- `GET|POST /admin/settings`
- `GET /implementation.md`

Private reader-library routes:

- `GET /my-library`
- `GET /my-library/lists/:id`
- `POST /my-library/lists`
- `POST /my-library/lists/:id/delete`
- `POST /my-library/lists/:id/books/:book_id`
- `POST /my-library/lists/:id/books/:book_id/remove`
- `GET /my-library/tags?tag=:tag`
- `POST /books/:id/tags`
- `POST /books/:id/tags/remove`
- `GET /read/:id` records a recent read and opens the appropriate reader.
- `GET /my-library/reading` manages all bookmarks and notes for the user.
- `POST /my-library/reading/:id` edits an owned bookmark or note.
- `POST /my-library/reading/:id/delete` deletes an owned bookmark or note.

Reader integration routes:

- `GET /api/about` returns public application and build information.
- `GET /api/reader/context?book_id=:id` returns build information plus private
  reading items and a CSRF token only when the request has a valid session.
- `POST /api/reader/items` creates an authenticated bookmark or note.
- `POST /api/reader/items/:id` edits an owned item and its tags.
- `POST /api/reader/items/:id/delete` deletes an owned item.

The TTS service also exposes `POST /track`. It accepts ordered paragraph text,
returns one long MP3 track, and includes compact paragraph start offsets derived
from Edge TTS word-boundary metadata. The reader downloads the current and next
track before they are needed, uses one media element, and updates Media Session
position state during playback.

## Implementation phases

- [x] Phase 1: document the design, route policy, persistence model, and checks.
- [x] Phase 2: implement the persistent auth store, password hashing, sessions,
  CSRF protection, and login throttling with unit tests.
- [x] Phase 3: implement email registration/login/logout and optional Google
  login, then add account-aware navigation.
- [x] Phase 4: enforce route-level RBAC while preserving anonymous direct-book
  access.
- [x] Phase 5: implement the admin dashboard, user/role management, library
  upload/rescan/recoverable deletion, and settings.
- [x] Phase 6: embed and serve this guide, regenerate packed assets, and run
  unit, integration, formatting, and build checks.
- [x] Phase 7: update deployment configuration, deploy with a binary backup,
  validate authentication and anonymous links, then commit and push.
- [x] Phase 8: add the SQLite v3 private-library schema and repository API.
- [x] Phase 9: implement recent reads, named favorite lists, private tags, and
  their account-aware user interface.
- [x] Phase 10: test ownership isolation and CSRF enforcement, pack assets,
  deploy both production sites with backups, and validate the live feature.
- [x] Phase 11: add persistent TTS playback preferences, timed sessions,
  background media controls, and an optional screen wake lock.
- [x] Phase 12: replace short background TTS transitions with timed read-ahead
  tracks, paragraph offsets, Media Session position state, and recovery checks.
- [x] Phase 13: add the SQLite v4 reading-item schema, repository operations,
  authenticated APIs, ownership/CSRF tests, and management page.
- [x] Phase 14: add EPUB/PDF bookmark and note controls plus the public About
  panel and build-number metadata.
- [ ] Phase 15: pack, test, back up, deploy both sites and TTS services, then
  validate background playback and private-data isolation.

## Phase checks

The implementation is complete only when all of these checks pass:

- The first email or verified Google registration creates an admin and can log
  in.
- A later email registration creates a reader.
- Correct passwords authenticate and incorrect passwords do not.
- Google login accepts only a correctly signed, unexpired ID token with a
  verified email and the configured OAuth audience.
- Anonymous catalog requests redirect to login.
- Anonymous direct book pages and their reader/download requests still work.
- Readers cannot enter administration routes.
- Managers can upload, rescan, and recoverably remove books but cannot manage
  users or settings.
- Admins can change roles/status, but cannot remove the final active admin.
- CSRF failures reject state changes.
- The SQLite store survives a process restart without exposing session tokens
  or plaintext passwords.
- The packed production binary contains the new templates, styles, and this
  guide.
- Reading a book updates only that user's recent-read history.
- Users can create, populate, view, and delete their own named lists but cannot
  access another user's lists by guessing an ID.
- Tags are private per user, case-insensitively unique per book, and removable.
- Collection pages ignore stale book IDs if a catalog book is later removed.
- Continuous TTS preserves the existing page-to-page behavior; timed TTS stops
  at the configured playback duration, including while the screen is off.
- One reusable HTML audio element and Media Session play/pause/stop handlers
  support background playback without losing the current TTS run.
- The Keep screen on option requests a wake lock only while TTS is playing,
  releases it when TTS stops, and reacquires it after the reader becomes
  visible if the browser released it.
- Browsers without Screen Wake Lock or Media Session support continue to read
  aloud with the supported subset of controls.
- TTS starts from a fully downloaded long track, preloads the next track, sends
  Media Session position updates, and does not require a request at each
  paragraph while the PWA is hidden.
- Paragraph offsets within a long track continue to move the visible highlight;
  returning from the background navigates to the paragraph currently playing.
- `/api/about` reports the build ID, UTC build time, and their combined build
  number on anonymous and authenticated requests.
- Anonymous reader context contains no CSRF token, bookmark/note data, or
  writing controls, and anonymous write requests return an authorization error.
- Signed-in users can create EPUB CFI and PDF page bookmarks/notes, edit titles
  and note text, replace tags, revisit locations, and delete items.
- Reading-item IDs cannot be used to view, edit, retag, or delete another
  user's data.

## Operational checks

After deployment:

```sh
curl -I https://ebook.micsapp.com/login
curl -I https://ebook.micsapp.com/books
curl -I https://ebook.micsapp.com/implementation.md
sudo systemctl status bookbrowser.service
```

Back up the existing executable and `.bookbrowser/bookbrowser.db` before
replacing a production binary. Only the public Google client ID belongs in
service configuration; no Google client secret is required.
