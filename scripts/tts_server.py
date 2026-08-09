#!/usr/bin/env python3
import asyncio
import hashlib
import logging
import os
import re
import tempfile
import time

import aiohttp.web
import edge_tts

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("tts")

LISTEN = os.environ.get("TTS_LISTEN", "127.0.0.1")
PORT = int(os.environ.get("TTS_PORT", "8094"))
CACHE_DIR = os.environ.get("TTS_CACHE", "/tmp/tts-cache")
MAX_TEXT = 12000


def env_int(name: str, default: int, minimum: int = 1) -> int:
    try:
        value = int(os.environ.get(name, str(default)))
    except ValueError:
        log.warning("invalid %s; using %d", name, default)
        return default
    if value < minimum:
        log.warning("%s must be at least %d; using %d", name, minimum, default)
        return default
    return value


MAX_CONCURRENCY = env_int("TTS_MAX_CONCURRENCY", 4)
SYNTH_TIMEOUT = env_int("TTS_SYNTH_TIMEOUT", 60)
CACHE_MAX_BYTES = env_int("TTS_CACHE_MAX_BYTES", 500 * 1024 * 1024)
CACHE_MAX_AGE = env_int("TTS_CACHE_MAX_AGE", 14 * 24 * 60 * 60)

EN_VOICES = {"en-US-JennyNeural", "en-US-GuyNeural", "en-US-AriaNeural", "en-GB-SoniaNeural"}
ZH_VOICES = {"zh-CN-XiaoxiaoNeural", "zh-CN-YunxiNeural", "zh-CN-XiaoyiNeural"}
ALLOWED = EN_VOICES | ZH_VOICES
DEFAULT_EN = "en-US-JennyNeural"
DEFAULT_ZH = "zh-CN-XiaoxiaoNeural"

CJK_RE = re.compile(r"[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\uff66-\uff9f]")

os.makedirs(CACHE_DIR, exist_ok=True)
synth_slots = asyncio.Semaphore(MAX_CONCURRENCY)


def cleanup_cache(protected_path: str = "") -> None:
    now = time.time()
    entries = []
    total = 0
    removed = 0
    reclaimed = 0

    try:
        directory = os.scandir(CACHE_DIR)
    except OSError:
        log.exception("cache scan failed")
        return

    with directory:
        for entry in directory:
            if not entry.name.endswith(".mp3") or not entry.is_file(follow_symlinks=False):
                continue
            try:
                stat = entry.stat(follow_symlinks=False)
            except OSError:
                continue

            if entry.path != protected_path and now - stat.st_mtime > CACHE_MAX_AGE:
                try:
                    os.unlink(entry.path)
                    removed += 1
                    reclaimed += stat.st_size
                except OSError:
                    log.warning("could not remove expired cache file %s", entry.path)
                continue

            entries.append((stat.st_mtime, stat.st_size, entry.path))
            total += stat.st_size

    if total > CACHE_MAX_BYTES:
        entries.sort()
        for _, size, path in entries:
            if total <= CACHE_MAX_BYTES:
                break
            if path == protected_path:
                continue
            try:
                os.unlink(path)
                total -= size
                removed += 1
                reclaimed += size
            except OSError:
                log.warning("could not remove cache file %s", path)

    if removed:
        log.info("cache cleanup removed=%d reclaimed_bytes=%d", removed, reclaimed)


async def synth(text: str, voice: str, rate: str) -> bytes:
    chunks = []
    communicate = edge_tts.Communicate(text, voice, rate=rate)
    async for c in communicate.stream():
        if c["type"] == "audio":
            chunks.append(c["data"])
    return b"".join(chunks)


async def handle_tts(request):
    if request.method == "POST":
        data = await request.post()
        text = data.get("text", "")
        voice = data.get("voice", "")
        rate = data.get("rate", "+0%")
    else:
        text = request.query.get("text", "")
        voice = request.query.get("voice", "")
        rate = request.query.get("rate", "+0%")

    if not text:
        return aiohttp.web.json_response({"error": "missing text"}, status=400)
    text = text[:MAX_TEXT]
    if voice not in ALLOWED:
        voice = DEFAULT_ZH if CJK_RE.search(text) else DEFAULT_EN
    if rate not in ("-20%", "-10%", "-5%", "+0%", "+10%", "+20%", "+30%"):
        rate = "+0%"

    key = hashlib.sha256((voice + "|" + rate + "|" + text).encode()).hexdigest()
    path = os.path.join(CACHE_DIR, key + ".mp3")

    if not os.path.exists(path):
        log.info("synth voice=%s chars=%d", voice, len(text))
        try:
            async with synth_slots:
                data = await asyncio.wait_for(synth(text, voice, rate), timeout=SYNTH_TIMEOUT)
        except Exception as exc:
            log.exception("synth failed")
            return aiohttp.web.json_response({"error": str(exc)}, status=500)
        tmp = ""
        try:
            with tempfile.NamedTemporaryFile(
                mode="wb", dir=CACHE_DIR, prefix=key + ".", suffix=".tmp", delete=False
            ) as fh:
                tmp = fh.name
                fh.write(data)
            os.replace(tmp, path)
            tmp = ""
        finally:
            if tmp:
                try:
                    os.unlink(tmp)
                except OSError:
                    pass
        cleanup_cache(protected_path=path)
    else:
        log.info("cache hit voice=%s chars=%d", voice, len(text))

    return aiohttp.web.FileResponse(
        path, headers={"Content-Type": "audio/mpeg", "Cache-Control": "public, max-age=86400"}
    )


async def handle_voices(request):
    return aiohttp.web.json_response(sorted(ALLOWED))


async def handle_ping(request):
    return aiohttp.web.json_response({"ok": True, "time": time.time()})


def make_app():
    cleanup_cache()
    app = aiohttp.web.Application()
    app.router.add_get("/tts", handle_tts)
    app.router.add_post("/tts", handle_tts)
    app.router.add_get("/voices", handle_voices)
    app.router.add_get("/ping", handle_ping)
    return app


if __name__ == "__main__":
    aiohttp.web.run_app(make_app(), host=LISTEN, port=PORT, access_log=None)
