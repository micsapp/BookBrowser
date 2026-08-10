#!/usr/bin/env python3
from __future__ import annotations

import asyncio
import base64
import bisect
import hashlib
import json
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
                    timing_path = entry.path[:-4] + ".json"
                    if os.path.exists(timing_path):
                        os.unlink(timing_path)
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
                timing_path = path[:-4] + ".json"
                if os.path.exists(timing_path):
                    os.unlink(timing_path)
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


async def synth_track(paragraphs: list[str], voice: str, rate: str) -> tuple[bytes, list[int]]:
    """Synthesize one long media track and return paragraph start offsets in ms."""
    text = "\n\n".join(paragraphs)
    paragraph_starts = []
    cursor = 0
    for paragraph in paragraphs:
        paragraph_starts.append(cursor)
        cursor += len(paragraph) + 2

    audio_chunks = []
    first_offsets = [None] * len(paragraphs)
    search_from = 0
    final_ticks = 0
    communicate = edge_tts.Communicate(text, voice, rate=rate, boundary="WordBoundary")
    async for chunk in communicate.stream():
        if chunk["type"] == "audio":
            audio_chunks.append(chunk["data"])
            continue
        if chunk["type"] != "WordBoundary":
            continue
        boundary_text = chunk.get("text", "")
        position = text.find(boundary_text, search_from) if boundary_text else -1
        if position < 0 and boundary_text:
            position = text.find(boundary_text)
        if position >= 0:
            paragraph_index = max(0, bisect.bisect_right(paragraph_starts, position) - 1)
            if first_offsets[paragraph_index] is None:
                first_offsets[paragraph_index] = int(chunk["offset"] / 10_000)
            search_from = position + len(boundary_text)
        final_ticks = max(final_ticks, int(chunk["offset"] + chunk["duration"]))

    audio = b"".join(audio_chunks)
    # Edge TTS currently emits 48-kbit CBR MP3. Boundary metadata is preferred,
    # while this duration estimate lets empty or punctuation-only paragraphs get
    # a stable interpolated offset too.
    estimated_ms = int(len(audio) * 8 * 1000 / 48_000) if audio else 0
    total_ms = max(estimated_ms, int(final_ticks / 10_000), 1)
    offsets = []
    previous = 0
    text_length = max(len(text), 1)
    for index, start in enumerate(paragraph_starts):
        value = first_offsets[index]
        if value is None:
            value = int(total_ms * start / text_length)
        value = max(previous, int(value))
        offsets.append(value)
        previous = value
    if offsets:
        offsets[0] = 0
    return audio, offsets


def choose_voice_rate(text: str, voice: str, rate: str) -> tuple[str, str]:
    if voice not in ALLOWED:
        voice = DEFAULT_ZH if CJK_RE.search(text) else DEFAULT_EN
    if rate not in ("-20%", "-10%", "-5%", "+0%", "+10%", "+20%", "+30%"):
        rate = "+0%"
    return voice, rate


def timing_header(offsets: list[int]) -> str:
    encoded = json.dumps(offsets, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


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
    voice, rate = choose_voice_rate(text, voice, rate)

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


async def handle_track(request):
    try:
        payload = await request.json()
    except (json.JSONDecodeError, ValueError):
        return aiohttp.web.json_response({"error": "invalid JSON body"}, status=400)
    paragraphs = payload.get("paragraphs")
    if not isinstance(paragraphs, list):
        return aiohttp.web.json_response({"error": "paragraphs must be an array"}, status=400)

    cleaned = []
    used = 0
    for value in paragraphs[:400]:
        if not isinstance(value, str):
            continue
        value = " ".join(value.split()).strip()
        if not value:
            continue
        separator_size = 2 if cleaned else 0
        available = MAX_TEXT - used - separator_size
        if available <= 0:
            break
        value = value[:available]
        if value:
            cleaned.append(value)
            used += separator_size + len(value)
        if used >= MAX_TEXT:
            break
    if not cleaned:
        return aiohttp.web.json_response({"error": "missing paragraph text"}, status=400)

    text = "\n\n".join(cleaned)
    voice, rate = choose_voice_rate(text, str(payload.get("voice", "")), str(payload.get("rate", "+0%")))
    key = hashlib.sha256(("track|" + voice + "|" + rate + "|" + text).encode()).hexdigest()
    path = os.path.join(CACHE_DIR, key + ".mp3")
    timing_path = os.path.join(CACHE_DIR, key + ".json")

    offsets = None
    if os.path.exists(path) and os.path.exists(timing_path):
        try:
            with open(timing_path, "r", encoding="utf-8") as handle:
                offsets = json.load(handle)
        except (OSError, ValueError):
            offsets = None

    if offsets is None:
        log.info("synth track voice=%s paragraphs=%d chars=%d", voice, len(cleaned), len(text))
        try:
            async with synth_slots:
                audio, offsets = await asyncio.wait_for(
                    synth_track(cleaned, voice, rate), timeout=SYNTH_TIMEOUT
                )
        except Exception as exc:
            log.exception("track synth failed")
            return aiohttp.web.json_response({"error": str(exc)}, status=500)

        audio_tmp = ""
        timing_tmp = ""
        try:
            with tempfile.NamedTemporaryFile(
                mode="wb", dir=CACHE_DIR, prefix=key + ".", suffix=".tmp", delete=False
            ) as handle:
                audio_tmp = handle.name
                handle.write(audio)
            with tempfile.NamedTemporaryFile(
                mode="w", encoding="utf-8", dir=CACHE_DIR, prefix=key + ".", suffix=".tmp", delete=False
            ) as handle:
                timing_tmp = handle.name
                json.dump(offsets, handle, separators=(",", ":"))
            os.replace(audio_tmp, path)
            audio_tmp = ""
            os.replace(timing_tmp, timing_path)
            timing_tmp = ""
        finally:
            for tmp in (audio_tmp, timing_tmp):
                if tmp:
                    try:
                        os.unlink(tmp)
                    except OSError:
                        pass
        cleanup_cache(protected_path=path)
    else:
        log.info("track cache hit voice=%s paragraphs=%d chars=%d", voice, len(cleaned), len(text))

    return aiohttp.web.FileResponse(
        path,
        headers={
            "Content-Type": "audio/mpeg",
            "Cache-Control": "public, max-age=86400",
            "X-TTS-Paragraph-Offsets": timing_header(offsets),
            "X-TTS-Paragraph-Count": str(len(offsets)),
        },
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
    app.router.add_post("/track", handle_track)
    app.router.add_get("/voices", handle_voices)
    app.router.add_get("/ping", handle_ping)
    return app


if __name__ == "__main__":
    aiohttp.web.run_app(make_app(), host=LISTEN, port=PORT, access_log=None)
