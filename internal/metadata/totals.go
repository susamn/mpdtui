package metadata

// Totals is a library-wide count of everything this database records,
// as shown by the Library card ('M'). Every field counts rows, never
// tracks MPD knows about: a track with no opinions recorded against it
// has no row here at all, which is why Tracks is normally far smaller
// than the library's own track count and is not a substitute for it.
type Totals struct {
	// Tracks is how many tracks have a row here -- a rating, a play, a
	// mark, a tag or a bookmark has been recorded for each of them.
	Tracks int

	// Rated is tracks with a rating above zero, and Stars the sum of
	// those ratings, so an average is one division away without a
	// second query.
	Rated int
	Stars int

	// Played is tracks played through at least once, Plays the total
	// number of play-throughs counted across all of them.
	Played int
	Plays  int

	// Marked and Tagged are distinct tracks carrying at least one mark
	// or tag -- not the number of mark/tag assignments, which is
	// larger whenever a track carries several.
	Marked int
	Tagged int

	// Bookmarks is every saved position, BookmarkedTracks how many
	// distinct tracks they are spread across.
	Bookmarks        int
	BookmarkedTracks int

	// MarkReasons and Tags are the sizes of the two catalogs
	// themselves (see Settings' Database tab), which is what makes a
	// zero in Marked or Tagged readable: no catalog entries yet, or a
	// catalog nothing has been filed under.
	MarkReasons int
	Tags        int
}

// Totals counts the whole database in one pass of small aggregates.
//
// Separate scalar queries rather than one joined statement: joining
// track_marks and track_tags against tracks in a single row would
// multiply their counts together, and the SELECTs are cheap enough
// (indexed primary keys over a few thousand rows) that the clarity is
// worth more than the round-trips to a local file.
func (db *DB) Totals() (Totals, error) {
	var t Totals
	queries := []struct {
		dest *int
		sql  string
	}{
		{&t.Tracks, `SELECT COUNT(*) FROM tracks`},
		{&t.Rated, `SELECT COUNT(*) FROM tracks WHERE rating > 0`},
		{&t.Stars, `SELECT COALESCE(SUM(rating), 0) FROM tracks WHERE rating > 0`},
		{&t.Played, `SELECT COUNT(*) FROM tracks WHERE play_count > 0`},
		{&t.Plays, `SELECT COALESCE(SUM(play_count), 0) FROM tracks`},
		{&t.Marked, `SELECT COUNT(DISTINCT track_id) FROM track_marks`},
		{&t.Tagged, `SELECT COUNT(DISTINCT track_id) FROM track_tags`},
		{&t.Bookmarks, `SELECT COUNT(*) FROM bookmarks`},
		{&t.BookmarkedTracks, `SELECT COUNT(DISTINCT track_id) FROM bookmarks`},
		{&t.MarkReasons, `SELECT COUNT(*) FROM mark_reason`},
		{&t.Tags, `SELECT COUNT(*) FROM tags`},
	}
	for _, q := range queries {
		if err := db.sql.QueryRow(q.sql).Scan(q.dest); err != nil {
			return Totals{}, err
		}
	}
	return t, nil
}
