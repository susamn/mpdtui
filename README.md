# mpdtui

A lazygit-style terminal UI for MPD.

## Build

```bash
go build -o mpdtui ./cmd/mpdtui
```

## Run

```bash
./mpdtui          # full panel UI
./mpdtui -mini    # lightweight inline player (single status line)
```

Connects using the same environment variables as `mpc`: `MPD_HOST`
(default `localhost`, may be `password@host`) and `MPD_PORT` (default
`6600`).

Press `?` inside the full UI for the full keybinding list.

## Test

```bash
go test ./...
```

`internal/mpdclient` and `internal/ui`'s integration tests need a
reachable MPD server (they skip automatically if there isn't one).

## License

MIT, see [LICENSE](LICENSE).
