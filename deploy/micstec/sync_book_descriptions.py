#!/usr/bin/env python3
import argparse
import datetime
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import uuid

DEFAULT_FILE = "/home/mli/books/.bookbrowser/book-descriptions.json"
MAX_BYTES = 8 << 20
HASH_RE = re.compile(r"^[0-9a-f]{40}$")
SSH_OPTS = ("-o", "BatchMode=yes", "-o", "ConnectTimeout=15")


class SyncError(Exception):
    pass


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def backup_stamp():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")


def parse_timestamp(value):
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=datetime.timezone.utc)
    return parsed


def load_document(path):
    try:
        size = os.path.getsize(path)
    except OSError as exc:
        raise SyncError("cannot stat %s: %s" % (path, exc))
    if size > MAX_BYTES:
        raise SyncError("%s exceeds %d bytes" % (path, MAX_BYTES))
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError) as exc:
        raise SyncError("cannot read %s: %s" % (path, exc))


def validate_document(document, label):
    if not isinstance(document, dict):
        raise SyncError("%s: root must be an object" % label)
    if document.get("version") != 1:
        raise SyncError("%s: unsupported version %r" % (label, document.get("version")))
    descriptions = document.get("descriptions")
    if not isinstance(descriptions, dict):
        raise SyncError("%s: descriptions must be an object" % label)
    hashes = set()
    for key, entry in descriptions.items():
        if not isinstance(key, str) or not key:
            raise SyncError("%s: invalid description key %r" % (label, key))
        if not isinstance(entry, dict):
            raise SyncError("%s/%s: entry must be an object" % (label, key))
        book_hash = entry.get("book_hash")
        if not isinstance(book_hash, str) or not HASH_RE.match(book_hash):
            raise SyncError("%s/%s: invalid book_hash" % (label, key))
        if not book_hash.startswith(key):
            raise SyncError("%s/%s: book_hash does not match key" % (label, key))
        text = entry.get("text")
        if not isinstance(text, str) or not text.strip():
            raise SyncError("%s/%s: empty text" % (label, key))
        if book_hash in hashes:
            raise SyncError("%s: duplicate book_hash %s" % (label, book_hash))
        hashes.add(book_hash)
    return descriptions


def merge_documents(local_document, remote_document):
    local = validate_document(local_document, "local")
    remote = validate_document(remote_document, "remote")
    merged = {}
    remote_only = 0
    conflicts = []
    for key, entry in local.items():
        merged[key] = entry
    for key, entry in remote.items():
        if key not in merged:
            merged[key] = entry
            remote_only += 1
            continue
        current = merged[key]
        if current.get("book_hash") != entry.get("book_hash"):
            raise SyncError("conflicting book_hash for %s" % key)
        if current.get("text") == entry.get("text"):
            continue
        local_time = parse_timestamp(current.get("retrieved_at"))
        remote_time = parse_timestamp(entry.get("retrieved_at"))
        if remote_time is not None and (local_time is None or remote_time > local_time):
            chosen = "remote"
            merged[key] = entry
        else:
            chosen = "local"
        conflicts.append(
            {
                "key": key,
                "title": current.get("title") or entry.get("title") or "",
                "local": current.get("retrieved_at") or "",
                "remote": entry.get("retrieved_at") or "",
                "chosen": chosen,
            }
        )
    local_only = len(set(local) - set(remote))
    result = dict(local_document)
    result["version"] = 1
    result["descriptions"] = merged
    changed_local = merged != local
    changed_remote = merged != remote
    if changed_local or changed_remote:
        result["updated_at"] = utc_now()
    validate_document(result, "merged")
    stats = {
        "local": len(local),
        "remote": len(remote),
        "merged": len(merged),
        "local_only": local_only,
        "remote_only": remote_only,
        "conflicts": conflicts,
        "changed_local": changed_local,
        "changed_remote": changed_remote,
    }
    return result, stats


def serialize_document(document):
    data = json.dumps(document, ensure_ascii=False, indent=2) + "\n"
    if len(data.encode("utf-8")) > MAX_BYTES:
        raise SyncError("merged file exceeds %d bytes" % MAX_BYTES)
    return data


def write_atomic(path, data):
    parent = os.path.dirname(os.path.abspath(path))
    os.makedirs(parent, exist_ok=True)
    fd, temp_path = tempfile.mkstemp(prefix="." + os.path.basename(path) + ".tmp.", dir=parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(data)
        os.chmod(temp_path, 0o600)
        os.replace(temp_path, path)
    except Exception:
        try:
            os.unlink(temp_path)
        except OSError:
            pass
        raise


def backup_local(path):
    if not os.path.isfile(path):
        return None
    backup = "%s.bak-sync-%s" % (path, backup_stamp())
    try:
        shutil.copy2(path, backup)
    except OSError as exc:
        raise SyncError("cannot back up %s: %s" % (path, exc))
    return backup


def run_ssh(peer, command, allow_missing=False):
    args = ["ssh"] + list(SSH_OPTS) + [peer, command]
    try:
        result = subprocess.run(args, capture_output=True, encoding="utf-8")
    except OSError as exc:
        raise SyncError("ssh %s: %s" % (peer, exc))
    if result.returncode == 3 and allow_missing:
        return None
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise SyncError("ssh %s failed: %s" % (peer, detail or ("exit %d" % result.returncode)))
    return result.stdout


def fetch_remote(peer, remote_file):
    quoted = shlex.quote(remote_file)
    command = "if [ -f %s ]; then cat %s; else exit 3; fi" % (quoted, quoted)
    output = run_ssh(peer, command, allow_missing=True)
    if output is None:
        return None
    try:
        document = json.loads(output)
    except ValueError as exc:
        raise SyncError("remote JSON: %s" % exc)
    validate_document(document, "remote")
    return document


def backup_remote(peer, remote_file):
    backup = "%s.bak-sync-%s" % (remote_file, backup_stamp())
    command = "cp -p %s %s" % (shlex.quote(remote_file), shlex.quote(backup))
    run_ssh(peer, command)
    return backup


def write_remote_atomic(peer, remote_file, data):
    parent = os.path.dirname(os.path.abspath(remote_file))
    base = os.path.basename(remote_file)
    remote_temp = os.path.join(
        parent, ".%s.tmp.%d.%s" % (base, os.getpid(), uuid.uuid4().hex[:8])
    )
    fd, local_temp = tempfile.mkstemp(prefix="book-descriptions-", suffix=".json")
    moved = False
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(data)
        scp_args = ["scp", "-q"] + list(SSH_OPTS) + [local_temp, "%s:%s" % (peer, remote_temp)]
        try:
            result = subprocess.run(scp_args, capture_output=True, encoding="utf-8")
        except OSError as exc:
            raise SyncError("scp %s: %s" % (peer, exc))
        if result.returncode != 0:
            detail = (result.stderr or result.stdout or "").strip()
            raise SyncError("scp %s failed: %s" % (peer, detail or ("exit %d" % result.returncode)))
        command = "chmod 600 %s && mv %s %s" % (
            shlex.quote(remote_temp),
            shlex.quote(remote_temp),
            shlex.quote(remote_file),
        )
        run_ssh(peer, command)
        moved = True
    finally:
        try:
            os.unlink(local_temp)
        except OSError:
            pass
        if not moved:
            try:
                run_ssh(peer, "rm -f %s" % shlex.quote(remote_temp))
            except SyncError:
                pass


def sync(args):
    local_document = load_document(args.local_file)
    validate_document(local_document, "local")
    remote_document = fetch_remote(args.peer, args.remote_file)
    remote_exists = remote_document is not None
    if remote_document is None:
        remote_document = {"version": 1, "descriptions": {}}
    merged, stats = merge_documents(local_document, remote_document)
    print(
        "%s description sync: local=%d remote=%d merged=%d local-only=%d remote-only=%d conflicts=%d"
        % (
            utc_now(),
            stats["local"],
            stats["remote"],
            stats["merged"],
            stats["local_only"],
            stats["remote_only"],
            len(stats["conflicts"]),
        )
    )
    for conflict in stats["conflicts"]:
        print(
            "%s conflict %s %s: local=%s remote=%s chose=%s"
            % (
                utc_now(),
                conflict["key"],
                conflict["title"],
                conflict["local"],
                conflict["remote"],
                conflict["chosen"],
            )
        )
    if not (stats["changed_local"] or stats["changed_remote"]):
        print("%s no changes" % utc_now())
        return
    if args.dry_run:
        print("%s would write both files" % utc_now())
        return
    data = serialize_document(merged)
    local_backup = backup_local(args.local_file)
    remote_backup = backup_remote(args.peer, args.remote_file) if remote_exists else None
    write_atomic(args.local_file, data)
    write_remote_atomic(args.peer, args.remote_file, data)
    print(
        "%s wrote local (backup %s) and remote (backup %s)"
        % (utc_now(), local_backup or "none", remote_backup or "none")
    )


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--peer", required=True)
    parser.add_argument("--local-file", default=DEFAULT_FILE)
    parser.add_argument("--remote-file", default=DEFAULT_FILE)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args(argv)
    if not args.peer.strip():
        parser.error("--peer is required")
    try:
        sync(args)
    except SyncError as exc:
        print("description sync failed: %s" % exc, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
