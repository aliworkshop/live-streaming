# live-streaming

A Go service that delivers three live-media features behind one HTTP server:

1. **User accounts** — signup/login with JWT-issued tokens.
2. **Two-party voice + video calls** — WebRTC peer-to-peer between any two signed-in users, with a Go signaling hub over
   WebSocket.
3. **24/7 channels** — a TV channel and an FM-radio channel that loop pre-segmented HLS content forever.

## Architecture

Mirrors the `sample_project` clean-architecture layout: an `app/` package that wires everything together, and one
feature module per domain with `domain / usecase / delivery` (and `repository` / `client` where applicable).

```
live-streaming/
├── main.go
├── app/                       # wiring (config, core, modules, routes, start)
├── user/
│   ├── init.go                # module exports
│   ├── domain/                # entities + UserUc / Repository interfaces
│   ├── auth/                  # JWT tokener (split to avoid import cycle)
│   ├── repository/            # in-memory store (swap for SQL when ready)
│   ├── usecase/               # bcrypt + token issuance
│   └── delivery/              # HTTP signup/login/me/list + AuthMiddleware
├── call/
│   ├── init.go
│   ├── domain/                # Signal, Peer, CallUc
│   ├── client/                # ws read/write pumps per peer
│   ├── usecase/               # signaling hub (forward, broadcast peers)
│   └── delivery/              # /ws/call upgrade with token check
├── stream/
│   ├── init.go
│   ├── domain/                # Segment, ChannelKind, StreamUc
│   ├── usecase/               # VOD playlist parser + sliding-window looper
│   └── delivery/              # HLS playlist + segment HTTP handlers
└── web/                       # static client (signup, call, tv, radio)
```

## Running it

```fish
cd /Users/alitorabi/go/src/aliworkshop/live-streaming
go run .
```

The server listens on `:8080` by default. Open two browsers (or a normal + incognito window) at `http://localhost:8080`,
sign up two users, then go to `/call.html` in each and click the other peer to start a video + voice call.

### Configuration

Environment overrides:

| Var                   | Default             | Purpose                                                 |
|-----------------------|---------------------|---------------------------------------------------------|
| `JWT_SECRET`          | `change-me-please`  | HMAC key for access tokens. **Set this in production.** |
| `MEDIA_DIR`           | `media`             | Root directory for pre-segmented channels.              |
| `TV_FILE` / `FM_FILE` | `tv.mp4` / `fm.mp3` | Reserved for future single-file fallback.               |
| `WEB_DIR`             | `web`               | Directory served as the static client.                  |

The HTTP listen address (`:8080`), JWT expiry (24h), and graceful-shutdown timeout (10s) live in `app/config.go`.

## 24/7 channels — pre-segment your media

The Go server doesn't transcode; `ffmpeg` pre-segments each video/audio source once, then the server loops the segments
forever as a live HLS stream by advancing `EXT-X-MEDIA-SEQUENCE` over time and wrapping around the segment list.

A channel can be either a **single show** that loops by itself, or a **playlist of shows** that play back-to-back and
then loop.

### Multi-show mode (recommended)

Each show lives in its own subdirectory under the channel folder. Shows play in **alphabetical order** of directory
name (so prefix with `01_`, `02_`, … to control order). The server merges them into one long sequence and inserts
`#EXT-X-DISCONTINUITY` at every show boundary, so codec/resolution differences between shows don't break players.

```
media/
├── tv/
│   ├── 01_intro/
│   │   ├── index.m3u8
│   │   └── seg00000.ts …
│   ├── 02_main/
│   │   ├── index.m3u8
│   │   └── seg00000.ts …
│   └── 03_outro/
│       ├── index.m3u8
│       └── seg00000.ts …
└── fm/
    ├── 01_morning_set/
    │   ├── index.m3u8
    │   └── seg00000.ts …
    └── 02_evening_set/
        ├── index.m3u8
        └── seg00000.ts …
```

Pre-segment each show:

```fish
mkdir -p media/tv/01_intro
ffmpeg -i intro.mp4 -c:v libx264 -c:a aac -hls_time 6 -hls_playlist_type vod -hls_segment_filename 'media/tv/01_intro/seg%05d.ts' media/tv/01_intro/index.m3u8

mkdir -p media/tv/02_main
ffmpeg -i main.mp4 -c:v libx264 -c:a aac -hls_time 6 -hls_playlist_type vod -hls_segment_filename 'media/tv/02_main/seg%05d.ts' media/tv/02_main/index.m3u8

mkdir -p media/fm/01_morning_set
ffmpeg -i morning.mp3 -c:a aac -vn -hls_time 6 -hls_playlist_type vod -hls_segment_filename 'media/fm/01_morning_set/seg%05d.ts' media/fm/01_morning_set/index.m3u8
```

To add a new show later, drop a new subdirectory in and restart the server — no re-encoding of existing shows.

### Single-show mode

If `media/tv/index.m3u8` exists directly (no subdirectories), the server treats the whole `media/tv/` folder as one show
that loops on its own. Same for `media/fm/`. This mode wins over multi-show if both exist.

```fish
mkdir -p media/tv
ffmpeg -i source.mp4 -c:v libx264 -c:a aac -hls_time 6 -hls_playlist_type vod -hls_segment_filename 'media/tv/seg%05d.ts' media/tv/index.m3u8
```

### After pre-segmenting

Restart `go run .` and visit `/tv.html` / `/radio.html`. Startup logs each channel's show and segment count. If a
channel directory is missing or empty, the server logs a warning and the corresponding playlist endpoint returns `503`
until you populate it.

## Endpoints

| Method | Path                  | Purpose                                            |
|--------|-----------------------|----------------------------------------------------|
| POST   | `/api/signup`         | `{username,password,displayName}` → `{token,user}` |
| POST   | `/api/login`          | `{username,password}` → `{token,user}`             |
| GET    | `/api/me`             | requires `Authorization: Bearer <token>`           |
| GET    | `/api/users`          | list users (auth)                                  |
| WS     | `/ws/call?token=…`    | WebRTC signaling hub                               |
| GET    | `/stream/tv.m3u8`     | TV live playlist                                   |
| GET    | `/stream/tv/<seq>.ts` | TV segment                                         |
| GET    | `/stream/fm.m3u8`     | Radio live playlist                                |
| GET    | `/stream/fm/<seq>.ts` | Radio segment                                      |

The signaling protocol is a tiny JSON envelope (`type`, `from`, `to`, `payload`) carrying `peers`, `call-request`,
`call-accept`, `call-reject`, `call-end`, `offer`, `answer`, `ice`, `peer-offline`, and `error` messages. See
`call/domain/entities.go`.

## Notes / things you'll likely want to swap

- `user/repository/memory.go` is in-memory — drop in a `dbcore`-backed implementation when ready (the
  `domain.Repository` interface is the seam).
- App wiring uses `net/http` instead of `aliworkshop/echoserver` + `gateway` since this is a fresh standalone module; if
  you'd rather live inside the gateway/authorizer/configer ecosystem, the seams are `app/core.go` (engine) and the
  `app.initJwt` hook (jwt → authorizer).
- The call signaling server is pure relay — peers connect P2P via STUN. Behind symmetric NATs you'd add a TURN server (
  configure it on the client side in `web/call.js`).
- For production-grade 24/7 streaming consider low-latency variants (LL-HLS / DASH) and a CDN; the current setup is
  solid for a self-hosted radio/TV.

## Verifying the build

```fish
go build ./...
go vet ./...
```

Both pass clean.
