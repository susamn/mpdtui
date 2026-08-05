package ui

import (
	"encoding/binary"
	"math"
	"math/cmplx"
	"os"
	"sync"
	"time"

	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	sampleRate = 44100
	numSamples = 1024
	numBins    = 20
	fifoPath   = "/tmp/mpd.fifo"
)

var bars = []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

type Visualizer struct {
	mu        sync.Mutex
	amplitudes []float64
	fft        *fourier.FFT
	running    bool
	quit       chan struct{}
}

func NewVisualizer() *Visualizer {
	return &Visualizer{
		amplitudes: make([]float64, numBins),
		fft:        fourier.NewFFT(numSamples),
		quit:       make(chan struct{}),
	}
}

func (v *Visualizer) Start() {
	v.mu.Lock()
	if v.running {
		v.mu.Unlock()
		return
	}
	v.running = true
	v.mu.Unlock()

	go v.readLoop()
}

func (v *Visualizer) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.running {
		return
	}
	close(v.quit)
	v.running = false
}

func (v *Visualizer) readLoop() {
	buf := make([]byte, numSamples*2) // 16-bit PCM = 2 bytes per sample (mono for visualizer is fine, we'll just read raw bytes and treat as mono or interleaved stereo)
	
	for {
		select {
		case <-v.quit:
			return
		default:
		}

		file, err := os.Open(fifoPath)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		for {
			select {
			case <-v.quit:
				file.Close()
				return
			default:
			}

			n, err := file.Read(buf)
			if err != nil || n < numSamples*2 {
				// FIFO empty or closed
				time.Sleep(50 * time.Millisecond)
				
				// decay
				v.mu.Lock()
				for i := range v.amplitudes {
					v.amplitudes[i] *= 0.7
				}
				v.mu.Unlock()
				break 
			}

			// Convert 16-bit PCM to float64
			samples := make([]float64, numSamples)
			for i := 0; i < numSamples; i++ {
				val := int16(binary.LittleEndian.Uint16(buf[i*2 : i*2+2]))
				samples[i] = float64(val) / 32768.0
			}

			// Apply Hann window
			for i := 0; i < numSamples; i++ {
				multiplier := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(numSamples-1)))
				samples[i] *= multiplier
			}

			coeffs := v.fft.Coefficients(nil, samples)
			
			// Map to logarithmic bins
			newAmps := make([]float64, numBins)
			for i := 0; i < numBins; i++ {
				// frequency range: 20Hz to 10000Hz logarithmically
				minFreq := 20.0
				maxFreq := 10000.0
				
				freqStart := minFreq * math.Pow(maxFreq/minFreq, float64(i)/float64(numBins))
				freqEnd := minFreq * math.Pow(maxFreq/minFreq, float64(i+1)/float64(numBins))
				
				idxStart := int(freqStart / (float64(sampleRate) / float64(numSamples)))
				idxEnd := int(freqEnd / (float64(sampleRate) / float64(numSamples)))
				
				if idxStart < 1 {
					idxStart = 1
				}
				if idxEnd >= len(coeffs) {
					idxEnd = len(coeffs) - 1
				}
				if idxStart >= idxEnd {
					idxEnd = idxStart + 1
				}

				sum := 0.0
				for j := idxStart; j < idxEnd; j++ {
					sum += cmplx.Abs(coeffs[j])
				}
				avg := sum / float64(idxEnd-idxStart)
				// scale up
				val := avg * 20.0 
				newAmps[i] = val
			}

			v.mu.Lock()
			for i := 0; i < numBins; i++ {
				// smoothing
				v.amplitudes[i] = v.amplitudes[i]*0.5 + newAmps[i]*0.5
			}
			v.mu.Unlock()
		}
		file.Close()
	}
}

func (v *Visualizer) Render() string {
	v.mu.Lock()
	defer v.mu.Unlock()

	out := ""
	for _, amp := range v.amplitudes {
		idx := int(amp * float64(len(bars)))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(bars) {
			idx = len(bars) - 1
		}
		out += bars[idx]
	}
	return out
}
