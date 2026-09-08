package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// pcmSine builds frames of interleaved 16-bit stereo PCM carrying a
// full-scale sine at hz, in the format MPD's fifo output is configured
// to write.
func pcmSine(hz float64, frames int) []byte {
	buf := make([]byte, frames*channels*bytesPerSample)
	for i := 0; i < frames; i++ {
		v := int16(30000 * math.Sin(2*math.Pi*hz*float64(i)/SampleRate))
		binary.LittleEndian.PutUint16(buf[i*4:], uint16(v))
		binary.LittleEndian.PutUint16(buf[i*4+2:], uint16(v))
	}
	return buf
}

func TestBandLevelsSilenceIsEmpty(t *testing.T) {
	levels := bandLevels(magnitudes(make([]float64, fftSize)), 24)
	for i, v := range levels {
		if v != 0 {
			t.Errorf("band %d = %g on silence, want 0", i, v)
		}
	}
}

func TestBandLevelsPutsEnergyInTheRightBand(t *testing.T) {
	// A 1kHz tone should light up the band covering 1kHz and leave the
	// bands at either end of the range alone -- this is what makes the
	// display a spectrum rather than a level meter.
	const tone = 1000.0
	samples := make([]float64, fftSize)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * tone * float64(i) / SampleRate)
	}

	const n = 24
	levels := bandLevels(magnitudes(samples), n)

	// Which band should hold it, by the same log spacing bandLevels uses.
	want := int(math.Log(tone/minHz) / math.Log(maxHz/minHz) * n)
	loudest := 0
	for i, v := range levels {
		if v > levels[loudest] {
			loudest = i
		}
	}
	if loudest != want {
		t.Errorf("loudest band = %d (%g), want %d", loudest, levels[loudest], want)
	}
	if levels[loudest] < 0.8 {
		t.Errorf("loudest band = %g for a full-scale tone, want near 1.0", levels[loudest])
	}
	if levels[0] > 0.1 || levels[n-1] > 0.1 {
		t.Errorf("energy leaked to the edges: first=%g last=%g", levels[0], levels[n-1])
	}
}

func TestBandLevelsClampsToUnitRange(t *testing.T) {
	// Deliberately overdriven input: levels must stay drawable rather
	// than overflowing the glyph table downstream.
	samples := make([]float64, fftSize)
	for i := range samples {
		samples[i] = 8 * math.Sin(2*math.Pi*400*float64(i)/SampleRate)
	}
	for i, v := range bandLevels(magnitudes(samples), 32) {
		if v < 0 || v > 1 {
			t.Errorf("band %d = %g, want within 0..1", i, v)
		}
	}
}

func TestIngestDownmixesStereoToMono(t *testing.T) {
	s := NewSpectrum("")
	// One frame: left full-scale positive, right full-scale negative.
	// The downmix must cancel them, which only holds if the channel
	// interleaving is read the right way round.
	negSample := int16(-16384)
	frame := make([]byte, 4)
	binary.LittleEndian.PutUint16(frame[0:], uint16(int16(16384)))
	binary.LittleEndian.PutUint16(frame[2:], uint16(negSample))
	s.ingest(frame)

	if s.pos != 1 {
		t.Fatalf("ring position = %d after one frame, want 1", s.pos)
	}
	if got := s.ring[0]; math.Abs(got) > 1e-9 {
		t.Errorf("downmix of +L/-R = %g, want 0", got)
	}
}

func TestIngestCarriesPartialFrames(t *testing.T) {
	// Reads land on arbitrary byte boundaries. A frame split across two
	// reads must be reassembled, not dropped -- dropping it would shift
	// channel parity for everything after it.
	s := NewSpectrum("")
	whole := make([]byte, 4)
	binary.LittleEndian.PutUint16(whole[0:], uint16(int16(8192)))
	binary.LittleEndian.PutUint16(whole[2:], uint16(int16(8192)))

	s.ingest(whole[:3])
	if s.pos != 0 {
		t.Fatalf("ring advanced on a partial frame: pos = %d, want 0", s.pos)
	}
	s.ingest(whole[3:])
	if s.pos != 1 {
		t.Fatalf("ring position = %d after the frame completed, want 1", s.pos)
	}
	if want := 8192.0 / 32768; math.Abs(s.ring[0]-want) > 1e-9 {
		t.Errorf("reassembled sample = %g, want %g", s.ring[0], want)
	}
}

func TestSpectrumWithoutPathIsNeverActive(t *testing.T) {
	// The "user has no fifo configured" case: everything must stay quiet
	// and nil rather than erroring, so visualizations fall back.
	s := NewSpectrum("")
	s.Start()
	defer s.Close()

	if s.Active() {
		t.Error("Active() = true with no configured fifo")
	}
	if got := s.Bands(16); got != nil {
		t.Errorf("Bands(16) = %v with no configured fifo, want nil", got)
	}
}

func TestNilSpectrumIsUsable(t *testing.T) {
	// Visualizations are constructed with a nil Spectrum in tests, so
	// the nil receiver has to behave like an inactive one.
	var s *Spectrum
	if s.Active() {
		t.Error("Active() = true on a nil Spectrum")
	}
	if got := s.Bands(8); got != nil {
		t.Errorf("Bands(8) = %v on a nil Spectrum, want nil", got)
	}
	s.Start()
	s.Close()
}

func TestSpectrumMissingFifoIsNeverActive(t *testing.T) {
	s := NewSpectrum(filepath.Join(t.TempDir(), "absent.fifo"))
	s.Start()
	defer s.Close()

	time.Sleep(50 * time.Millisecond)
	if s.Active() {
		t.Error("Active() = true for a fifo that doesn't exist")
	}
}

func TestSpectrumReadsLiveAudioFromFifo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mpd.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// O_RDWR is how MPD's own fifo plugin opens it: it keeps a writer on
	// the pipe for the whole session so readers never see EOF.
	w, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open fifo for writing: %v", err)
	}
	defer w.Close()

	s := NewSpectrum(path)
	s.Start()
	defer s.Close()

	// Enough frames to fill the analysis window, written in chunks
	// smaller than the pipe buffer so the writer never blocks.
	pcm := pcmSine(1000, fftSize*2)
	for off := 0; off < len(pcm); off += 4096 {
		end := off + 4096
		if end > len(pcm) {
			end = len(pcm)
		}
		if _, err := w.Write(pcm[off:end]); err != nil {
			t.Fatalf("write pcm: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	var bands []float64
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bands = s.Bands(24); bands != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bands == nil {
		t.Fatal("Bands() never returned data from a fifo carrying audio")
	}
	if !s.Active() {
		t.Error("Active() = false while audio is arriving")
	}

	loudest := 0
	for i, v := range bands {
		if v > bands[loudest] {
			loudest = i
		}
	}
	want := int(math.Log(1000.0/minHz) / math.Log(maxHz/minHz) * 24)
	if loudest != want {
		t.Errorf("loudest band = %d, want %d for a 1kHz tone through the fifo", loudest, want)
	}
}

func TestSpectrumGoesInactiveWhenAudioStops(t *testing.T) {
	// Pausing MPD simply stops the writes; the visualization has to
	// notice and fall back rather than freezing on the last frame.
	s := NewSpectrum("")
	s.ingest(pcmSine(1000, fftSize))
	s.mu.Lock()
	s.lastData = time.Now().Add(-staleAfter - time.Millisecond)
	s.mu.Unlock()

	if s.Active() {
		t.Error("Active() = true after the feed went stale")
	}
	if got := s.Bands(16); got != nil {
		t.Errorf("Bands(16) = %v after the feed went stale, want nil", got)
	}
}

func TestBandsSmoothsBetweenFrames(t *testing.T) {
	// Smoothing lives in Bands (shared by every visualization) rather
	// than in any one of them. A silent frame right after a loud one
	// must decay, not snap to zero.
	s := NewSpectrum("")
	s.ingest(pcmSine(1000, fftSize))
	loud := s.Bands(24)
	if loud == nil {
		t.Fatal("Bands() = nil right after ingesting audio")
	}

	peak := 0
	for i, v := range loud {
		if v > loud[peak] {
			peak = i
		}
	}

	s.ingest(make([]byte, fftSize*channels*bytesPerSample))
	time.Sleep(5 * time.Millisecond)
	quiet := s.Bands(24)
	if quiet == nil {
		t.Fatal("Bands() = nil after ingesting silence")
	}
	if quiet[peak] >= loud[peak] {
		t.Errorf("peak band did not decay: %g -> %g", loud[peak], quiet[peak])
	}
	if quiet[peak] == 0 {
		t.Error("peak band snapped straight to zero, want a smoothed decay")
	}
}
