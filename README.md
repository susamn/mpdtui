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

## Demo

<p align="center">
  <a href="https://github.com/susamn/mpdtui/raw/main/assets/demo.webm">
    <img src="assets/demo-thumb.png" width="720" alt="mpdtui demo -- click to play the video">
  </a>
  <br>
  <sub>Click the screenshot to play the demo (GitHub doesn't inline-render repo video files).</sub>
</p>

## Features

- **Library** — browse Artist → Album → Track, or free-text search
- **Playlists** — load, append, save, delete stored playlists
- **Queue** — reorder, remove, clear, jump to any track
- **Now Playing bar** — live progress, volume, repeat/random/single/consume
  flags, updated instantly via MPD's `idle` protocol (stays in sync even
  when playback changes from another client, e.g. `mpc`)
- **Lightweight inline mode** (`-mini`) — two live status lines (queue/
  playlist counts, then track/progress), no alt-screen takeover, for
  tmux status panes or a quick glance
- **Fuzzy pickers** (`-p` / `-t`) — fzf-style playlist/track search from
  the shell, no panels involved
- Confirmation prompts on destructive actions (clear queue, delete playlist)

## Install

### Homebrew

```bash
brew tap susamn/mpdtui
brew install mpdtui
```

(First install of a third-party tap: if Homebrew refuses to load the
formula as untrusted, run `brew trust susamn/mpdtui` first.)

### From source

```bash
go build -o mpdtui ./cmd/mpdtui
```

Requires Go 1.26+ and a reachable MPD server.

## Usage

```bash
./mpdtui          # full panel UI
./mpdtui -mini    # lightweight inline player
./mpdtui -p       # fuzzy-search playlists; Enter clears the queue and plays it
./mpdtui -t       # fuzzy-search tracks; Enter adds it to the queue and plays it
```

`-mini`, `-p`, and `-t` are mutually exclusive.

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
| `D` | Clear entire queue (confirm) |
| `Tab`, `1`/`2`/`3` | Cycle / jump focus between panels |
| `/` | Search (contextual: filters Library/Playlists, jumps to a match in Queue) |
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
| Playlists | `Esc` | Clear active filter |
| Queue | `Enter` | Play selected track |
| Queue | `d` | Remove selected track |
| Queue | `J` / `K` | Move selected track down / up |
| Queue | `/` | Search: "Search track:" box above the queue, Enter jumps to first match (Esc cancels) |

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
