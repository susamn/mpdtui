package audio

import (
	"math"
	"testing"
)

// naiveDFT is the textbook O(n^2) transform, used purely as an oracle to
// check the fast one against.
func naiveDFT(in []float64) ([]float64, []float64) {
	n := len(in)
	re := make([]float64, n)
	im := make([]float64, n)
	for k := 0; k < n; k++ {
		for t := 0; t < n; t++ {
			angle := -2 * math.Pi * float64(k) * float64(t) / float64(n)
			re[k] += in[t] * math.Cos(angle)
			im[k] += in[t] * math.Sin(angle)
		}
	}
	return re, im
}

func TestFFTMatchesNaiveDFT(t *testing.T) {
	const n = 64
	in := make([]float64, n)
	for i := range in {
		// An arbitrary non-symmetric signal: a symmetric one would hide
		// sign errors in the imaginary half.
		in[i] = math.Sin(float64(i)*0.7) + 0.3*math.Cos(float64(i)*2.3) + 0.1*float64(i%5)
	}

	wantRe, wantIm := naiveDFT(in)

	gotRe := make([]float64, n)
	copy(gotRe, in)
	gotIm := make([]float64, n)
	fft(gotRe, gotIm)

	const tol = 1e-9
	for k := 0; k < n; k++ {
		if math.Abs(gotRe[k]-wantRe[k]) > tol || math.Abs(gotIm[k]-wantIm[k]) > tol {
			t.Fatalf("bin %d = (%g, %g), want (%g, %g)", k, gotRe[k], gotIm[k], wantRe[k], wantIm[k])
		}
	}
}

func TestFFTHandlesDegenerateInput(t *testing.T) {
	// Guards the early return: a 0- or 1-point transform must not panic
	// (bandLevels is reached with whatever the ring holds).
	for _, n := range []int{0, 1} {
		re := make([]float64, n)
		im := make([]float64, n)
		fft(re, im)
	}
}

func TestMagnitudesRecoversSineAmplitude(t *testing.T) {
	// A full-scale cosine sitting exactly on a bin centre should read
	// back as amplitude 1.0 -- that's what the hannGain normalization in
	// magnitudes exists to make true, and it's the anchor for the dB
	// scale bandLevels applies.
	const n, bin = 1024, 64
	in := make([]float64, n)
	for i := range in {
		in[i] = math.Cos(2 * math.Pi * float64(bin) * float64(i) / float64(n))
	}

	mags := magnitudes(in)
	if len(mags) != n/2+1 {
		t.Fatalf("magnitudes returned %d bins, want %d", len(mags), n/2+1)
	}
	if got := mags[bin]; math.Abs(got-1) > 0.02 {
		t.Errorf("peak bin magnitude = %g, want ~1.0", got)
	}
	// Well away from the peak, only windowing leakage should remain.
	for _, k := range []int{2, 32, 200, 500} {
		if mags[k] > 0.01 {
			t.Errorf("bin %d = %g, want near zero away from the peak", k, mags[k])
		}
	}
}

func TestMagnitudesOfSilenceIsZero(t *testing.T) {
	for _, m := range magnitudes(make([]float64, 256)) {
		if m != 0 {
			t.Fatalf("silence produced a non-zero magnitude: %g", m)
		}
	}
}
