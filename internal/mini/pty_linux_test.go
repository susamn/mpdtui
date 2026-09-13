//go:build linux

package mini

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY allocates a pseudo-terminal pair and returns the controlling
// side and the terminal side.
//
// Run needs a real tty: it calls term.IsTerminal and term.MakeRaw on
// stdin, neither of which a pipe satisfies. A pty is the only way to
// exercise it, and it costs nothing outside the test -- no process is
// spawned and nothing about the developer's own terminal is touched.
func openPTY(t *testing.T) (controller, terminal *os.File) {
	t.Helper()

	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx available: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		ptmx.Close()
		t.Skipf("unlocking the pty failed: %v", err)
	}
	n, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
	if err != nil {
		ptmx.Close()
		t.Skipf("finding the pty number failed: %v", err)
	}
	pts, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		ptmx.Close()
		t.Skipf("opening the pty slave failed: %v", err)
	}

	t.Cleanup(func() { pts.Close(); ptmx.Close() })
	return ptmx, pts
}

// withPTYStdio points os.Stdin and os.Stdout at a pty for the duration
// of the test, and returns the controlling side to write keys into.
func withPTYStdio(t *testing.T) *os.File {
	t.Helper()
	ptmx, pts := openPTY(t)

	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = pts, pts
	t.Cleanup(func() { os.Stdin, os.Stdout = oldIn, oldOut })

	return ptmx
}

// waitForRawMode blocks until stdin is actually in raw mode, which is
// what Run does first. Without waiting, a control byte written too
// early is eaten by the line discipline (Ctrl-C is still a signal
// character until ISIG is cleared) instead of reaching the key reader.
func waitForRawMode(t *testing.T, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		termios, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
		if err == nil && termios.Lflag&unix.ISIG == 0 && termios.Lflag&unix.ECHO == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("stdin never entered raw mode")
}
