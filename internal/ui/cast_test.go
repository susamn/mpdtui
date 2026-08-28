package ui

import (
	"context"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mpdtui/internal/cast"
)

// syncApp is newTestApp with runAsync forced synchronous, so cast
// operations (routed through runAsync) complete before assertions.
func syncApp() *App {
	a := newTestApp()
	a.runAsync = func(work func() error, onSuccess func()) {
		if work() == nil {
			onSuccess()
		}
	}
	return a
}

type fakeCast struct {
	targets []cast.Target
	session *cast.Session
	started []cast.Target
	stopped int
}

func (f *fakeCast) Discover(context.Context) []cast.Target { return f.targets }

func (f *fakeCast) StartCast(_ context.Context, t cast.Target, _ cast.MediaMeta) (*cast.Session, error) {
	f.started = append(f.started, t)
	f.session = &cast.Session{Target: t}
	return f.session, nil
}

func (f *fakeCast) StopCast(context.Context) error {
	f.stopped++
	f.session = nil
	return nil
}

func (f *fakeCast) Session() *cast.Session                          { return f.session }
func (f *fakeCast) Reattach(context.Context) (*cast.Session, error) { return f.session, nil }

func TestCastStatusLine(t *testing.T) {
	a := newTestApp()

	if a.castStatusLine() != "" {
		t.Errorf("castStatusLine with nil cast = %q, want empty", a.castStatusLine())
	}

	fc := &fakeCast{}
	a.cast = fc
	if a.castStatusLine() != "" {
		t.Errorf("castStatusLine with no session = %q, want empty", a.castStatusLine())
	}

	fc.session = &cast.Session{Target: cast.Target{Name: "Living Room"}}
	if got := a.castStatusLine(); got == "" || !contains(got, "Living Room") {
		t.Errorf("castStatusLine with session = %q, want it to name the device", got)
	}
}

func TestPOpensCastOverlay(t *testing.T) {
	a := newTestApp()
	a.cast = &fakeCast{targets: []cast.Target{{Kind: cast.KindChromecast, ID: "x", Name: "TV"}}}

	pKey := tcell.NewEventKey(tcell.KeyRune, 'P', tcell.ModNone)
	if a.globalInputCapture(pKey) != nil {
		t.Fatal("'P' was not consumed by globalInputCapture")
	}
	if a.mode != modeOverlay {
		t.Fatalf("mode = %d after 'P', want modeOverlay", a.mode)
	}
	if a.tv.GetFocus() != a.castPicker {
		t.Error("focus not on the cast picker after 'P'")
	}
}

func TestPWithNoCastControllerFlashesInsteadOfOpening(t *testing.T) {
	a := newTestApp() // a.cast is nil
	a.globalInputCapture(tcell.NewEventKey(tcell.KeyRune, 'P', tcell.ModNone))
	if a.mode == modeOverlay {
		t.Error("cast overlay opened despite no cast controller")
	}
}

func TestCastPickerRenderStates(t *testing.T) {
	a := newTestApp()
	a.cast = &fakeCast{}

	a.castPicker.render(nil, true)
	if n := a.castPicker.GetItemCount(); n != 1 {
		t.Errorf("scanning: item count = %d, want 1 (the scanning row)", n)
	}

	a.castPicker.render(nil, false)
	if n := a.castPicker.GetItemCount(); n != 1 {
		t.Errorf("empty: item count = %d, want 1 (the no-devices row)", n)
	}

	a.castPicker.render([]cast.Target{
		{Kind: cast.KindChromecast, Name: "TV"},
		{Kind: cast.KindChromecast, Name: "Speaker"},
	}, false)
	if n := a.castPicker.GetItemCount(); n != 2 {
		t.Errorf("with targets: item count = %d, want 2", n)
	}
}

func TestCastPickerRenderShowsStopRowWhenCasting(t *testing.T) {
	a := newTestApp()
	a.cast = &fakeCast{session: &cast.Session{Target: cast.Target{Name: "TV"}}}
	a.castPicker.render([]cast.Target{{Kind: cast.KindChromecast, Name: "Speaker"}}, false)

	if n := a.castPicker.GetItemCount(); n != 2 {
		t.Fatalf("item count = %d, want 2 (Stop row + 1 target)", n)
	}
	main, _ := a.castPicker.GetItemText(0)
	if !contains(main, "Stop casting") {
		t.Errorf("first row = %q, want the Stop-casting row", main)
	}
}

func TestCastPickerApplyStartsCast(t *testing.T) {
	a := syncApp()
	fc := &fakeCast{}
	a.cast = fc
	target := cast.Target{Kind: cast.KindChromecast, ID: "x", Name: "TV"}

	a.castPicker.render([]cast.Target{target}, false)
	a.showOverlay("cast", centered(a.castPicker, 64, 14), a.castPicker)
	a.castPicker.apply(0, false) // no active session, so index 0 is the target

	if len(fc.started) != 1 || fc.started[0].ID != "x" {
		t.Fatalf("StartCast calls = %+v, want one for target x", fc.started)
	}
}

func TestCastPickerApplyStopsWhenSessionActive(t *testing.T) {
	a := syncApp()
	fc := &fakeCast{session: &cast.Session{Target: cast.Target{Name: "TV"}}}
	a.cast = fc

	a.castPicker.render(nil, false)
	a.showOverlay("cast", centered(a.castPicker, 64, 14), a.castPicker)
	a.castPicker.apply(0, true) // index 0 is the synthetic "Stop casting" row

	if fc.stopped != 1 {
		t.Fatalf("StopCast calls = %d, want 1", fc.stopped)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
