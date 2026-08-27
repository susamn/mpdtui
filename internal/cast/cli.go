package cast

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// cliTimeout bounds a headless cast command overall -- discovery plus the
// device round-trip.
const cliTimeout = 15 * time.Second

// List prints discovered cast targets, one per line, as KIND NAME ADDR.
func List(w io.Writer, m *Manager) error {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	targets := m.Discover(ctx)
	if len(targets) == 0 {
		fmt.Fprintln(w, "no cast targets found (check that mDNS/SSDP multicast works on this network)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, t := range targets {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", t.Kind, t.Name, t.Addr)
	}
	return tw.Flush()
}

// Start begins casting to the target whose ID or (case-insensitive) name
// matches nameOrID.
func Start(w io.Writer, m *Manager, nameOrID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	target, err := matchTarget(m.Discover(ctx), nameOrID)
	if err != nil {
		return err
	}

	meta := MediaMeta{}
	if song, err := m.mpd.CurrentSong(); err == nil {
		meta = mediaMetaFromSong(song)
	}

	sess, err := m.StartCast(ctx, target, meta)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "casting to %s (%s)\n", sess.Target.Name, sess.StreamURL)
	return nil
}

// Stop tears down the active cast, whether this process started it or a
// since-closed one did.
func Stop(w io.Writer, m *Manager) error {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()

	switch err := m.StopCast(ctx); err {
	case nil:
		fmt.Fprintln(w, "cast stopped")
		return nil
	case ErrNotCasting:
		fmt.Fprintln(w, "no active cast found")
		return nil
	default:
		return err
	}
}

func matchTarget(targets []Target, nameOrID string) (Target, error) {
	var byName []Target
	for _, t := range targets {
		if t.ID == nameOrID {
			return t, nil
		}
		if strings.EqualFold(t.Name, nameOrID) {
			byName = append(byName, t)
		}
	}
	switch len(byName) {
	case 1:
		return byName[0], nil
	case 0:
		if len(targets) == 0 {
			return Target{}, fmt.Errorf("no cast targets found matching %q", nameOrID)
		}
		names := make([]string, len(targets))
		for i, t := range targets {
			names[i] = t.Name
		}
		return Target{}, fmt.Errorf("no cast target matching %q; found: %s", nameOrID, strings.Join(names, ", "))
	default:
		return Target{}, fmt.Errorf("%q is ambiguous: %d targets share that name", nameOrID, len(byName))
	}
}
