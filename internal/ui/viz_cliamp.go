package ui

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"mpdtui/internal/audio"
	"mpdtui/internal/mpdclient"
)

// cliampVisualization renders a retro Winamp / CLIAMP inspired spectrum analyzer
// with discrete multi-band frequency bars, vibrant color gradients, and falling
// peak caps.
//
// Like the Equalizer visualization, it has two sources for those bars:
//
//   - Real spectrum: when MPD's fifo output is feeding internal/audio (see
//     Visualization's doc comment in visualizer.go), each bar is one
//     logarithmically spaced band of the actual decoded audio -- so bass really
//     is on the left, treble really is on the right, and the bars are the music.
//   - Fallback: with no live audio, cliampBandLevel simulates that same layout
//     from playback state and elapsed time alone -- kick-tempo pulses at the
//     bass end, harmonic movement through the mids, fast flutter in the treble.
//     Decorative, but shaped like the real thing so the panel reads the same way
//     for a setup without the fifo configured.
//
// Each frequency bar tracks its highest recent position with a floating peak cap
// (Unicode upper-eighth block '▔') that holds momentarily before descending under
// simulated gravity. That runs identically on top of either source.
type cliampVisualization struct {
	// spectrum may be nil (tests construct it that way), which behaves
	// exactly like a feed that is never active: the simulation is used.
	spectrum *audio.Spectrum

	mu        sync.Mutex
	peaks     []float64
	peakTimes []time.Duration
	lastTime  time.Duration
}

func newCliampVisualization(spectrum *audio.Spectrum) *cliampVisualization {
	return &cliampVisualization{spectrum: spectrum}
}

func (c *cliampVisualization) Name() string {
	return "Cliamp"
}

const (
	cliampPeakHoldDuration = 120 * time.Millisecond // Time peak stays before falling
	cliampPeakGravity      = 14.0                   // Levels per second descent rate
)

func (c *cliampVisualization) Render(width, height int, elapsed time.Duration, st mpdclient.Status) []string {
	lines := make([]string, height)
	if width <= 0 || height <= 0 {
		return lines
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If clock restarted or rewound, reset peak tracking.
	if elapsed < c.lastTime {
		c.peaks = nil
		c.peakTimes = nil
	}
	c.lastTime = elapsed

	// Determine bar layout: barWidth and gap between bars.
	barWidth := 2
	gap := 1
	if width < 12 {
		barWidth = 1
		gap = 0
	} else if width < 26 {
		barWidth = 1
		gap = 1
	}

	numBars := (width + gap) / (barWidth + gap)
	if numBars < 1 {
		numBars = 1
	}

	// Ensure peak tracker slices match current bar count.
	if len(c.peaks) != numBars {
		c.peaks = make([]float64, numBars)
		c.peakTimes = make([]time.Duration, numBars)
	}

	maxLevel := height * 8
	maxLevelF := float64(maxLevel)

	// Calculate volume scaling (0.0 .. 1.0).
	volumeScale := float64(st.Volume) / 100.0
	if st.Volume < 0 {
		volumeScale = 0.8 // fallback for unknown volume
	} else if volumeScale > 1.0 {
		volumeScale = 1.0
	} else if volumeScale < 0.0 {
		volumeScale = 0.0
	}

	// Calculate current bar levels and update peak caps.
	barLevels := make([]int, numBars)
	peakLevels := make([]int, numBars)

	isPlaying := st.State == mpdclient.StatePlay
	t := elapsed.Seconds()

	// One band per bar from the live feed; nil when there's no audio to
	// read, which drops through to the simulation below.
	var bands []float64
	if isPlaying {
		bands = c.spectrum.Bands(numBars)
	}

	for b := 0; b < numBars; b++ {
		var targetLevel float64
		switch {
		case !isPlaying || volumeScale <= 0:
			targetLevel = 0
		case bands != nil:
			targetLevel = bands[b] * volumeScale * maxLevelF
		default:
			targetLevel = cliampBandLevel(t, b, numBars, volumeScale, maxLevelF)
		}

		// Peak hold and gravity decay
		if targetLevel >= c.peaks[b] {
			c.peaks[b] = targetLevel
			c.peakTimes[b] = elapsed
		} else if isPlaying {
			dt := (elapsed - c.peakTimes[b]).Seconds()
			if dt > cliampPeakHoldDuration.Seconds() {
				decay := cliampPeakGravity * (dt - cliampPeakHoldDuration.Seconds())
				c.peaks[b] -= decay
				if c.peaks[b] < targetLevel {
					c.peaks[b] = targetLevel
				}
				// Advance peak time reference to preserve continuous integration
				c.peakTimes[b] = elapsed - cliampPeakHoldDuration
			}
		} else {
			// When stopped or paused, reset peaks immediately
			c.peaks[b] = 0
		}

		barLevels[b] = int(math.Round(targetLevel))
		if barLevels[b] > maxLevel {
			barLevels[b] = maxLevel
		}
		if barLevels[b] < 0 {
			barLevels[b] = 0
		}

		peakLevels[b] = int(math.Round(c.peaks[b]))
		if peakLevels[b] > maxLevel {
			peakLevels[b] = maxLevel
		}
		if peakLevels[b] < 0 {
			peakLevels[b] = 0
		}
	}

	// Calculate horizontal centering padding
	totalUsed := numBars*barWidth + (numBars-1)*gap
	padLeft := (width - totalUsed) / 2
	if padLeft < 0 {
		padLeft = 0
	}
	padRight := width - totalUsed - padLeft
	if padRight < 0 {
		padRight = 0
	}

	padLeftStr := strings.Repeat(" ", padLeft)
	padRightStr := strings.Repeat(" ", padRight)
	gapStr := strings.Repeat(" ", gap)

	// Render row by row (y=0 is top row, y=height-1 is bottom row)
	for y := 0; y < height; y++ {
		var rowBuilder strings.Builder
		rowBuilder.WriteString(padLeftStr)

		rowFromBottom := height - 1 - y // 0 for bottom row, 1 for top row (in 2-row layout)
		rowBaseLevel := rowFromBottom * 8

		for b := 0; b < numBars; b++ {
			if b > 0 && gap > 0 {
				rowBuilder.WriteString(gapStr)
			}

			barL := barLevels[b]
			peakL := peakLevels[b]

			// Calculate how many eighth-blocks fill this row (0..8)
			rowFilled := barL - rowBaseLevel
			if rowFilled > 8 {
				rowFilled = 8
			} else if rowFilled < 0 {
				rowFilled = 0
			}

			var cellContent string
			if rowFilled > 0 {
				glyph := equalizerBlocks[rowFilled]
				color := cliampColorForLevel(barL, maxLevel)
				cellContent = fmt.Sprintf("[%s]%s[-]", color, strings.Repeat(string(glyph), barWidth))
			} else {
				// Check if the falling peak cap sits in this empty cell
				// Peak level must fall in this row's level range and be strictly above the bar
				peakInRow := peakL > rowBaseLevel && peakL <= rowBaseLevel+8
				if peakInRow && peakL > barL {
					// '▔' is Unicode U+2594 (Upper 1/8th block)
					cellContent = fmt.Sprintf("[#ff1744]%s[-]", strings.Repeat("▔", barWidth))
				} else {
					cellContent = strings.Repeat(" ", barWidth)
				}
			}

			rowBuilder.WriteString(cellContent)
		}

		rowBuilder.WriteString(padRightStr)
		lines[y] = rowBuilder.String()
	}

	return lines
}

// cliampBandLevel computes the simulated frequency energy for band index `b` out
// of `numBars` at time `t`, scaled by `volumeScale` (0..1) and `maxLevel`. This
// is the fallback path only -- it runs when there is no live audio to draw from.
func cliampBandLevel(t float64, b, numBars int, volumeScale, maxLevel float64) float64 {
	// Normalized frequency position: 0.0 (sub-bass) to 1.0 (highest treble)
	var f float64
	if numBars > 1 {
		f = float64(b) / float64(numBars-1)
	} else {
		f = 0.5
	}

	// 1. Bass / Sub-bass component (f < 0.4)
	// 130 BPM kick beat envelope + sub-bass resonance
	beat := math.Sin(2.0 * math.Pi * 2.16 * t)
	if beat < 0 {
		beat = 0
	}
	beat = beat * beat * beat * beat // sharp transient punch

	sub := 0.5 * (math.Sin(3.2*t+0.5*float64(b)) + 1.0)
	bassEnvelope := math.Max(0, 1.0-1.6*f)
	bassEnergy := (beat*1.3 + sub*0.7) * bassEnvelope

	// 2. Mid frequencies component (0.2 < f < 0.8)
	// Harmonic waves + syncopated rhythmic variations
	w1 := 0.5 * (math.Sin(5.3*t+3.2*f*math.Pi) + 1.0)
	w2 := 0.5 * (math.Cos(3.7*t-2.8*f*math.Pi) + 1.0)
	sync := math.Sin(2.0*math.Pi*1.08*t + 1.8*float64(b))
	if sync < 0 {
		sync = 0
	}
	sync = sync * sync * sync
	midEnvelope := math.Max(0, 1.0-2.4*math.Abs(f-0.45))
	midEnergy := (w1*0.4 + w2*0.35 + sync*0.5) * midEnvelope

	// 3. Treble / High frequencies component (f > 0.45)
	// Fast shimmer and rapid flutter
	f1 := math.Sin(16.5*t + 5.1*float64(b))
	if f1 < 0 {
		f1 = 0
	}
	f2 := math.Cos(24.2*t + 2.7*float64(b))
	if f2 < 0 {
		f2 = 0
	}
	trebleFlutter := f1*f1*0.6 + f2*f2*0.4
	trebleEnvelope := math.Max(0, (f-0.35)/0.65)
	trebleEnergy := trebleFlutter * trebleEnvelope

	// Combined multi-band raw energy
	raw := bassEnergy*1.15 + midEnergy*1.0 + trebleEnergy*1.1

	// Gentle baseline floor so bars have dynamic activity
	floor := 0.06 * (math.Sin(1.5*t+float64(b)) + 1.0)

	norm := (raw + floor) / 1.35
	if norm > 1.0 {
		norm = 1.0
	} else if norm < 0.0 {
		norm = 0.0
	}

	return norm * volumeScale * maxLevel
}

// cliampColorForLevel returns dynamic hex color tags matching classic Winamp /
// Cliamp spectrum styling (green -> yellow -> orange -> red).
func cliampColorForLevel(level, maxLevel int) string {
	ratio := float64(level) / float64(maxLevel)
	switch {
	case ratio >= 0.85:
		return "#ff1744" // vibrant red / peak
	case ratio >= 0.60:
		return "#ff9100" // warm amber / high
	case ratio >= 0.35:
		return "#ffd600" // bright yellow / mid
	default:
		return "#00e676" // vibrant green / low
	}
}
