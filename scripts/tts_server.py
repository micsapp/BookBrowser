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


MAX_CONCURRENCY = env_int("TTS_MAX_CONCURRENCY", 8)
SYNTH_TIMEOUT = env_int("TTS_SYNTH_TIMEOUT", 60)
CACHE_MAX_BYTES = env_int("TTS_CACHE_MAX_BYTES", 500 * 1024 * 1024)
CACHE_MAX_AGE = env_int("TTS_CACHE_MAX_AGE", 14 * 24 * 60 * 60)
# Long tracks are split into paragraph-level chunks that are synthesized on
# separate edge-tts connections in parallel; this is far faster than one long
# serial connection because a single edge-tts stream only sustains ~120-200
# characters/second. Verified that 16 parallel edge-tts connections are
# tolerated. TTS_PARALLEL_CHARS picks the target chunk size and
# TTS_PARALLEL_SLOTS caps how many of those chunk connections run at once
# (which also bounds the global edge-tts connection count via the shared
# synth_slots semaphore).
PARALLEL_CHARS = env_int("TTS_PARALLEL_CHARS", 1200, 200)
PARALLEL_SLOTS = env_int("TTS_PARALLEL_SLOTS", MAX_CONCURRENCY, 1)
# Upstream health: every real edge-tts synthesis records its outcome, and an
# uncached probe runs when the service has been idle for
# TTS_HEALTH_PROBE_INTERVAL seconds. BookBrowser polls GET /health and raises
# one administrator system alert while the status is "failing".
HEALTH_PROBE_INTERVAL = env_int("TTS_HEALTH_PROBE_INTERVAL", 900, 60)
# A failure streak must last this long before it counts as "failing", so a
# network blip that fails several parallel sections at once raises no alert.
HEALTH_CONFIRM_SECONDS = 60
# While failing, probe at this interval so recovery is noticed even when idle.
HEALTH_FAILING_PROBE_INTERVAL = 300
HEALTH_PROBE_TEXT = "Health check."

EN_VOICES = {"en-US-JennyNeural", "en-US-GuyNeural", "en-US-AriaNeural", "en-GB-SoniaNeural"}
ZH_VOICES = {"zh-CN-XiaoxiaoNeural", "zh-CN-YunxiNeural", "zh-CN-XiaoyiNeural"}
ALLOWED = EN_VOICES | ZH_VOICES
DEFAULT_EN = "en-US-JennyNeural"
DEFAULT_ZH = "zh-CN-XiaoxiaoNeural"

CJK_RE = re.compile(r"[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\uff66-\uff9f]")

os.makedirs(CACHE_DIR, exist_ok=True)
synth_slots = asyncio.Semaphore(MAX_CONCURRENCY)


def describe_error(exc: BaseException) -> str:
    """Summarize an edge-tts failure without its long signed request URL."""
    text = str(exc).split(", url=", 1)[0].strip()
    if isinstance(exc, asyncio.TimeoutError) and not text:
        text = "no response within %d seconds" % SYNTH_TIMEOUT
    summary = type(exc).__name__ + (": " + text if text else "")
    return summary[:300]


class UpstreamHealth:
    """Tracks whether Microsoft Edge neural TTS is currently synthesizing."""

    def __init__(self) -> None:
        self.consecutive_failures = 0
        self.failing_since = 0.0
        self.last_success_at = 0.0
        self.last_failure_at = 0.0
        self.last_outcome_at = 0.0
        self.last_error = ""

    def record_success(self) -> None:
        now = time.time()
        if self.consecutive_failures:
            log.info("edge-tts recovered after %d consecutive failures", self.consecutive_failures)
        self.consecutive_failures = 0
        self.failing_since = 0.0
        self.last_success_at = now
        self.last_outcome_at = now

    def record_failure(self, exc: BaseException) -> None:
        now = time.time()
        if not self.consecutive_failures:
            self.failing_since = now
        self.consecutive_failures += 1
        self.last_failure_at = now
        self.last_outcome_at = now
        self.last_error = describe_error(exc)

    def status(self) -> str:
        if self.consecutive_failures:
            persisted = self.last_failure_at - self.failing_since >= HEALTH_CONFIRM_SECONDS
            return "failing" if self.consecutive_failures >= 2 and persisted else "suspect"
        return "ok" if self.last_success_at else "unknown"

    def probe_due(self, now: float) -> bool:
        if not self.last_outcome_at:
            return True
        idle = now - self.last_outcome_at
        status = self.status()
        if status == "suspect":
            return idle >= HEALTH_CONFIRM_SECONDS
        if status == "failing":
            return idle >= HEALTH_FAILING_PROBE_INTERVAL
        return idle >= HEALTH_PROBE_INTERVAL

    def snapshot(self) -> dict:
        return {
            "status": self.status(),
            "consecutive_failures": self.consecutive_failures,
            "failing_since": self.failing_since,
            "last_success_at": self.last_success_at,
            "last_failure_at": self.last_failure_at,
            "last_error": self.last_error,
            "edge_tts_version": getattr(edge_tts, "__version__", ""),
            "time": time.time(),
        }


health = UpstreamHealth()


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


def split_sections(paragraphs: list[str], budget: int) -> list[list[str]]:
    """Group consecutive paragraphs into ~budget-character sections.

    Paragraphs are never split so every section keeps whole paragraphs and its
    own word-boundary timing stays paragraph-accurate.
    """
    sections = []
    current = []
    used = 0
    for paragraph in paragraphs:
        separator = 2 if current else 0
        needed = used + separator + len(paragraph)
        if current and needed > budget:
            sections.append(current)
            current = []
            used = 0
            separator = 0
        current.append(paragraph)
        used += separator + len(paragraph)
    if current:
        sections.append(current)
    return sections


async def synth_section(
    paragraphs: list[str], voice: str, rate: str
) -> tuple[bytes, list[int], int]:
    """Synthesize one paragraph section on a dedicated edge-tts connection.

    Returns (audio bytes, paragraph offsets in ms relative to this section's
    stream, section duration in ms).
    """
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
    return audio, offsets, estimated_ms


async def synth_track(paragraphs: list[str], voice: str, rate: str) -> tuple[bytes, list[int]]:
    """Synthesize one long media track and return paragraph start offsets in ms.

    Long tracks are split into paragraph sections and each section is
    synthesized on its own edge-tts connection in parallel. The MP3 frame
    streams concatenate cleanly (verified) and the section offsets are shifted
    onto the concatenated timeline. Short tracks keep the original single
    connection path.
    """
    sections = split_sections(paragraphs, PARALLEL_CHARS)
    if len(sections) == 1:
        async with synth_slots:
            audio, offsets, _ = await synth_section(sections[0], voice, rate)
        return audio, offsets

    sem = asyncio.Semaphore(PARALLEL_SLOTS)

    async def worker(section: list[str]) -> tuple[bytes, list[int], int]:
        async with sem:
            async with synth_slots:
                audio, offsets, duration = await synth_section(section, voice, rate)
        return audio, offsets, duration

    results = await asyncio.gather(*[worker(section) for section in sections])

    audio_parts = []
    all_offsets = []
    base = 0
    previous = 0
    for audio, offsets, duration in results:
        audio_parts.append(audio)
        for value in offsets:
            value = max(previous, base + int(value))
            all_offsets.append(value)
            previous = value
        base += duration
    audio = b"".join(audio_parts)
    if all_offsets:
        all_offsets[0] = 0
    return audio, all_offsets


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
            health.record_failure(exc)
            log.exception("synth failed")
            return aiohttp.web.json_response({"error": str(exc)}, status=500)
        health.record_success()
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
            audio, offsets = await asyncio.wait_for(
                synth_track(cleaned, voice, rate), timeout=SYNTH_TIMEOUT
            )
        except Exception as exc:
            health.record_failure(exc)
            log.exception("track synth failed")
            return aiohttp.web.json_response({"error": str(exc)}, status=500)
        health.record_success()

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


async def handle_health(request):
    return aiohttp.web.json_response(health.snapshot())


async def probe_upstream() -> None:
    try:
        async with synth_slots:
            data = await asyncio.wait_for(
                synth(HEALTH_PROBE_TEXT, DEFAULT_EN, "+0%"), timeout=min(SYNTH_TIMEOUT, 30)
            )
        if not data:
            raise RuntimeError("edge-tts returned no audio")
    except Exception as exc:
        health.record_failure(exc)
        log.warning("edge-tts health probe failed: %s", health.last_error)
    else:
        health.record_success()


async def health_probe_loop() -> None:
    while True:
        if health.probe_due(time.time()):
            await probe_upstream()
        await asyncio.sleep(15)


async def health_probe_context(app):
    task = asyncio.ensure_future(health_probe_loop())
    yield
    task.cancel()
    try:
        await task
    except asyncio.CancelledError:
        pass


def make_app():
    cleanup_cache()
    app = aiohttp.web.Application()
    app.cleanup_ctx.append(health_probe_context)
    app.router.add_get("/tts", handle_tts)
    app.router.add_post("/tts", handle_tts)
    app.router.add_post("/track", handle_track)
    app.router.add_get("/voices", handle_voices)
    app.router.add_get("/ping", handle_ping)
    app.router.add_get("/health", handle_health)
    return app


if __name__ == "__main__":
    aiohttp.web.run_app(make_app(), host=LISTEN, port=PORT, access_log=None)
