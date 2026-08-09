# Current Ebook Deployment

Last verified: 2026-08-09

This document describes the production BookBrowser deployment serving `https://ebook.micstec.com`, including the ebook reader, the text-to-speech (TTS) service, paragraph highlighting, and the operational commands used to maintain it.

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
| TTS virtual environment | `/home/mli/ttsvenv` |
| TTS cache | `/tmp/bookbrowser-tts-cache` |

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

The public reader normally uses a URL-encoded `POST /tts/tts` request with `text`, and optionally `voice` and `rate`. Input is capped at 12,000 characters per backend request.

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

The playback flow is:

1. The user selects the read-aloud button. The polished book-and-sound-wave icon changes to a stop icon, `aria-pressed` is updated, and an accessible live status reports playback state.
2. The reader obtains the visible page's start and end CFIs from epub.js and resolves their combined range through the active rendition. This is important because the returned nodes belong to the currently visible iframe rather than a detached document.
3. Visible text is grouped by its closest paragraph or block element and divided into speech chunks of at most 320 characters while retaining the paragraph association.
4. Before each chunk plays, the corresponding live paragraph receives the `tts-reading-paragraph` class. A style injected into the EPUB iframe gives that paragraph the active highlight. The previous highlight is removed as playback advances.
5. The browser posts the text to `/tts/tts`. Nginx forwards it to the local TTS backend, which returns a cached or newly generated MP3.
6. The browser plays the MP3 and continues through the chunks. At the end of a page, it advances the rendition and resumes after epub.js reports the new location.
7. If backend synthesis or MP3 playback fails, the reader falls back to the browser's `SpeechSynthesisUtterance` support.
8. Stopping playback or navigating manually cancels active audio, browser speech synthesis, object URLs, callbacks, and highlights. A playback run identifier prevents stale asynchronous work from restarting an earlier session.

The button is responsive on smaller screens and supports keyboard focus, screen-reader text, an ARIA live status, and reduced-motion preferences.

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
     -ldflags '-X main.curversion=dev-tts-highlight' \
     -o /tmp/BookBrowser.deploy .
   ```

4. Install the binary and restart the application. Installing to the repository's `build` directory may require the current user only; do not change the service paths without also updating the unit.

   ```bash
   install -m 755 /tmp/BookBrowser.deploy \
     /home/mli/projects/BookBrowser/build/BookBrowser
   systemctl --user restart bookbrowser.service bookbrowser-tts.service
   ```

5. Repeat the health checks above and open the public EPUB reader to confirm that audio plays, the current paragraph is highlighted, the control switches to its stop state, and page-to-page reading continues.

## Current verification notes

At the last deployment verification:

- The public reader returned HTTP 200 and served the updated TTS markup, JavaScript, and CSS.
- A public `POST https://ebook.micstec.com/tts/tts` request returned a valid `audio/mpeg` response.
- Desktop and mobile reader layouts showed the improved TTS control correctly.
- The JavaScript syntax check, Git whitespace check, and full Go test suite passed.
- Both user services were enabled and active with user lingering enabled.
- The catalog completed indexing with three errors reported for individual books; this did not prevent the service or reader from operating.
