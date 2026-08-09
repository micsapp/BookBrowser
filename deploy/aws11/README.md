# aws11 ebook TTS deployment

`ebook.micsapp.com` runs BookBrowser from `/home/mli/books` on
`127.0.0.1:8091`. Nginx sends `/tts/*` to the separate Edge-TTS service on
`127.0.0.1:8094`; the trailing slash on `proxy_pass` turns the reader's
`POST /tts/tts` into `POST /tts` at the Python service.

## Installed paths

- Application binary: `/home/mli/books/BookBrowser-linux-64bit`
- TTS server: `/home/mli/books/tts_server.py`
- Python environment: `/home/mli/ttsvenv`
- TTS cache: `/var/cache/bookbrowser-tts`
- Authentication database: `/home/mli/books/.bookbrowser/bookbrowser.db`
- systemd unit: `/etc/systemd/system/bookbrowser-tts.service`
- Nginx site: `/etc/nginx/sites-enabled/ebook`
- Nginx rate-limit zone: `/etc/nginx/conf.d/bookbrowser-tts-rate-limit.conf`

The service allows four simultaneous syntheses, times each synthesis out after
60 seconds, limits the persistent cache to 500 MiB and 14 days, and runs under
a 256 MiB systemd memory ceiling. Nginx permits two requests per second per
client address with a burst of six.

BookBrowser stores users, roles, sessions, and settings in SQLite. The first
email registration or verified Google registration becomes admin. Stop
BookBrowser before a file-level database backup so the SQLite database and WAL
are consistent.

Google Identity Services requires only the public client ID. Export it in the
BookBrowser launcher when Google login is wanted:

```sh
export BOOKBROWSER_GOOGLE_CLIENT_ID='your-client-id.apps.googleusercontent.com'
```

Add `https://ebook.micsapp.com` as an authorized JavaScript origin in the
Google OAuth web client. No client secret or redirect callback is used.

## Installation outline

On Ubuntu 20.04, install `python3.8-venv`, create `/home/mli/ttsvenv`, and
install the pinned packages from `requirements-tts.txt`. Copy
`../../scripts/tts_server.py` to `/home/mli/books/tts_server.py`, then install
the unit and Nginx files in this directory at the paths above.

Before replacing the Nginx site, copy the old file outside `sites-enabled` so
the backup is not loaded as a second virtual host. Validate with `nginx -t`
before reloading Nginx.

## Operations

```sh
sudo systemctl status bookbrowser-tts.service
sudo journalctl -u bookbrowser-tts.service -f
curl http://127.0.0.1:8094/ping
curl --data-urlencode 'text=BookBrowser TTS test.' \
  -o /tmp/bookbrowser-tts-test.mp3 \
  https://ebook.micsapp.com/tts/tts
file /tmp/bookbrowser-tts-test.mp3
```

Restart the backend after changing `tts_server.py`:

```sh
sudo systemctl restart bookbrowser-tts.service
```
