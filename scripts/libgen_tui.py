#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""libgen_tui.py - interactive terminal UI to search and download books from LibGen mirrors.

Usage:
    python3 libgen_tui.py [--mirror vg|li|rs|st|is] [--dir PATH] [--res 25] [--query TERMS]

Keys inside the UI:
    Type words + Enter   -> run a search
    Up / Down arrows     -> move through the results
    Space                -> toggle selection of the highlighted row
    a                    -> select / deselect all
    Enter on a row       -> same as Space (toggle)
    d                    -> download the selected rows (falls back to the cursor row)
    s                    -> new search (clears selection)
    q / Ctrl-C           -> quit
"""

import argparse
import os
import re
import select
import shutil
import signal
import sys
import termios
import time
import tty
import unicodedata
from dataclasses import dataclass, field
from urllib.parse import unquote

import requests
from bs4 import BeautifulSoup

MIRRORS = {
    "vg": "https://libgen.vg",
    "li": "https://libgen.li",
    "rs": "https://libgen.rs",
    "st": "https://libgen.st",
    "is": "https://libgen.is",
}

HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
    ),
    "Accept-Language": "en-US,en;q=0.9",
    "Referer": "/",
}

RETRIES = 4          # search retries when a mirror returns an empty page
RETRY_SLEEP = 2.0    # seconds between search retries
CHUNK = 65536
STALL_LIMIT = 25     # abort a download if no data arrives for this many seconds
DL_ATTEMPTS = 2      # download retries on transient failure

ANSI = {
    "reset": "\033[0m",
    "bold": "\033[1m",
    "dim": "\033[2m",
    "border": "\033[34m",       # blue
    "header": "\033[1;36m",     # bold cyan
    "cursor": "\033[1;32m",     # bold green
    "selected": "\033[1;33m",   # bold yellow
    "error": "\033[1;31m",      # bold red
    "ok": "\033[1;32m",
    "key": "\033[36m",
    "input": "\033[1;37m",
}


@dataclass
class Book:
    title: str = ""
    authors: str = ""
    publisher: str = ""
    year: str = ""
    language: str = ""
    pages: str = ""
    size: str = ""
    extension: str = ""
    md5: str = ""
    candidates: list = field(default_factory=list)  # ("get"|"ads", url)

    def clean_filename(self):
        name = self.title if self.title else (self.md5 or "book")
        name = re.sub(r"[\\/:*?\"<>|\x00-\x1f]", "_", name).strip()
        name = re.sub(r"\s+", " ", name)
        ext = (self.extension or "").strip(".")
        return _clip_filename(f"{name}.{ext}" if ext else name)


# ---------------------------------------------------------------------------
# terminal helpers
# ---------------------------------------------------------------------------

_OLD_TERM = None
_ENTERED_RAW = False


def atexit_reg():
    import atexit

    def _restore():
        _restore_term()

    atexit.register(_restore)


def _restore_term():
    global _ENTERED_RAW
    if _ENTERED_RAW:
        try:
            termios.tcsetattr(sys.stdin.fileno(), termios.TCSADRAIN, _OLD_TERM)
        except Exception:
            pass
        _ENTERED_RAW = False
    print("\033[0m\033[?25h", end="", flush=True)


def enter_raw():
    global _OLD_TERM, _ENTERED_RAW
    _OLD_TERM = termios.tcgetattr(sys.stdin.fileno())
    tty.setcbreak(sys.stdin.fileno())
    _ENTERED_RAW = True


def stop_echo_restore_sigint():
    signal.signal(signal.SIGINT, lambda *_: (_restore_term(), os._exit(130)))


def term_width():
    return shutil.get_terminal_size(fallback=(80, 24)).columns


def term_height():
    return shutil.get_terminal_size(fallback=(80, 24)).lines


def width_of(text):
    w = 0
    for ch in text:
        w += 2 if unicodedata.east_asian_width(ch) in "WF" else 1
    return w


def pad(text, width):
    text = re.sub(r"\033\[[0-9;]*m", "", text)
    w = width_of(text)
    return text + " " * max(0, width - w)


def truncate(text, width):
    out, w, ansi = [], 0, False
    chars = list(text)
    i = 0
    while i < len(chars) and w < width:
        ch = chars[i]
        if ch == "\033":
            ansi_block = []
            while i < len(chars):
                c = chars[i]
                ansi_block.append(c)
                i += 1
                if c == "m":
                    break
            out.append("".join(ansi_block))
            continue
        cw = 2 if unicodedata.east_asian_width(ch) in "WF" else 1
        if w + cw > width:
            break
        out.append(ch)
        w += cw
        i += 1
    return "".join(out)


_KEY_BUF = b""


def _parse_escape(buf):
    """Parse an escape sequence starting at buf[0] == ESC.
    Returns (key, consumed_bytes) or (None, 0) if the sequence is incomplete."""
    if buf[0] != 0x1B:
        return None, 0
    if len(buf) == 1:
        return None, 0                       # bare ESC, maybe start of a sequence
    if buf[1] == ord("O"):                   # SS3 arrow keys
        if len(buf) < 3:
            return None, 0
        return {65: "UP", 66: "DOWN", 67: "RIGHT", 68: "LEFT"}.get(buf[2], "UNKNOWN"), 3
    if buf[1] == ord("["):                   # CSI sequence
        for i in range(2, len(buf)):
            b = buf[i]
            if 0x40 <= b <= 0x7E:            # final byte
                params = buf[2:i]
                if params in (b"200", b"201"):       # bracketed-paste markers
                    return "IGNORE", i + 1
                if params == b"":                    # plain arrow keys
                    key = {0x41: "UP", 0x42: "DOWN", 0x43: "RIGHT", 0x44: "LEFT"}.get(b)
                    if key:
                        return key, i + 1
                return "UNKNOWN", i + 1     # modified sequence (ctrl/shift+arrow etc.)
        return None, 0                       # incomplete CSI
    return "UNKNOWN", 2


def _decode_prefix(buf):
    """Decode complete UTF-8 chars at the front of buf (no leading ESC).
    Returns (decoded_text, remaining_bytes). Stops at an ESC byte or an
    incomplete/invalid byte sequence."""
    n = len(buf)
    i = 0
    while i < n:
        b = buf[i]
        if b == 0x1B:
            break
        if b < 0x80:
            i += 1
            continue
        if 0xC2 <= b <= 0xDF:
            ln = 2
        elif 0xE0 <= b <= 0xEF:
            ln = 3
        elif 0xF0 <= b <= 0xF4:
            ln = 4
        else:
            i += 1
            continue
        if i + ln > n:
            break                            # need more bytes
        if all(0x80 <= buf[j] <= 0xBF for j in range(i + 1, i + ln)):
            i += ln
        else:
            i += 1                           # bad continuation byte
    if i == 0:
        return "", buf
    return buf[:i].decode("utf-8", "replace"), buf[i:]


def read_key(timeout=0.08):
    """Read one key from the terminal (arrow keys, escape sequences, UTF-8 text).
    Buffers partial multibyte UTF-8 characters so e.g. Chinese input works."""
    global _KEY_BUF
    if not _KEY_BUF:
        if not select.select([sys.stdin], [], [], timeout)[0]:
            return None
        try:
            _KEY_BUF = os.read(sys.stdin.fileno(), 32)
        except OSError:
            _KEY_BUF = b""
        if not _KEY_BUF:
            return None
    buf = _KEY_BUF
    if buf[0] == 0x1B:
        key, consumed = _parse_escape(buf)
        if key is None:
            deadline = time.time() + 0.2     # wait for the rest of the sequence
            while time.time() < deadline and key is None:
                time.sleep(0.01)
                if select.select([sys.stdin], [], [], 0.02)[0]:
                    try:
                        buf += os.read(sys.stdin.fileno(), 32)
                    except OSError:
                        pass
                key, consumed = _parse_escape(buf)
            if key is None:
                key, consumed = "ESC", 1
        _KEY_BUF = buf[consumed:]
        return key
    text, rest = _decode_prefix(buf)
    if rest == buf:                          # front is an incomplete multibyte char
        deadline = time.time() + 0.2
        while time.time() < deadline:
            time.sleep(0.01)
            if select.select([sys.stdin], [], [], 0.02)[0]:
                try:
                    buf += os.read(sys.stdin.fileno(), 32)
                except OSError:
                    pass
            text, rest = _decode_prefix(buf)
            if rest != buf:
                break
        if rest == buf:                      # give up after 0.2s
            text = buf[:1].decode("utf-8", "replace")
            rest = buf[1:]
    _KEY_BUF = rest
    return text


# ---------------------------------------------------------------------------
# search + download
# ---------------------------------------------------------------------------

def make_session():
    s = requests.Session()
    s.headers.update(HEADERS)
    return s


def _clean_title(td0):
    soup = BeautifulSoup(str(td0), "html.parser")
    for tag in soup(["font", "span", "nobr"]):
        tag.decompose()
    text = soup.get_text(" ", strip=True) if soup else ""
    text = re.sub(r"\s*\bc\b\s*$", "", text.strip())          # trailing "c" badge
    text = re.sub(r"\s*\bc\b\s*", " ", text).strip()
    return re.sub(r"\s+", " ", text)


def parse_search(html):
    soup = BeautifulSoup(html, "html.parser")
    table = soup.find("table", id="tablelibgen")
    if not table:
        return []
    books = []
    seen = set()
    for tr in table.find_all("tr")[1:]:
        tds = tr.find_all("td")
        if not tds or not any(td.find("a") for td in tds):
            continue

        def txt(i):
            return tds[i].get_text(" ", strip=True) if i < len(tds) else ""

        title = _clean_title(tds[0]) or txt(0)
        authors = re.sub(r"\s*\(Author\)\s*", "", txt(1)).strip(" ,;")
        if len(tds) == 5:
            size, extension = txt(2), txt(3)
        else:
            size, extension = txt(6), txt(7)
        bc = Book(
            title=title,
            authors=authors,
            publisher=txt(2),
            year=txt(3),
            language=txt(4),
            pages=txt(5),
            size=size,
            extension=extension,
        )
        cands = []
        for a in tds[-1].find_all("a") if len(tds) > 8 else tr.find_all("a"):
            href = a.get("href") or ""
            if re.search(r"get\.php\?md5=", href, re.I):
                cands.append(("get", href))
            elif re.search(r"ads\.php\?md5=", href, re.I):
                cands.append(("ads", href))
        for kind, url in cands:
            m = re.search(r"md5=([0-9a-f]{32})", url, re.I)
            if m:
                bc.md5 = m.group(1)
                break
        bc.candidates = cands
        if bc.md5 and bc.md5 not in seen:
            seen.add(bc.md5)
            books.append(bc)
    return books


def search(session, base, query, res=25):
    """Search and return Book list. Retries because mirrors are flaky."""
    url = base.rstrip("/") + "/index.php"
    # NOTE: the site expects the array syntax "topics[]=l"; plain "topics=l"
    # silently returns an empty page on most mirror nodes.
    params = [("req", query), ("column", "def"), ("topics[]", "l"), ("res", res)]
    html = None
    for attempt in range(RETRIES):
        try:
            r = session.get(url, params=params, timeout=25)
            if r.status_code == 200 and "text/html" in r.headers.get("content-type", ""):
                books = parse_search(r.text)
                if books:
                    return books
                html = r.text
        except requests.RequestException:
            pass
        time.sleep(RETRY_SLEEP * (attempt + 1))
    return []


def resolve_download_url(session, base, book):
    """Return a URL that streams the file, trying candidates in order."""
    for kind, url in book.candidates:
        if not url.startswith("http"):
            url = base.rstrip("/") + "/" + url.lstrip("/")
        if kind == "get":
            return url
        if kind == "ads":
            try:
                r = session.get(url, timeout=25)
                if r.status_code != 200:
                    continue
                soup = BeautifulSoup(r.text, "html.parser")
                for a in soup.find_all("a"):
                    href = a.get("href") or ""
                    if "get.php?md5=" in href and "key=" in href:
                        if href.startswith("http"):
                            return href
                        return base.rstrip("/") + "/" + href.lstrip("/")
            except requests.RequestException:
                continue
    return None


def _fix_filename(name):
    """requests decodes header bytes as latin-1; if the server actually sent
    UTF-8, repair the mojibake (e.g. 'FÃ©nix' -> 'Fénix')."""
    if any(c in name for c in ("\u00c3", "\u00c2")):
        try:
            fixed = name.encode("latin-1").decode("utf-8")
            if "\ufffd" not in fixed:
                return fixed
        except (UnicodeEncodeError, UnicodeDecodeError):
            pass
    return name


def _clip_filename(name, limit=180):
    """Keep a filename within `limit` bytes so it never hits the filesystem
    limit (Linux: 255 bytes), always preserving the extension."""
    root, ext = os.path.splitext(name)
    ext = ext[:10]
    if len(root.encode("utf-8", "replace")) + len(ext.encode("utf-8", "replace")) <= limit:
        return root + ext
    budget = max(1, limit - len(ext.encode("utf-8", "replace")) - 1)
    root = root.encode("utf-8")[:budget].decode("utf-8", "ignore")
    return root + ext


def filename_from_headers(headers, fallback):
    cd = headers.get("content-disposition", "")
    m = re.search(r'filename\*?=(?:UTF-8\'\')?"?([^\";]+)"?', cd, re.I)
    name = unquote(m.group(1)).strip() if m else ""
    name = _clip_filename(_fix_filename(name))
    return name if name else fallback


def _unique_path(path):
    base, ext = os.path.splitext(path)
    n = 1
    while os.path.exists(path):
        path = f"{base} ({n}){ext}"
        n += 1
    return path


def _raw_read(raw, sock, stall_limit):
    """Read one chunk from a requests raw stream. Aborts if no data arrives
    for `stall_limit` seconds. CDN mirrors are very slow, so we must NOT rely
    on the socket read timeout (which kills slow-but-working transfers)."""
    if sock is not None and hasattr(sock, "fileno"):
        readable, _, _ = select.select([sock], [], [], stall_limit)
        if not readable:
            raise TimeoutError(f"no data received for {stall_limit}s")
    return raw.read(CHUNK, decode_content=True)


def download(session, url, dest_dir, on_progress=None):
    """Stream one file to dest_dir. Returns (path, bytes) or raises on failure."""
    last_err = None
    path = None
    for attempt in range(DL_ATTEMPTS):
        try:
            r = session.get(url, stream=True, timeout=(10, 120))
            if r.status_code != 200:
                r.close()
                raise RuntimeError(f"HTTP {r.status_code}")
            raw = r.raw
            conn = getattr(raw, "_connection", None)
            sock = getattr(conn, "sock", None) if conn is not None else None

            name = filename_from_headers(r.headers, None)
            final_url = r.url or url
            if not name:
                m = re.search(r"md5=([0-9a-f]{32})", final_url, re.I)
                name = (m.group(1) if m else "download") + ".bin"
            if path is None:
                path = _unique_path(os.path.join(dest_dir, name))
            elif os.path.exists(path):
                os.remove(path)              # retry: drop the partial file

            total = int(r.headers.get("content-length") or 0)
            done = 0
            encoded = bool(r.headers.get("content-encoding"))
            with open(path, "wb") as fh:
                while True:
                    # When a known Content-Length is fully received (and the
                    # body is not content-encoded, so wire bytes == decoded
                    # bytes), urllib3 reports EOF without touching the socket.
                    # Stop here so the next _raw_read()'s select() can't block
                    # on the idle keep-alive socket and raise a spurious stall
                    # timeout right after the 100% progress update.
                    if total and not encoded and done >= total:
                        break
                    chunk = _raw_read(raw, sock, STALL_LIMIT)
                    if not chunk:
                        break
                    fh.write(chunk)
                    done += len(chunk)
                    if on_progress:
                        on_progress(done, total)
            r.close()
            return path, done
        except Exception as e:               # noqa: BLE001
            last_err = e
            r.close()
            if attempt < DL_ATTEMPTS - 1:
                time.sleep(1.5)
    if path and os.path.exists(path):
        try:
            os.remove(path)
        except OSError:
            pass
    raise last_err


# ---------------------------------------------------------------------------
# the TUI
# ---------------------------------------------------------------------------

class App:
    def __init__(self, session, base, dest_dir, res):
        self.session = session
        self.base = base
        self.dest_dir = os.path.abspath(os.path.expanduser(dest_dir))
        self.res = res
        self.query = ""
        self.books = []
        self.cursor = 0
        self.selected = set()
        self.top = 0
        self.mode = "input"      # input | folder | results
        self.prev_mode = "input"
        self.folder_input = self.dest_dir
        self.status = ""
        self.last_selected_query = None

    # ---------------- drawing ----------------
    def _frame(self):
        W = term_width()
        H = term_height()
        lines = []

        def box_line(title, color=ANSI["border"], fill="\u2500"):
            left = W - width_of(re.sub("\033\\[[0-9;]*m", "", title)) - 2
            line = ANSI["border"] + "\u250c " + fill * max(0, left)
            lines.append(line + title + ANSI["reset"])

        box_line(f"libgen TUI \u2013 {self.base}")
        if self.mode == "input":
            box_line(" Search")
        elif self.mode == "folder":
            box_line(" Download folder")
        else:
            box_line(f" Results ({len(self.books)}) \u2500 matches for \u201c{self.query}\u201d")

        # clue banner
        banner = pad("", W)
        if self.mode == "input":
            banner = ANSI["key"] + "type&Enter=search  " + ANSI["key"] + "^F=folder  "
            banner += ANSI["key"] + "Esc/q=quit" + ANSI["reset"]
        elif self.mode == "results":
            banner = (
                ANSI["key"] + "type&Enter=search  "
                + ANSI["key"] + "\u2191/\u2193=move  "
                + ANSI["key"] + "Space=mark  "
                + ANSI["key"] + "a=all  "
                + ANSI["key"] + "d=download  "
                + ANSI["key"] + "f=folder  "
                + ANSI["key"] + "s=new  "
                + ANSI["key"] + "Esc/q=quit" + ANSI["reset"]
            )
        elif self.mode == "folder":
            banner = ANSI["key"] + "Enter=set  " + ANSI["key"] + "Esc=cancel  " + ANSI["key"] + "Backsp=delete"
        lines.append(pad(truncate(banner, W), W))

        if self.mode == "input":
            q = ANSI["input"] + "query: " + self.query + "\u2588" + ANSI["reset"]
            lines.append(pad(truncate(q, W), W))
        elif self.mode == "folder":
            q = ANSI["input"] + "folder: " + self.folder_input + "\u2588" + ANSI["reset"]
            lines.append(pad(truncate(q, W), W))
        else:
            # header row
            header = self._row_line(marker="", mark="", idx=" #", title="Title",
                                    author="Author(s)", year="Year", size="Size", ext="Ext",
                                    style=ANSI["header"], cursor=False)
            lines.append(header)

            avail = H - len(lines) - 3
            if avail <= 0:
                avail = 1
            if self.cursor < self.top:
                self.top = self.cursor
            if self.cursor >= self.top + avail:
                self.top = self.cursor - avail + 1

            for off in range(avail):
                idx = self.top + off
                if idx < len(self.books):
                    b = self.books[idx]
                    marker = ">" if idx == self.cursor else " "
                    mark = "[x]" if idx in self.selected else "[ ]"
                    style = ANSI["selected"] if idx in self.selected else (ANSI["cursor"] if idx == self.cursor else "")
                    lines.append(self._row_line(marker, mark, idx + 1, b.title, b.authors,
                                                b.year, b.size, b.extension, style,
                                                cursor=idx == self.cursor))
                else:
                    lines.append("")

        if self.mode == "folder":
            lines.append(pad(truncate(ANSI["dim"] + "current: " + self.dest_dir + ANSI["reset"], W), W))
        if self.status:
            lines.append(pad(truncate(ANSI["ok"] + self.status + ANSI["reset"], W), W))
        else:
            lines.append(pad(truncate(ANSI["dim"] + "download folder: " + self.dest_dir + ANSI["reset"], W), W))
        bottom = ANSI["border"] + "\u2514" + "\u2500" * max(0, W - 2) + "\u2518" + ANSI["reset"]
        return "\033[?25l\033[H\033[J" + "\n".join(lines[: H - 1]) + "\n" + bottom

    def _row_line(self, marker, mark, idx, title, author, year, size, ext, style, cursor):
        W = term_width()
        mark = pad(mark, 3)
        idxc = pad(" %d" % idx if isinstance(idx, int) else str(idx), 4)
        # fixed: marker(2) + mark(4) + idx(4) + year(8) + size(8) + ext(6) = 32
        # plus 5 separating spaces; format, size and year always stay visible.
        rest = W - 32 - 5
        if rest >= 40:
            title_w = rest - int(rest * 0.32)
            author_w = rest - title_w
        elif rest >= 20:
            title_w = int(rest * 0.6)
            author_w = rest - title_w
        else:
            title_w = max(0, rest - 5)
            author_w = rest - title_w
        row = " " + marker + " " + mark + ANSI["dim"] + idxc + ANSI["reset"]
        row += " " + pad(truncate(title, title_w), title_w)
        row += " " + pad(truncate(author, author_w), author_w)
        row += " " + pad(truncate(year, 7), 7)
        row += " " + pad(truncate(size, 7), 7)
        row += " " + pad(truncate(ext, 5), 5)
        return style + truncate(row, W) + ANSI["reset"]

    # ---------------- actions ----------------
    def run_search(self):
        self.status = ""
        q = self.query.strip()
        if not q:
            self.status = "empty query - type something first"
            self.mode = "input"
            return
        self.status = f"searching \u201c{q}\u201d on {self.base} ..."
        self.render_once()
        books = search(self.session, self.base, q, self.res)
        self.books = books
        self.cursor = 0
        self.top = 0
        if not books:
            self.mode = "input"
            self.status = "no results (mirror may be flaky) - try again or use another --mirror"
        else:
            self.mode = "results"
            self.selected.clear()
            self.status = f"{len(books)} results"
        self.render_once()

    def download_selected(self):
        targets = list(self.selected) if self.selected else ([self.cursor] if self.books else [])
        if not targets:
            return
        names = [(self.books[i].clean_filename(), i) for i in targets]
        done_ok, done_fail = [], []
        os.makedirs(self.dest_dir, exist_ok=True)
        for n_done, (fallback_name, i) in enumerate(names, 1):
            b = self.books[i]
            self.status = f"[{n_done}/{len(names)}] downloading {fallback_name}"
            self.render_once()
            url = resolve_download_url(self.session, self.base, b)
            if not url:
                done_fail.append((fallback_name, "no download link found"))
                continue
            err = None
            try:
                path, _size = download(self.session, url, self.dest_dir,
                                       on_progress=self._make_progress(fallback_name))
                done_ok.append((path, _size))
            except Exception as e:  # noqa: BLE001
                done_fail.append((fallback_name, str(e)))
        if done_fail and not done_ok:
            reasons = "; ".join(f"{n} -> {e}" for n, e in done_fail)
            reasons = truncate(reasons, 120)
            self.status = ANSI["error"] + f"download failed ({len(done_fail)}): {reasons}" + ANSI["reset"]
        else:
            msg = f"saved {len(done_ok)} file(s) in {self.dest_dir}" + (f"; {len(done_fail)} failed" if done_fail else "")
            self.status = ANSI["ok"] + msg + ANSI["reset"]
        self.render_once()

    def _make_progress(self, name):
        W = term_width()

        def cb(done, total):
            pct = (done / total * 100) if total else 0
            cells = 16 if total else 10
            filled = int(cells * pct / 100)
            bar = "\u2588" * filled + "\u2591" * (cells - filled)
            frac = f"{done/1048576:.1f}/{total/1048576:.1f} MB" if total else f"{done/1048576:.1f} MB"
            line = f"  {truncate(name, W-42)} {bar} {pct:6.1f}% {frac}"
            sys.stdout.write(pad(truncate(ANSI["ok"] + line + ANSI["reset"], W), W) + "\r")
            sys.stdout.flush()

        return cb

    def toggle(self):
        if self.books:
            if self.cursor in self.selected:
                self.selected.discard(self.cursor)
            else:
                self.selected.add(self.cursor)

    def toggle_all(self):
        if self.books:
            if len(self.selected) == len(self.books):
                self.selected.clear()
            else:
                self.selected = set(range(len(self.books)))

    def enter_folder_mode(self):
        self.folder_input = self.dest_dir
        self.prev_mode = self.mode
        self.mode = "folder"

    def apply_folder(self):
        p = os.path.expanduser(self.folder_input.strip()) or os.path.expanduser(".")
        try:
            os.makedirs(p, exist_ok=True)
            self.dest_dir = os.path.abspath(p)
            self.status = ANSI["ok"] + f"download folder: {self.dest_dir}"
        except OSError as e:
            self.status = ANSI["error"] + f"cannot use folder: {e}"
        self.folder_input = self.dest_dir
        self.mode = self.prev_mode

    def render_once(self):
        sys.stdout.write(self._frame() + "\033[?25h")
        sys.stdout.flush()
        time.sleep(0.03)

    # ---------------- main loop ----------------
    def run(self):
        self.render_once()
        last_size = (term_width(), term_height())
        while True:
            k = read_key(0.4)
            if k is None:
                size = (term_width(), term_height())
                if size != last_size:
                    last_size = size
                    sys.stdout.write(self._frame())
                    sys.stdout.flush()
                continue
            if k == "IGNORE":
                continue
            if k == "UNKNOWN":
                sys.stdout.write(self._frame())
                sys.stdout.flush()
                continue
            last_size = (term_width(), term_height())
            if self.mode == "folder":
                if k == "ESC":
                    self.mode = self.prev_mode
                elif len(k) == 1:
                    if k in ("\r", "\n"):
                        self.apply_folder()
                    elif k == "\x03":
                        self.mode = self.prev_mode
                    elif k in ("\x7f", "\x08"):
                        self.folder_input = self.folder_input[:-1]
                    elif k.isprintable():
                        self.folder_input += k
                else:                     # burst of typed chars
                    for ch in k:
                        if ch in ("\x7f", "\x08"):
                            self.folder_input = self.folder_input[:-1]
                        elif ch in ("\r", "\n"):
                            self.apply_folder()
                        elif ch.isprintable():
                            self.folder_input += ch
            elif self.mode == "input":
                if len(k) == 1:
                    if k == "\r" or k == "\n":
                        self.run_search()
                    elif (k in ("q", "Q") and self.query == "") or k in ("\x03", "ESC"):
                        break
                    elif k == "\x06":      # Ctrl+F: set download folder
                        self.enter_folder_mode()
                    elif k in ("\x7f", "\x08"):
                        self.query = self.query[:-1]
                    elif k.isprintable():
                        self.query += k
                else:                     # burst of typed chars
                    for ch in k:
                        if ch in ("\x7f", "\x08"):
                            self.query = self.query[:-1]
                        elif ch in ("\r", "\n"):
                            self.run_search()
                        elif ch.isprintable():
                            self.query += ch
            else:
                if k in ("\x03", "ESC") or k in ("q", "Q"):
                    break
                elif k in ("f", "F"):
                    self.enter_folder_mode()
                elif k in ("UP", "k"):
                    self.cursor = (self.cursor - 1) % len(self.books) if self.books else 0
                elif k in ("DOWN", "j"):
                    self.cursor = (self.cursor + 1) % len(self.books) if self.books else 0
                elif k == " " or k == "\r" or k == "\n":
                    self.toggle()
                elif k in ("a", "A"):
                    self.toggle_all()
                elif k in ("d", "D"):
                    self.download_selected()
                elif k in ("s", "S"):
                    self.query = ""
                    self.books = []
                    self.selected.clear()
                    self.cursor = 0
                    self.mode = "input"
            sys.stdout.write(self._frame())
            sys.stdout.flush()


def main():
    ap = argparse.ArgumentParser(description="TUI to search & download from LibGen mirrors")
    ap.add_argument("--mirror", default="vg", choices=sorted(MIRRORS), help="which mirror to use (default: vg)")
    ap.add_argument("--dir", "--folder", dest="download_dir", default=".",
                    help="download folder (default: current dir)")
    ap.add_argument("--res", type=int, default=25, help="results per page (default: 25)")
    ap.add_argument("--query", default=None, help="start with this search term")
    args = ap.parse_args()

    if not sys.stdin.isatty():
        print("must run in a terminal", file=sys.stderr)
        return 1

    base = MIRRORS[args.mirror]
    folder = os.path.expanduser(args.download_dir)
    try:
        os.makedirs(folder, exist_ok=True)
    except OSError as e:
        print(f"cannot create download folder {folder}: {e}", file=sys.stderr)
        return 1

    atexit_reg()
    stop_echo_restore_sigint()
    enter_raw()
    session = make_session()
    app = App(session, base, folder, args.res)
    if args.query:
        app.query = args.query
        app.run_search()
    try:
        app.run()
    finally:
        _restore_term()
    print()
    return 0


if __name__ == "__main__":
    sys.exit(main())