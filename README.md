<p align="center">
  <img src="assets/logo.svg" width="140" alt="mpdtui logo">
</p>

<h1 align="center">mpdtui</h1>

<p align="center">
  A <a href="https://github.com/jesseduffield/lazygit">lazygit</a>-style terminal UI for
  <a href="https://www.musicpd.org/">MPD</a> (Music Player Daemon).
</p>

<p align="center">
  <img alt="Go version" src="https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white">
  <img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg">
</p>

---

Bordered panels, single-key contextual actions, vim-style navigation — no
mouse required. Two run modes: a full panel UI, and a lightweight
single-line inline player for a shell or tmux pane.

## Features

- **Library** — browse Artist → Album → Track, or free-text search
- **Playlists** — load, append, save, delete stored playlists
- **Queue** — reorder, remove, clear, jump to any track
- **Now Playing bar** — live progress, volume, repeat/random/single/consume
  flags, updated instantly via MPD's `idle` protocol (stays in sync even
  when playback changes from another client, e.g. `mpc`)
- **Lightweight inline mode** (`-mini`) — a single status line, no
  alt-screen takeover, for tmux status panes or a quick glance
- Confirmation prompts on destructive actions (clear queue, delete playlist)

## Install

```bash
go build -o mpdtui ./cmd/mpdtui
```

Requires Go 1.26+ and a reachable MPD server.

## Usage

```bash
./mpdtui          # full panel UI
./mpdtui -mini    # lightweight inline player (single status line)
```

Connects using the same environment variables as `mpc`:

| Variable | Default | Notes |
|---|---|---|
| `MPD_HOST` | `localhost` | may be `password@host` |
| `MPD_PORT` | `6600` | |

Press `?` inside the full UI for the in-app keybinding list.

## Keybindings

**Global** (any panel):

| Key | Action |
|---|---|
| `Space` | Toggle play/pause |
| `s` | Stop |
| `n` / `p` | Next / previous track |
| `,` / `.` | Seek -5s / +5s |
| `-` / `=` | Volume down / up |
| `z` | Toggle random (shuffle) |
| `x` | Toggle repeat |
| `c` | Toggle consume |
| `Z` | Toggle single |
| `Tab`, `1`/`2`/`3` | Cycle / jump focus between panels |
| `/` | Search (contextual: Library or Playlists) |
| `?` | Help overlay |
| `q` | Quit |

**Panel-local**:

| Panel | Key | Action |
|---|---|---|
| Library | `Enter` | Drill into artist/album, or add+play a track |
| Library | `a` | Add selected item to queue (no play) |
| Library | `Backspace` | Go back up a level |
| Playlists | `Enter` | Load playlist into queue and play |
| Playlists | `a` | Append playlist to queue |
| Playlists | `d` | Delete playlist (confirm) |
| Playlists | `S` | Save current queue as a new playlist |
| Queue | `Enter` | Play selected track |
| Queue | `d` | Remove selected track |
| Queue | `J` / `K` | Move selected track down / up |
| Queue | `D` | Clear entire queue (confirm) |

**Mini mode** (`-mini`): `Space` play/pause, `n`/`p` next/prev, `s` stop,
`-`/`=` volume, `q`/`Ctrl-C` quit.

## Test

```bash
go test ./...
```

`internal/mpdclient` and `internal/ui`'s integration tests need a
reachable MPD server (they skip automatically if there isn't one).

## License

MIT, see [LICENSE](LICENSE).
