// Package audio turns MPD's raw PCM output into the frequency-domain
// data a visualization can actually draw.
//
// MPD's client protocol (port 6600) exposes no audio data at all -- no
// spectrum, no waveform, nothing but playback status. MPD the *server*,
// however, can be told to duplicate its decoded output into a named
// pipe via the "fifo" output plugin:
//
//	audio_output {
//	    type   "fifo"
//	    name   "Visualizer feed"
//	    path   "/tmp/mpd.fifo"
//	    format "44100:16:2"
//	}
//
// That pipe carries real, decoded PCM -- the same trick ncmpcpp's
// visualizer uses -- and is the only way to draw something that actually
// responds to the music. This package reads that pipe (Spectrum, in
// spectrum.go) and reduces it to per-band magnitudes; this file holds
// the transform itself.
package audio

import "math"

// fft computes the discrete Fourier transform of re/im in place, using
// an iterative radix-2 Cooley-Tukey decimation-in-time. len(re) must be
// a power of two and len(im) must match.
//
// Hand-rolled rather than pulled from gonum: this is the only numerical
// routine mpdtui needs, and a ~40-line in-place transform is a better
// trade than a dependency on a general-purpose scientific library for a
// tool that otherwise has almost none.
func fft(re, im []float64) {
	n := len(re)
	if n < 2 {
		return
	}

	// Bit-reversal permutation: reorder the input so the butterflies
	// below can run bottom-up over contiguous strides.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}

	for size := 2; size <= n; size <<= 1 {
		angle := -2 * math.Pi / float64(size)
		wRe, wIm := math.Cos(angle), math.Sin(angle)
		for start := 0; start < n; start += size {
			curRe, curIm := 1.0, 0.0
			for k := 0; k < size/2; k++ {
				i, j := start+k, start+k+size/2
				// t = cur * x[j]
				tRe := curRe*re[j] - curIm*im[j]
				tIm := curRe*im[j] + curIm*re[j]
				re[j] = re[i] - tRe
				im[j] = im[i] - tIm
				re[i] += tRe
				im[i] += tIm
				curRe, curIm = curRe*wRe-curIm*wIm, curRe*wIm+curIm*wRe
			}
		}
	}
}

// hannWindow returns an n-point Hann window. Applied before the
// transform so a sample block that doesn't contain a whole number of
// cycles (i.e. essentially always) doesn't smear its energy across every
// bin -- untapered blocks make a spectrum look like uniform noise.
func hannWindow(n int) []float64 {
	w := make([]float64, n)
	if n < 2 {
		for i := range w {
			w[i] = 1
		}
		return w
	}
	for i := range w {
		w[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return w
}

// hannGain is the coherent gain of a Hann window (its mean value): a
// windowed sinusoid's bin magnitude is scaled by this, so magnitudes are
// divided by it to recover the true amplitude.
const hannGain = 0.5

// magnitudes runs one windowed FFT over samples (which must be a power
// of two long) and returns the amplitude of bins 0..n/2, normalized so a
// full-scale sine at a bin centre reads 1.0.
func magnitudes(samples []float64) []float64 {
	n := len(samples)
	if n < 2 {
		return nil
	}
	re := make([]float64, n)
	im := make([]float64, n)
	window := hannWindow(n)
	for i, s := range samples {
		re[i] = s * window[i]
	}
	fft(re, im)

	half := n/2 + 1
	out := make([]float64, half)
	scale := 2 / (float64(n) * hannGain)
	for k := 0; k < half; k++ {
		out[k] = math.Hypot(re[k], im[k]) * scale
	}
	return out
}
