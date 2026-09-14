# Package dependencies

Seven source files cite this document as the authority for why they
duplicate a few lines rather than add an import. This is that document:
the rules those comments are appealing to, and the reasoning behind
them.

The graph below is generated from the code, not maintained by hand.
Regenerate it after any change to the imports:

```sh
for p in $(go list ./...); do
  printf '%-22s -> %s\n' "${p#mpdtui/}" \
    "$(go list -f '{{join .Imports "\n"}}' "$p" | grep '^mpdtui/' \
       | sed 's|mpdtui/internal/||;s|mpdtui/||' | tr '\n' ' ')"
done
```

## The graph

```
cmd/mpdtui  -> config lyricsline metadata mini mpdclient picker trackinfo ui version

  ENTRY POINTS (one per mode, siblings, none imports another)
    ui         -> albumart lyrics lyricsindex metadata mpdclient
                  settingsview textutil uitheme version visualizer
    mini       -> metadata mpdclient theme
    picker     -> mpdclient theme

  UI COMPONENTS (own a panel or overlay, used only by ui)
    albumart     -> mpdclient
    visualizer   -> audio mpdclient
    settingsview -> metadata uitheme

  RENDERING
    uitheme    -> theme        (palette -> tcell colors, for tview front ends)

  DOMAIN
    lyricsindex -> lyrics textutil
    lyricsline  -> lyrics mpdclient
    trackinfo   -> lyrics metadata mpdclient
    lyrics      -> textutil
    metadata    -> textutil
    mpdclient   -> config
    config      -> kvparser theme
    theme       -> kvparser

  LEAVES (no internal dependencies at all)
    audio  kvparser  textutil  version
```

No cycles. Nothing above imports anything below it back.

## The rules

### 1. Only `cmd/mpdtui` and `internal/mpdclient` import `internal/config`

Everything else receives settings as plain resolved values. `cmd/mpdtui`
reads the config once at startup and passes the answers down --
`ui.Run`'s `musicDir` and `metaDB` arguments, and every field of
`ui.ConfigSummary`, exist for this reason.

The point is that a package can be constructed and tested without a
config file on disk, and that "where does this setting come from" has
one answer instead of one per caller. `mpdclient` is the exception
because connection details are read at dial time.

Cited by: `ui/settings.go` (`ConfigSummary`), `ui/lyrics.go`
(hardcodes a config path in one message rather than import the package).

A consequence worth stating: a component under `ui` that needs a
setting takes it as a constructor argument. `visualizer.New(fifoPath)`
is the worked example -- it used to call `config.LoadVisualizerFIFO()`
itself.

### 2. The three entry points are siblings and never import each other

`ui`, `mini` and `picker` are alternative front ends over the same
client, selected by flag in `main.go`. None may import another.

`mini` in particular exists to render without `tcell`/`tview` at all --
it writes bare ANSI escapes -- so an import of `ui` would defeat its
reason for existing. That is why `mini/format.go` has its own
`formatDuration` and `progressBar`, and why `ui/globalsearch.go` has
its own fuzzy matcher rather than importing `picker`'s.

Cited by: `mini/format.go`, `mini/mini_test.go`, `picker/picker.go`,
`ui/globalsearch.go`.

### 3. Shared behavior goes to a leaf, not sideways

When two of the siblings need the same thing, it moves down into a
package all of them may import -- `theme` for the palette, `textutil`
for normalization -- never across from one sibling to another.

`theme` is the worked example: it holds the palette as data, and each
sibling turns it into whatever its own renderer needs.

`uitheme` is the second, one level up: it is the palette rendered into
`tcell.Color`, which is what a tview front end needs. `ui` uses it, and
so may any panel extracted out of `ui` -- that is the point of it being
a package rather than unexported state inside `ui`. `mini` must stay
clear of it, because it imports tcell and mini's whole reason for
existing is rendering without tcell; `mini` goes to `theme` directly.
`picker` is tview-based and could adopt it, which would retire the
`hexColor`/`contrastColor` duplication in rule 4's table.

### 4. Small duplication beats a new edge

The rules above are worth a few duplicated lines, and the codebase
makes that trade deliberately in four places -- each one carrying a
comment saying so, which is the actual requirement. Duplication is
allowed here when it is small, stable, and commented; it is not a
general licence to copy.

| Duplicated | Where | Instead of importing |
|---|---|---|
| `formatDuration`, `progressBar` | `mini/format.go` | `ui` |
| fuzzy subsequence matcher | `ui/globalsearch.go` | `picker` |
| path normalization | `metadata/metadata.go` | `lyrics` |
| palette -> ANSI escapes | `mini` | `uitheme` (it is tcell-based) |
| `hexColor`, `contrastColor` | `picker` | `uitheme` |

If one of these grows or starts drifting in behavior, that is the
signal to move it down to a leaf (rule 3) instead.

## Adding a package

- Does it belong under an entry point, or below all of them? Something
  only `ui` uses (`albumart`, `visualizer`) sits under `ui`. Something
  two entry points need is a leaf.
- A panel pulled out of `ui` takes its colors from `uitheme`, not from
  constructor arguments -- they change under it on a theme reload.
- Everything else it needs is a `Deps` struct of plain values and
  callbacks, never the `App`. `settingsview.Deps` is the worked example:
  six fields, each documented with why the view needs it. If a panel
  cannot name what it needs, that is the finding, not a reason to pass
  the `App`.
- Take dependencies as constructor arguments. A package that reads its
  own config or opens its own database cannot be tested without one.
- Check the graph still has no cycles, and that rule 1 still holds:

```sh
for p in $(go list ./...); do
  go list -f '{{join .Imports "\n"}}' "$p" \
    | grep -q '^mpdtui/internal/config$' && echo "imports config: $p"
done
# expected: cmd/mpdtui and internal/mpdclient, nothing else
```
