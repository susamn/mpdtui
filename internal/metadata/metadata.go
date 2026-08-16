// Package metadata stores local, per-track bookkeeping -- play count,
// rating, a "marked for X" flag with a reason, and tags -- that MPD
// itself has no concept of. It's a SQLite database local to this
// machine, entirely separate from MPD's own library database: MPD stays
// the sole source of truth for what tracks/playlists actually exist,
// this package only ever adds opinions on top of a track MPD already
// reports.
//
// A track's key is its MPD-relative File path, normalized (see
// normalizePath) the same way internal/lyrics normalizes filenames --
// deliberately duplicated here rather than importing internal/lyrics
// (per DEPENDENCY.md, this package stays an independent leaf, not worth
// a new cross-package edge for ~10 lines).
package metadata

import (
	"database/sql"
	"errors"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

// DB is a handle to the local track-metadata database.
type DB struct {
	sql *sql.DB
}

// schema creates every table if it doesn't already exist -- safe to run
// on every Open, including against a database created by an earlier
// version of this schema.
const schema = `
CREATE TABLE IF NOT EXISTS mark_reason (
	id     INTEGER PRIMARY KEY,
	reason TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS tags (
	id      INTEGER PRIMARY KEY,
	tagname TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS tracks (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	normalized_path TEXT NOT NULL UNIQUE,
	real_path       TEXT NOT NULL,
	play_count      INTEGER NOT NULL DEFAULT 0,
	rating          INTEGER NOT NULL DEFAULT 0,
	mark            INTEGER REFERENCES mark_reason(id),
	updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS track_tags (
	track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
	tag_id   INTEGER NOT NULL REFERENCES tags(id),
	PRIMARY KEY (track_id, tag_id)
);
`

// seedMarkReasons/seedTags populate the two catalog tables' starting
// rows, at the specific ids requested (mark_reason 1 = "mark for
// deletion"; tags 1/2/3 = bengali/hindi/english) -- INSERT OR IGNORE, so
// re-running Open against an existing database never duplicates or
// clobbers rows the user has since added or edited by hand.
const seedMarkReasons = `INSERT OR IGNORE INTO mark_reason (id, reason) VALUES (1, 'mark for deletion');`
const seedTags = `
INSERT OR IGNORE INTO tags (id, tagname) VALUES (1, 'bengali');
INSERT OR IGNORE INTO tags (id, tagname) VALUES (2, 'hindi');
INSERT OR IGNORE INTO tags (id, tagname) VALUES (3, 'english');
`

// Open opens (creating if necessary) the SQLite database at path,
// applies the schema, and seeds the catalog tables' starting rows.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection avoids SQLITE_BUSY from concurrent writers --
	// unnecessary insurance in practice (every call into this package
	// from internal/ui already runs on tview's own single-threaded event
	// loop), but cheap and standard practice for SQLite from Go.
	sqlDB.SetMaxOpenConns(1)

	db := &DB{sql: sqlDB}
	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := sqlDB.Exec(seedMarkReasons); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := sqlDB.Exec(seedTags); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.sql.Close()
}

// MarkReason is a catalog entry from mark_reason.
type MarkReason struct {
	ID     int64
	Reason string
}

// Tag is a catalog entry from tags.
type Tag struct {
	ID      int64
	Tagname string
}

// Track is one track's local metadata. Zero-value PlayCount/Rating and a
// nil Mark/empty Tags are what Get returns for a file with no row yet --
// a track with no opinions recorded about it, not an error.
type Track struct {
	NormalizedPath string
	RealPath       string
	PlayCount      int
	Rating         int // 0 (unrated) - 5
	Mark           *MarkReason
	Tags           []Tag
}

// normalizePath folds file for use as the stable database key: each path
// segment (every directory name and the filename) is folded to just its
// letters/digits, lowercased, independently -- the same per-rune fold
// internal/lyrics.Normalize uses -- then rejoined with "/". Segments are
// kept separate deliberately: stripping "/" itself along with the rest
// of the punctuation would collide two different tracks that share a
// generic filename in different folders (e.g. "Artist A/01 Intro.mp3" and
// "Artist B/01 Intro.mp3" -- a common occurrence, not a hypothetical).
func normalizePath(file string) string {
	segments := strings.Split(file, "/")
	for i, seg := range segments {
		segments[i] = normalizeSegment(seg)
	}
	return strings.Join(segments, "/")
}

// normalizeSegment folds s to just its letters and digits, lowercased.
func normalizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// upsertTrack inserts a bare row for file if one doesn't already exist
// (real_path set, everything else at its default), or refreshes
// real_path on an existing row -- file's raw path can drift slightly
// (e.g. a retag that only changes punctuation) while still normalizing
// to the same key, and this keeps real_path reflecting the most
// recently seen value. Returns the row's id, needed by every other
// method here for track_tags joins and updates.
func (db *DB) upsertTrack(file string) (id int64, err error) {
	norm := normalizePath(file)
	_, err = db.sql.Exec(`
		INSERT INTO tracks (normalized_path, real_path)
		VALUES (?, ?)
		ON CONFLICT(normalized_path) DO UPDATE SET real_path = excluded.real_path
	`, norm, file)
	if err != nil {
		return 0, err
	}
	row := db.sql.QueryRow(`SELECT id FROM tracks WHERE normalized_path = ?`, norm)
	err = row.Scan(&id)
	return id, err
}

// Get returns file's current metadata, or a zero-opinion Track (not an
// error) if no row exists for it yet.
func (db *DB) Get(file string) (Track, error) {
	norm := normalizePath(file)
	row := db.sql.QueryRow(`
		SELECT id, real_path, play_count, rating, mark
		FROM tracks WHERE normalized_path = ?
	`, norm)

	var id int64
	var mark sql.NullInt64
	t := Track{NormalizedPath: norm, RealPath: file}
	err := row.Scan(&id, &t.RealPath, &t.PlayCount, &t.Rating, &mark)
	if errors.Is(err, sql.ErrNoRows) {
		return t, nil
	}
	if err != nil {
		return Track{}, err
	}

	if mark.Valid {
		reason, err := db.markReasonByID(mark.Int64)
		if err != nil {
			return Track{}, err
		}
		t.Mark = &reason
	}
	tags, err := db.tagsForTrack(id)
	if err != nil {
		return Track{}, err
	}
	t.Tags = tags
	return t, nil
}

// Rate sets file's rating (0-5; 0 clears it), creating its row if
// necessary.
func (db *DB) Rate(file string, rating int) error {
	id, err := db.upsertTrack(file)
	if err != nil {
		return err
	}
	_, err = db.sql.Exec(`UPDATE tracks SET rating = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, rating, id)
	return err
}

// IncrementPlayCount adds one to file's play count, creating its row if
// necessary.
func (db *DB) IncrementPlayCount(file string) error {
	id, err := db.upsertTrack(file)
	if err != nil {
		return err
	}
	_, err = db.sql.Exec(`UPDATE tracks SET play_count = play_count + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// SetMark sets file's mark to reasonID, or clears it if reasonID is nil,
// creating its row if necessary.
func (db *DB) SetMark(file string, reasonID *int64) error {
	id, err := db.upsertTrack(file)
	if err != nil {
		return err
	}
	var mark sql.NullInt64
	if reasonID != nil {
		mark = sql.NullInt64{Int64: *reasonID, Valid: true}
	}
	_, err = db.sql.Exec(`UPDATE tracks SET mark = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, mark, id)
	return err
}

// SetTags replaces file's full set of tags with exactly tagIDs, creating
// its row if necessary.
func (db *DB) SetTags(file string, tagIDs []int64) error {
	id, err := db.upsertTrack(file)
	if err != nil {
		return err
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM track_tags WHERE track_id = ?`, id); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(`INSERT INTO track_tags (track_id, tag_id) VALUES (?, ?)`, id, tagID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddMarkReason inserts a new mark_reason catalog row, auto-assigning
// its id -- the in-app counterpart to editing the database by hand
// (previously the only way to add one, per this package's own doc
// comment on seedMarkReasons). reason must be non-empty and not already
// present (the column is UNIQUE); either violation returns the
// underlying sqlite error unchanged rather than a wrapped one, since
// there's nothing this layer can usefully add. Returns the new row's id.
func (db *DB) AddMarkReason(reason string) (int64, error) {
	res, err := db.sql.Exec(`INSERT INTO mark_reason (reason) VALUES (?)`, reason)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddTag inserts a new tags catalog row the same way AddMarkReason does.
func (db *DB) AddTag(tagname string) (int64, error) {
	res, err := db.sql.Exec(`INSERT INTO tags (tagname) VALUES (?)`, tagname)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteMarkReason removes a mark_reason catalog row. First clears it
// (SET mark = NULL) from any track that still references it, in the
// same transaction -- without that, Get's own markReasonByID lookup for
// such a track would start failing on a dangling reference (sql.
// ErrNoRows) instead of returning a valid Track, breaking rendering
// anywhere that track shows up (Queue's Mark column, mini mode, Now
// Playing) until the mark happened to be reset by hand.
func (db *DB) DeleteMarkReason(id int64) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE tracks SET mark = NULL WHERE mark = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM mark_reason WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTag removes a tags catalog row, first deleting any track_tags
// rows referencing it -- the same orphan-reference reasoning as
// DeleteMarkReason, via the join table rather than a direct column.
func (db *DB) DeleteTag(id int64) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM track_tags WHERE tag_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMarkReasons returns every row in mark_reason, ordered by id.
func (db *DB) ListMarkReasons() ([]MarkReason, error) {
	rows, err := db.sql.Query(`SELECT id, reason FROM mark_reason ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MarkReason
	for rows.Next() {
		var r MarkReason
		if err := rows.Scan(&r.ID, &r.Reason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListTags returns every row in tags, ordered by id.
func (db *DB) ListTags() ([]Tag, error) {
	rows, err := db.sql.Query(`SELECT id, tagname FROM tags ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Tagname); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) markReasonByID(id int64) (MarkReason, error) {
	row := db.sql.QueryRow(`SELECT id, reason FROM mark_reason WHERE id = ?`, id)
	var r MarkReason
	err := row.Scan(&r.ID, &r.Reason)
	return r, err
}

func (db *DB) tagsForTrack(trackID int64) ([]Tag, error) {
	rows, err := db.sql.Query(`
		SELECT tags.id, tags.tagname
		FROM tags
		JOIN track_tags ON track_tags.tag_id = tags.id
		WHERE track_tags.track_id = ?
		ORDER BY tags.id
	`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Tagname); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
