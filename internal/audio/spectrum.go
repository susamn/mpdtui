package audio

import (
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"syscall"
	"time"
)

const (
	// SampleRate, sampleBits and channels are the PCM format this
	// package expects on the fifo -- they must match the `format` line
	// on MPD's fifo output. 44100:16:2 is both MPD's own default and
	// what every fifo-visualizer setup in the wild uses, so rather than
	// making these configurable (MPD gives us no way to discover the
	// actual format of the pipe, so a mismatch would have to be guessed
	// at anyway), the README documents the required line.
	SampleRate     = 44100
	channels       = 2
	bytesPerSample = 2 // signed 16-bit little-endian

	// fftSize is the analysis window, in mono samples: 2048 at 44.1kHz
	// is ~46ms of audio and ~21.5Hz per bin. Big enough to separate bass
	// notes, short enough that the display tracks transients at the
	// 40ms redraw cadence (see app.go's animTicker).
	fftSize = 2048

	// staleAfter is how long the fifo can go quiet before Active reports
	// false and visualizations fall back to their simulated animation.
	// MPD writes continuously while playing and stops instantly on
	// pause/stop, so this only needs to outlast normal scheduling jitter.
	staleAfter = 250 * time.Millisecond

	// reopenBackoff is how long the reader waits before retrying after
	// the fifo turns out to be missing, unreadable, or writer-less.
	// mpdtui runs fine without a fifo at all, so this path stays quiet
	// and cheap rather than erroring.
	reopenBackoff = 2 * time.Second
)

// Band shaping. Magnitudes are converted to decibels because loudness is
// perceptually logarithmic -- a linear magnitude scale leaves everything
// but the bass line pinned at zero.
const (
	minHz = 40    // below this is mostly rumble at terminal-sized resolution
	maxHz = 16000 // above this there's rarely enough energy to see

	floorDB = -68 // maps to an empty bar
	ceilDB  = -12 // maps to a full bar

	// tiltDBPerOctave lifts the high end. Recorded music has a natural
	// downward spectral slope, so without a tilt the treble bars barely
	// move while the bass pins to the top.
	tiltDBPerOctave = 2.6

	// Smoothing time constants. Attack is fast so a kick drum lands on
	// the frame it happens; release is slower so bars fall rather than
	// flicker between frames.
	attackTau  = 0.02
	releaseTau = 0.16
)

// Spectrum reads MPD's fifo output in the background and turns the most
// recent audio into per-band magnitudes.
//
// It is safe for concurrent use and never blocks its caller: the reader
// goroutine owns the pipe, Bands only ever touches a snapshot. Every
// failure mode -- no fifo configured, fifo missing, MPD not playing --
// surfaces as Active reporting false, never as an error the UI has to
// handle, because a missing visualizer feed is a normal state for a
// setup that simply doesn't have that audio_output configured.
type Spectrum struct {
	path string

	mu sync.Mutex
	// ring holds the most recent fftSize mono samples, oldest-first
	// starting at pos.
	ring     [fftSize]float64
	pos      int
	filled   bool
	lastData time.Time
	// carry holds bytes left over when a read ends mid-frame.
	carry []byte
	// smoothed is the previous Bands result, held here (rather than in
	// each visualization) so smoothing is a property of the analysis
	// every visualization shares.
	smoothed   []float64
	lastSmooth time.Time

	done     chan struct{}
	stopOnce sync.Once

	// closeFile closes the currently open fifo, if any, so Close can
	// interrupt a reader parked in read(2).
	closeMu sync.Mutex
	file    *os.File
}

// NewSpectrum returns a Spectrum that will read PCM from the fifo at
// path. An empty path yields a Spectrum that is simply never active,
// which is the correct behaviour for a user who hasn't configured the
// output -- callers don't need to nil-check.
func NewSpectrum(path string) *Spectrum {
	return &Spectrum{path: path, done: make(chan struct{})}
}

// Start launches the background reader. Safe to call on a Spectrum with
// no configured path (it returns immediately).
func (s *Spectrum) Start() {
	if s == nil || s.path == "" {
		return
	}
	go s.run()
}

// Close stops the background reader and unblocks it if it is parked
// waiting for audio.
func (s *Spectrum) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.done)
		s.closeMu.Lock()
		if s.file != nil {
			s.file.Close()
		}
		s.closeMu.Unlock()
	})
}

// Active reports whether real audio has arrived recently enough to draw
// from. Visualizations use this to decide between the real spectrum and
// their simulated fallback.
func (s *Spectrum) Active() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filled && time.Since(s.lastData) < staleAfter
}

// Bands returns n magnitudes in 0..1, lowest frequency first, spaced
// logarithmically between minHz and maxHz, smoothed against the previous
// call. It returns nil when there's no live audio -- a caller that gets
// nil should fall back to its simulated animation.
func (s *Spectrum) Bands(n int) []float64 {
	if s == nil || n <= 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.filled || time.Since(s.lastData) >= staleAfter {
		s.smoothed = nil
		return nil
	}

	// Copy the ring out oldest-first before transforming it.
	samples := make([]float64, fftSize)
	copy(samples, s.ring[s.pos:])
	copy(samples[fftSize-s.pos:], s.ring[:s.pos])

	raw := bandLevels(magnitudes(samples), n)

	now := time.Now()
	if len(s.smoothed) != n {
		s.smoothed = raw
		s.lastSmooth = now
		return append([]float64(nil), raw...)
	}
	dt := now.Sub(s.lastSmooth).Seconds()
	s.lastSmooth = now
	if dt <= 0 || dt > 1 {
		// First frame after a stall: don't let a huge dt make the
		// smoothing a no-op in one direction and a jump in the other.
		dt = 0.04
	}
	attack := 1 - math.Exp(-dt/attackTau)
	release := 1 - math.Exp(-dt/releaseTau)
	for i, v := range raw {
		a := release
		if v > s.smoothed[i] {
			a = attack
		}
		s.smoothed[i] += (v - s.smoothed[i]) * a
	}
	return append([]float64(nil), s.smoothed...)
}

// bandLevels reduces FFT bin magnitudes to n logarithmically spaced
// bands in 0..1. Each band takes the loudest bin it covers rather than
// the mean: a mean washes out a narrow peak against the many quiet bins
// beside it, which at the top of the range is most of them.
func bandLevels(mags []float64, n int) []float64 {
	out := make([]float64, n)
	if len(mags) < 2 || n <= 0 {
		return out
	}
	binHz := float64(SampleRate) / float64((len(mags)-1)*2)
	ratio := math.Log(maxHz/minHz) / float64(n)

	for b := 0; b < n; b++ {
		lo := minHz * math.Exp(ratio*float64(b))
		hi := minHz * math.Exp(ratio*float64(b+1))

		loBin := int(lo / binHz)
		hiBin := int(hi / binHz)
		if loBin < 1 {
			loBin = 1 // skip bin 0: DC offset, not audible content
		}
		if hiBin <= loBin {
			hiBin = loBin // narrow low bands can land inside a single bin
		}
		if hiBin >= len(mags) {
			hiBin = len(mags) - 1
		}

		peak := 0.0
		for k := loBin; k <= hiBin; k++ {
			if mags[k] > peak {
				peak = mags[k]
			}
		}

		db := 20 * math.Log10(peak+1e-12)
		db += tiltDBPerOctave * math.Log2(lo/minHz)
		out[b] = clamp01((db - floorDB) / (ceilDB - floorDB))
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// run owns the fifo for the life of the Spectrum: open, read until the
// pipe or the process goes away, back off, open again. It deliberately
// never reports errors anywhere -- see the type's doc comment.
func (s *Spectrum) run() {
	buf := make([]byte, 8192)
	for {
		select {
		case <-s.done:
			return
		default:
		}

		f, err := s.open()
		if err != nil {
			if !s.sleep(reopenBackoff) {
				return
			}
			continue
		}

		for {
			n, err := f.Read(buf)
			if n > 0 {
				s.ingest(buf[:n])
			}
			if err == nil {
				continue
			}
			if errors.Is(err, os.ErrClosed) {
				return
			}
			if errors.Is(err, io.EOF) {
				// No writer on the pipe. MPD normally holds it open for
				// the whole session, so this means MPD is gone; wait
				// before reopening rather than spinning on EOF.
				if !s.sleep(reopenBackoff) {
					return
				}
			}
			break
		}

		s.closeMu.Lock()
		if s.file == f {
			s.file = nil
		}
		s.closeMu.Unlock()
		f.Close()
	}
}

// open opens the fifo for reading without blocking. O_NONBLOCK matters
// twice over: opening a fifo read-only otherwise blocks in open(2) until
// a writer appears (which would hang this goroutine until the first time
// something plays), and it lets Go's runtime poller park the read
// instead of busy-waiting, so an idle visualizer costs nothing.
func (s *Spectrum) open() (*os.File, error) {
	f, err := os.OpenFile(s.path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	s.closeMu.Lock()
	select {
	case <-s.done:
		// Closed while we were opening.
		s.closeMu.Unlock()
		f.Close()
		return nil, os.ErrClosed
	default:
	}
	s.file = f
	s.closeMu.Unlock()
	return f, nil
}

// sleep waits d, returning false if the Spectrum was closed meanwhile.
func (s *Spectrum) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-s.done:
		return false
	case <-t.C:
		return true
	}
}

// ingest downmixes a chunk of interleaved stereo PCM to mono and appends
// it to the ring. Reads land on arbitrary byte boundaries, so a trailing
// partial frame is carried over to the next chunk rather than dropped --
// dropping it would shift channel parity and turn the downmix into
// noise.
func (s *Spectrum) ingest(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := chunk
	if len(s.carry) > 0 {
		data = append(s.carry, chunk...)
		s.carry = s.carry[:0]
	}

	const frame = channels * bytesPerSample
	full := len(data) / frame * frame
	for i := 0; i < full; i += frame {
		l := float64(int16(uint16(data[i]) | uint16(data[i+1])<<8))
		r := float64(int16(uint16(data[i+2]) | uint16(data[i+3])<<8))
		s.ring[s.pos] = (l + r) / 2 / 32768
		s.pos++
		if s.pos == fftSize {
			s.pos = 0
			s.filled = true
		}
	}
	if rest := data[full:]; len(rest) > 0 {
		s.carry = append(s.carry[:0], rest...)
	}
	s.lastData = time.Now()
}
