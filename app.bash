#!/usr/bin/env bash
# app.bash — operational helpers for the live-streaming project.
#
# Run from the project root:
#     ./app.bash <subcommand> [args]
#
# Run `./app.bash help` (or no args) for the subcommand list.

set -euo pipefail

PORT="${PORT:-9000}"
LOG="${LOG:-/tmp/ls.log}"
PIDFILE="${PIDFILE:-/tmp/ls.pid}"

# ---------- server lifecycle ----------

cmd_build() {
    go build ./...
    go vet ./...
    echo "build + vet OK"
}

cmd_run() {
    go run .
}

# Run the server in the background, redirect logs to $LOG, then wait until
# the listener is up so subsequent `through`/`probe` calls don't race.
cmd_run_bg() {
    cmd_stop >/dev/null 2>&1 || true
    go run . > "$LOG" 2>&1 &
    echo $! > "$PIDFILE"
    echo "started: pid=$(cat "$PIDFILE") log=$LOG"
    cmd_wait_ready
}

cmd_stop() {
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null \
        | awk 'NR>1 {print $2}' | xargs -r kill 2>/dev/null || true
    pkill -f "go-build.*main" 2>/dev/null || true
    pkill -f "cloudflared tunnel.*localhost:$PORT" 2>/dev/null || true
    sleep 1
    if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | grep -q LISTEN; then
        echo "WARN: something still listening on :$PORT"
    else
        echo "stopped (port $PORT free)"
    fi
}

cmd_logs() {
    tail -f "$LOG"
}

cmd_wait_ready() {
    for _ in $(seq 1 30); do
        grep -q "server is running" "$LOG" 2>/dev/null && return 0
        sleep 1
    done
    echo "timeout waiting for server to bind :$PORT" >&2
    return 1
}

# ---------- proxy diagnostics ----------

# Probe a remote HLS URL directly (without going through our proxy).
# Useful for confirming an upstream is reachable + multi-variant before
# adding it to externalChannels in cmd/config/config-local.yaml.
cmd_probe() {
    local url="${1:?url required}"
    echo "=== HEAD ==="
    curl -sS -m 8 -A "Mozilla/5.0" --compressed -o /tmp/p.m3u8 \
         -w "code=%{http_code} type=%{content_type} bytes=%{size_download}\n" "$url"
    if grep -q "EXT-X-STREAM-INF" /tmp/p.m3u8 2>/dev/null; then
        echo ""
        echo "=== variants ==="
        grep -E "RESOLUTION=|^#EXT-X-STREAM-INF" /tmp/p.m3u8 | head -20
    elif grep -q "EXTINF" /tmp/p.m3u8 2>/dev/null; then
        echo "(single-quality HLS — no variants listed in master)"
    fi
}

# Exercise a configured external channel end-to-end through the proxy:
# master -> first variant -> first segment.
cmd_through() {
    local ch="${1:?channel name required (e.g. mux-live)}"
    echo "=== /stream/$ch.m3u8 ==="
    curl -s -o /tmp/m.m3u8 \
         -w "  master: code=%{http_code} bytes=%{size_download}\n" \
         "http://localhost:$PORT/stream/$ch.m3u8"
    echo "  variants:"
    grep -E "^#EXT-X-STREAM-INF" /tmp/m.m3u8 \
        | grep -oE 'RESOLUTION=[0-9]+x[0-9]+|BANDWIDTH=[0-9]+' \
        | paste - - | sed 's/^/    /'
    local var
    var="$(awk '!/^#/ && NF' /tmp/m.m3u8 | head -1)"
    [ -z "$var" ] && return
    curl -s -o /tmp/v.m3u8 \
         -w "  variant: code=%{http_code} bytes=%{size_download}\n" \
         "http://localhost:$PORT$var"
    local seg
    seg="$(awk '!/^#/ && NF' /tmp/v.m3u8 | head -1)"
    [ -z "$seg" ] && return
    curl -s -o /dev/null \
         -w "  segment: code=%{http_code} bytes=%{size_download}\n" \
         "http://localhost:$PORT$seg"
}

# List the channels the running server is currently exposing.
cmd_channels() {
    curl -s "http://localhost:$PORT/api/external" \
        | python3 -m json.tool 2>/dev/null \
        || curl -s "http://localhost:$PORT/api/external"
}

# Resolve a YouTube watch URL to its current HLS master via yt-dlp — the
# same call the proxy makes for kind:youtube channels.
cmd_resolve_yt() {
    local url="${1:?youtube watch URL required}"
    yt-dlp -g --no-warnings -f "best[protocol*=m3u8]/best" "$url"
}

# ---------- ffmpeg HLS segmenting for the on-disk TV / FM channels ----------

# Segment a video into media/tv/<show>/index.m3u8 (+ seg00000.ts ...).
# The Go server's stream module picks these up automatically and loops
# them as the 24/7 TV channel (multi-show mode).
cmd_segment_tv() {
    local show="${1:?show name (e.g. 01_intro)}"
    local file="${2:?source video file}"
    mkdir -p "media/tv/$show"
    ffmpeg -i "$file" \
        -c:v libx264 -c:a aac \
        -hls_time 6 -hls_playlist_type vod \
        -hls_segment_filename "media/tv/$show/seg%05d.ts" \
        "media/tv/$show/index.m3u8"
}

# Segment an audio file into media/fm/<show>/index.m3u8 — same pattern
# as TV but audio-only. Powers the 24/7 FM radio channel.
cmd_segment_fm() {
    local show="${1:?show name (e.g. 01_morning_set)}"
    local file="${2:?source audio file}"
    mkdir -p "media/fm/$show"
    ffmpeg -i "$file" \
        -c:a aac -vn \
        -hls_time 6 -hls_playlist_type vod \
        -hls_segment_filename "media/fm/$show/seg%05d.ts" \
        "media/fm/$show/index.m3u8"
}

# ---------- prerequisites ----------

# brew-install the optional dependencies the server needs at runtime.
cmd_install() {
    local missing=()
    command -v cloudflared >/dev/null 2>&1 || missing+=(cloudflared)
    command -v yt-dlp      >/dev/null 2>&1 || missing+=(yt-dlp)
    command -v ffmpeg      >/dev/null 2>&1 || missing+=(ffmpeg)
    if [ ${#missing[@]} -eq 0 ]; then
        echo "all prereqs already installed"
        return
    fi
    if ! command -v brew >/dev/null 2>&1; then
        echo "Homebrew not found; install manually: ${missing[*]}" >&2
        return 1
    fi
    brew install "${missing[@]}"
}

# ---------- dispatch ----------

cmd_help() {
    cat <<EOF
usage: $0 <subcommand> [args]

server lifecycle:
  build                       go build + go vet
  run                         run server in foreground
  run-bg                      run in background, redirect logs to \$LOG, wait until ready
  stop                        kill anything listening on :\$PORT and lingering cloudflared
  logs                        tail -f \$LOG

proxy diagnostics:
  probe <url>                 probe a remote HLS URL directly (master + variants)
  through <channel>           hit master -> variant -> segment via the local proxy
  channels                    list /api/external from the running server
  resolve-yt <youtube-url>    run yt-dlp -g (same call kind:youtube channels make)

24/7 channels (ffmpeg pre-segmenting):
  segment-tv <show> <file.mp4>   pre-segment a video into media/tv/<show>/
  segment-fm <show> <file.mp3>   pre-segment audio into media/fm/<show>/

prerequisites:
  install                     brew-install cloudflared, yt-dlp, ffmpeg if missing

env vars (override before invoking):
  PORT      default 9000
  LOG       default /tmp/ls.log
  PIDFILE   default /tmp/ls.pid
EOF
}

case "${1:-help}" in
    build)         cmd_build ;;
    run)           cmd_run ;;
    run-bg)        cmd_run_bg ;;
    stop)          cmd_stop ;;
    logs)          cmd_logs ;;
    probe)         shift; cmd_probe "$@" ;;
    through)       shift; cmd_through "$@" ;;
    channels)      cmd_channels ;;
    resolve-yt)    shift; cmd_resolve_yt "$@" ;;
    segment-tv)    shift; cmd_segment_tv "$@" ;;
    segment-fm)    shift; cmd_segment_fm "$@" ;;
    install)       cmd_install ;;
    help|-h|--help) cmd_help ;;
    *)             echo "unknown subcommand: $1" >&2; cmd_help; exit 2 ;;
esac
