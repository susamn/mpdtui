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

var bars = []string{" ", ".", "-", "~", "*", "'", "`"}

type Visualizer struct {
	mu         sync.Mutex
	amplitudes []float64
	peaks      []float64
	fft        *fourier.FFT
	running    bool
	quit       chan struct{}
}

func NewVisualizer() *Visualizer {
	return &Visualizer{
		amplitudes: make([]float64, numBins),
		peaks:      make([]float64, numBins),
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
	buf := make([]byte, numSamples*2)

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
				time.Sleep(50 * time.Millisecond)
				
				v.mu.Lock()
				for i := range v.amplitudes {
					v.amplitudes[i] *= 0.7
					v.peaks[i] -= 0.1
					if v.peaks[i] < 0 {
						v.peaks[i] = 0
					}
				}
				v.mu.Unlock()
				break 
			}

			samples := make([]float64, numSamples)
			for i := 0; i < numSamples; i++ {
				val := int16(binary.LittleEndian.Uint16(buf[i*2 : i*2+2]))
				samples[i] = float64(val) / 32768.0
			}

			for i := 0; i < numSamples; i++ {
				multiplier := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(numSamples-1)))
				samples[i] *= multiplier
			}

			coeffs := v.fft.Coefficients(nil, samples)
			
			newAmps := make([]float64, numBins)
			for i := 0; i < numBins; i++ {
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
				val := avg * 20.0 
				newAmps[i] = val
			}

			v.mu.Lock()
			for i := 0; i < numBins; i++ {
				v.amplitudes[i] = v.amplitudes[i]*0.5 + newAmps[i]*0.5
				
				// Raindrop peak physics
				if v.amplitudes[i] >= v.peaks[i] {
					v.peaks[i] = v.amplitudes[i]
				} else {
					v.peaks[i] -= 0.05 // gravity pull down
					if v.peaks[i] < 0 {
						v.peaks[i] = 0
					}
				}
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
	for _, peak := range v.peaks {
		idx := int(peak * float64(len(bars)))
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
