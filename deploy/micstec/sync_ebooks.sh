#!/usr/bin/env bash
#
# sync_ebooks.sh — keep the two BookBrowser ebook libraries in sync.
#
#   ebook.micstec.com  (local,  ip-172-31-1-73)   <->  aws11.micsapp.com (ip-172-31-87-175)
#
# Bidirectional and add-only: books added on either host are copied to the
# other, but removals are never propagated, so a sync can never delete a book.
# Only ebook files (*.epub/*.pdf/*.mobi/*.azw/*.azw3) are transferred. The
# supplemental introduction file (.bookbrowser/book-descriptions.json) is
# merged separately; other per-host .bookbrowser state (auth DB, google.env,
# app.env) is never touched.
#
# The same script runs on either host; it auto-detects which host it is on and
# syncs with its peer. Run on demand with:
#   ./sync_ebooks.sh
#   ./sync_ebooks.sh --dry-run
#
# A daily cron job on the local host invokes this script. Set SYNC_PEER=<host>
# to override peer auto-detection.
#
# After the files are synced, both BookBrowser servers are told to rescan the
# library (SIGUSR1 triggers a reindex, no auth or restart required), so newly
# synced books appear in the catalog immediately.

set -euo pipefail

BOOK_DIR="/home/mli/books"
LOCAL_HOST="ec2-54-165-17-203.compute-1.amazonaws.com"   # ebook.micstec.com
AWS11_HOST="aws11.micsapp.com"
LOG_FILE="$BOOK_DIR/.bookbrowser/sync_ebooks.log"
LOCK_FILE="$BOOK_DIR/.bookbrowser/sync_ebooks.lock"
DESC_SYNC="$BOOK_DIR/sync_book_descriptions.py"

DRY=""
[ "${1:-}" = "--dry-run" ] && DRY="--dry-run"

log() { printf '%s %s\n' "$(date '+%F %T')" "$*" >> "$LOG_FILE"; }

# Determine which host we are on and pick the peer.
me="$(hostname -f 2>/dev/null || hostname)"
peer=""
case "$me" in
    *ip-172-31-1-73*)   peer="$AWS11_HOST" ;;
    *ip-172-31-87-175*) peer="$LOCAL_HOST" ;;
    *)                  peer="${SYNC_PEER:-}" ;;
esac
if [ -z "$peer" ]; then
    echo "ERROR: cannot determine peer host (hostname '$me'); set SYNC_PEER=<host>." >&2
    exit 1
fi

# Process pattern of the BookBrowser server on each host (used to send SIGUSR1).
# The '[B]' bracket trick keeps pkill from matching its own command line. The
# TTS server (tts_server.py) is deliberately excluded by these patterns.
case "$me" in
    *ip-172-31-1-73*)   MY_PAT='[B]ookBrowser --addr';          PEER_PAT='[B]ookBrowser-linux-64bit' ;;
    *ip-172-31-87-175*) MY_PAT='[B]ookBrowser-linux-64bit';     PEER_PAT='[B]ookBrowser --addr' ;;
    *)                  MY_PAT='[B]ookBrowser --addr';          PEER_PAT='[B]ookBrowser-linux-64bit' ;;
esac

# Avoid overlapping syncs (cron + manual).
exec 9>"$LOCK_FILE"
flock -n 9 || { echo "Another sync is already running; skipping." >&2; exit 0; }

RSYNC=(rsync -a --no-owner --no-group --update --stats --timeout=120 $DRY
    -e "ssh -o BatchMode=yes -o ConnectTimeout=15"
    --exclude='.bookbrowser/'
    --exclude='__pycache__/'
    --exclude='*.pyc'
    --include='*/'
    --include='*.epub'  --include='*.EPUB'
    --include='*.pdf'   --include='*.PDF'
    --include='*.mobi'  --include='*.MOBI'
    --include='*.azw'   --include='*.AZW'
    --include='*.azw3'  --include='*.AZW3'
    --exclude='*')

log "sync start: $me -> $peer${DRY:+ (dry-run)}"
"${RSYNC[@]}" "$BOOK_DIR/" "$peer:$BOOK_DIR/"
"${RSYNC[@]}" "$peer:$BOOK_DIR/" "$BOOK_DIR/"
log "sync complete: $me <-> $peer"
echo "Sync complete ($me <-> $peer)${DRY:+ (dry-run)}."

desc_status=0
if [ -x "$DESC_SYNC" ]; then
    desc_args=(--peer "$peer")
    if [ -n "$DRY" ]; then
        desc_args+=(--dry-run)
    fi
    if "$DESC_SYNC" "${desc_args[@]}"; then
        log "description sync complete"
    else
        echo "WARN: description sync failed." >&2
        log "description sync failed"
        desc_status=1
    fi
else
    echo "WARN: $DESC_SYNC not found; description sync skipped." >&2
    log "description sync skipped: helper not found"
    desc_status=1
fi

# Rescan both servers so newly synced books show up in the catalog. SIGUSR1
# triggers a reindex without requiring authentication or a restart. Skipped
# for --dry-run.
if [ -z "$DRY" ]; then
    if pkill -USR1 -f "$MY_PAT" 2>/dev/null; then
        echo "Local server rescan triggered."
        log "rescan: sent SIGUSR1 to local server"
    else
        echo "WARN: no local BookBrowser process found; rescan skipped."
        log "rescan: no local BookBrowser process found"
    fi
    if ssh -o BatchMode=yes -o ConnectTimeout=15 "$peer" "pkill -USR1 -f '$PEER_PAT' 2>/dev/null"; then
        echo "Peer server ($peer) rescan triggered."
        log "rescan: sent SIGUSR1 to peer server ($peer)"
    else
        echo "WARN: no BookBrowser process found on peer; rescan skipped."
        log "rescan: no BookBrowser process found on peer"
    fi
fi

exit "$desc_status"
