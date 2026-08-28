// Package lyricsindex maintains a persistent, on-demand search index of
// the plain text of every track's .txt/.lrc lyrics sidecar (see
// internal/lyrics), stored as a single SQLite file in mpdtui's config
// directory.
//
// It exists so the interactive lyrics search ("l" in the global-search
// popup) never has to walk the music directory itself: that walk is
// thousands of ReadDir/ReadFile syscalls and visibly stalls the UI on a
// large library. Instead the user rebuilds the index explicitly (a
// keypress, with a progress display), and every search is then a single
// sequential read of this file with the matching done in memory.
//
// Reindex is incremental: a sidecar whose modification time and kind set
// still match the row already stored for it is not re-read, so a rebuild
// after adding lyrics for a handful of new tracks only touches those
// tracks.
package lyricsindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	_ "modernc.org/sqlite"

	"mpdtui/internal/lyrics"
)

// schemaVersion is bumped only on an incompatible change to the entries
// table; Open wipes and recreates the index when it finds an older one,
// since the index is a pure derived cache -- losing it just means the
// user rebuilds.
const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS entries (
	file        TEXT PRIMARY KEY,
	display     TEXT NOT NULL,
	kinds       TEXT NOT NULL,
	mtime_ns    INTEGER NOT NULL,
	text_folded TEXT NOT NULL
);
`

// Track is the minimal per-song input Reindex needs: MPD's own
// forward-slash-relative file path, and a display label to snapshot so
// search hints don't need a second MPD round-trip to render.
type Track struct {
	File    string
	Display string
}

// Entry is one indexed track, as returned by Load.
type Entry struct {
	File    string
	Display string
	// TextFolded is the sidecar text lowercased with diacritics stripped
	// (see Fold) -- ready to match a Fold'd query against with a plain
	// strings.Contains, no per-search re-folding of the whole corpus.
	TextFolded string
}

// Info summarises the current on-disk index.
type Info struct {
	Exists    bool
	MusicDir  string
	IndexedAt time.Time
	Count     int
}

// Stats summarises a Reindex run.
type Stats struct {
	Tracks    int // tracks considered (len(tracks))
	Indexed   int // rows in the index afterwards
	Read      int // sidecars actually read (new or changed) this run
	Unchanged int // sidecars skipped because mtime+kinds matched the stored row
	Removed   int // rows deleted (track gone from the library, or lost its sidecar)
	Elapsed   time.Duration
}

// Progress, when non-nil, is called periodically during Reindex with how
// many of total tracks have been examined so far (and once more at the
// end with done == total). It runs on Reindex's own goroutine.
type Progress func(done, total int)

// progressEvery is how often (in tracks examined) Reindex fires Progress
// -- coarse enough not to swamp a UI's redraw queue, fine enough to feel
// live on a big library.
const progressEvery = 128

func open(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("lyricsindex: no index path configured")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	var stored int
	err = db.QueryRow(`SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'schema_version'`).Scan(&stored)
	if err != nil && err != sql.ErrNoRows {
		db.Close()
		return nil, err
	}
	if stored != schemaVersion {
		if _, err := db.Exec(`DELETE FROM entries`); err != nil {
			db.Close()
			return nil, err
		}
		if _, err := db.Exec(
			`INSERT INTO meta (key, value) VALUES ('schema_version', ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			fmt.Sprint(schemaVersion),
		); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

// Load returns every indexed entry, in no particular order. A missing
// index file is not an error -- it opens as an empty one -- so callers
// distinguish "no index yet" via Stat/ReadInfo, not a Load failure.
func Load(dbPath string) ([]Entry, error) {
	db, err := open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT file, display, text_folded FROM entries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.File, &e.Display, &e.TextFolded); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReadInfo reports what the on-disk index currently holds, for a
// "last built: ..." status line. A never-built index reports Exists
// false with the rest zeroed.
func ReadInfo(dbPath string) (Info, error) {
	if dbPath == "" {
		return Info{}, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return Info{}, nil
		}
		return Info{}, err
	}
	db, err := open(dbPath)
	if err != nil {
		return Info{}, err
	}
	defer db.Close()

	info := Info{Exists: true}
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&info.Count); err != nil {
		return Info{}, err
	}
	info.MusicDir = metaValue(db, "music_dir")
	if raw := metaValue(db, "indexed_at"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			info.IndexedAt = t
		}
	}
	return info, nil
}

func metaValue(db *sql.DB, key string) string {
	var v string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// Reindex rebuilds the index at dbPath for exactly the given tracks,
// reading each track's .txt/.lrc sidecar under musicDir. Sidecars
// unchanged since the last run (same modification time and same set of
// .txt/.lrc kinds present) are not re-read. Rows for tracks no longer in
// the list, or that have lost their sidecar, are removed. The whole
// rebuild runs in one transaction, so a failure or cancellation partway
// leaves the previous index intact.
//
// ctx cancellation is honoured between tracks; on cancel the partial
// Stats gathered so far is returned alongside ctx.Err().
func Reindex(ctx context.Context, dbPath, musicDir string, tracks []Track, p Progress) (Stats, error) {
	start := time.Now()
	stats := Stats{Tracks: len(tracks)}

	if musicDir == "" {
		return stats, fmt.Errorf("lyricsindex: music directory is not configured")
	}

	db, err := open(dbPath)
	if err != nil {
		return stats, err
	}
	defer db.Close()

	existing, err := loadRowMeta(db)
	if err != nil {
		return stats, err
	}

	tx, err := db.Begin()
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()

	upsert, err := tx.Prepare(`
		INSERT INTO entries (file, display, kinds, mtime_ns, text_folded)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(file) DO UPDATE SET
			display = excluded.display,
			kinds = excluded.kinds,
			mtime_ns = excluded.mtime_ns,
			text_folded = excluded.text_folded
	`)
	if err != nil {
		return stats, err
	}
	defer upsert.Close()

	touchDisplay, err := tx.Prepare(`UPDATE entries SET display = ? WHERE file = ?`)
	if err != nil {
		return stats, err
	}
	defer touchDisplay.Close()

	// candidatesByDir memoises the per-directory sidecar listing so an
	// album's worth of tracks costs one ReadDir, not one per track.
	type dirCands struct{ txt, lrc map[string]string }
	candidatesByDir := make(map[string]dirCands)

	seen := make(map[string]bool, len(tracks))
	for i, tr := range tracks {
		if err := ctx.Err(); err != nil {
			stats.Elapsed = time.Since(start)
			return stats, err
		}

		dir := lyrics.Dir(musicDir, tr.File)
		dc, ok := candidatesByDir[dir]
		if !ok {
			dc = dirCands{txt: lyrics.Candidates(dir), lrc: lyrics.LRCCandidates(dir)}
			candidatesByDir[dir] = dc
		}

		txtName, hasTxt := lyrics.Match(tr.File, dc.txt)
		lrcName, hasLRC := lyrics.Match(tr.File, dc.lrc)
		if !hasTxt && !hasLRC {
			continue
		}

		kinds, mtime := sidecarSignature(dir, txtName, hasTxt, lrcName, hasLRC)
		seen[tr.File] = true

		if prev, ok := existing[tr.File]; ok && prev.kinds == kinds && prev.mtimeNS == mtime {
			stats.Unchanged++
			stats.Indexed++
			if prev.display != tr.Display {
				if _, err := touchDisplay.Exec(tr.Display, tr.File); err != nil {
					return stats, err
				}
			}
			if p != nil && (i+1)%progressEvery == 0 {
				p(i+1, len(tracks))
			}
			continue
		}

		text := readSidecarText(dir, txtName, hasTxt, lrcName, hasLRC)
		if _, err := upsert.Exec(tr.File, tr.Display, kinds, mtime, Fold(text)); err != nil {
			return stats, err
		}
		stats.Read++
		stats.Indexed++

		if p != nil && (i+1)%progressEvery == 0 {
			p(i+1, len(tracks))
		}
	}

	for file := range existing {
		if !seen[file] {
			if _, err := tx.Exec(`DELETE FROM entries WHERE file = ?`, file); err != nil {
				return stats, err
			}
			stats.Removed++
		}
	}

	if err := setMeta(tx, "music_dir", musicDir); err != nil {
		return stats, err
	}
	if err := setMeta(tx, "indexed_at", start.Format(time.RFC3339)); err != nil {
		return stats, err
	}
	if err := tx.Commit(); err != nil {
		return stats, err
	}

	if p != nil {
		p(len(tracks), len(tracks))
	}
	stats.Elapsed = time.Since(start)
	return stats, nil
}

type rowMeta struct {
	display string
	kinds   string
	mtimeNS int64
}

func loadRowMeta(db *sql.DB) (map[string]rowMeta, error) {
	rows, err := db.Query(`SELECT file, display, kinds, mtime_ns FROM entries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]rowMeta)
	for rows.Next() {
		var file string
		var rm rowMeta
		if err := rows.Scan(&file, &rm.display, &rm.kinds, &rm.mtimeNS); err != nil {
			return nil, err
		}
		out[file] = rm
	}
	return out, rows.Err()
}

func setMeta(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// sidecarSignature returns a stable "kinds" string ("txt", "lrc", or
// "txt+lrc") and the newer of the present sidecars' modification times in
// Unix nanoseconds -- the pair Reindex compares against the stored row to
// decide whether a re-read is needed. A sidecar that can't be stat'd
// contributes mtime 0, which simply forces a re-read (and readSidecarText
// then also fails soft), rather than an error that would sink the run.
func sidecarSignature(dir, txtName string, hasTxt bool, lrcName string, hasLRC bool) (kinds string, mtimeNS int64) {
	var parts []string
	if hasTxt {
		parts = append(parts, "txt")
		mtimeNS = maxInt64(mtimeNS, statMtimeNS(filepath.Join(dir, txtName)))
	}
	if hasLRC {
		parts = append(parts, "lrc")
		mtimeNS = maxInt64(mtimeNS, statMtimeNS(filepath.Join(dir, lrcName)))
	}
	return strings.Join(parts, "+"), mtimeNS
}

func statMtimeNS(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano()
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// readSidecarText returns the combined plain lyrics text for a track:
// the .txt verbatim and/or the .lrc flattened to its line content
// (timestamps and metadata tags dropped, see lyrics.ParseLRC), joined
// with a blank line when both exist. Unreadable sidecars contribute
// nothing rather than failing.
func readSidecarText(dir, txtName string, hasTxt bool, lrcName string, hasLRC bool) string {
	var parts []string
	if hasTxt {
		if data, err := os.ReadFile(filepath.Join(dir, txtName)); err == nil {
			parts = append(parts, string(data))
		}
	}
	if hasLRC {
		if data, err := os.ReadFile(filepath.Join(dir, lrcName)); err == nil {
			parts = append(parts, lrcPlainText(string(data)))
		}
	}
	return strings.Join(parts, "\n\n")
}

func lrcPlainText(raw string) string {
	lines := lyrics.ParseLRC(raw)
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

// stripDiacritics matches internal/ui's own search folding exactly (NFD,
// drop combining marks, NFC) -- the index stores text folded this way and
// the interactive search folds its query the same way, so the two must
// not drift. It's duplicated here rather than shared through a new
// package so internal/lyricsindex stays a leaf; the transform never
// errors on the well-formed UTF-8 this only ever sees.
var stripDiacritics = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Fold lowercases s and strips its diacritics, so a query and the indexed
// text match regardless of case or accent (the app-wide "buble" matches
// "Bublé" promise).
func Fold(s string) string {
	folded, _, err := transform.String(stripDiacritics, s)
	if err != nil {
		folded = s
	}
	return strings.ToLower(folded)
}
