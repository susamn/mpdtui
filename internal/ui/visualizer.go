package ui

import (
	"encoding/binary"
	"io"
	"math"
	"math/cmplx"
	"os"
	"sync"
	"time"

	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	sampleRate = 44100
	numSamples = 4096
	numBins    = 40
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
	buf := make([]byte, numSamples*4) // 16-bit stereo = 4 bytes per sample

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

			n, err := io.ReadFull(file, buf)
			if err != nil || n < numSamples*4 {
				time.Sleep(50 * time.Millisecond)
				
				v.mu.Lock()
				for i := range v.amplitudes {
					v.amplitudes[i] *= 0.5
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
				valL := int16(binary.LittleEndian.Uint16(buf[i*4 : i*4+2]))
				valR := int16(binary.LittleEndian.Uint16(buf[i*4+2 : i*4+4]))
				samples[i] = (float64(valL) + float64(valR)) / 2.0 / 32768.0
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
				
				// Log scale to make quieter frequencies visible
				val := math.Log10(avg*500.0 + 1.0) * 0.4
				if val > 1.0 {
					val = 1.0
				}
				newAmps[i] = val
			}

			v.mu.Lock()
			for i := 0; i < numBins; i++ {
				v.amplitudes[i] = v.amplitudes[i]*0.4 + newAmps[i]*0.6
				
				if v.amplitudes[i] >= v.peaks[i] {
					v.peaks[i] = v.amplitudes[i]
				} else {
					v.peaks[i] -= 0.04
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
	// Plot continuous line using Braille characters.
	// Braille characters have 2 columns and 4 rows.
	for i := 0; i < len(v.peaks)-1; i += 2 {
		y1 := v.peaks[i]
		y2 := v.peaks[i+1]

		row1 := int(y1 * 3.99)
		row2 := int(y2 * 3.99)
		if row1 < 0 { row1 = 0 }
		if row1 > 3 { row1 = 3 }
		if row2 < 0 { row2 = 0 }
		if row2 > 3 { row2 = 3 }

		dots1 := []int{0x40, 0x04, 0x02, 0x01}
		dots2 := []int{0x80, 0x20, 0x10, 0x08}

		val := 0x2800 + dots1[row1] + dots2[row2]
		
		// Fill area under the curve? 
		// For a continuous *line*, we just plot the dots.
		out += string(rune(val))
	}
	return out
}
