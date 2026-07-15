// Package fingerprint turns audio samples into a compact, noise-robust
// fingerprint using the MFCC recipe:
//
//	frames → window → power spectrum → mel bands → log → DCT → binarize
//
// The result is a matrix of 1s and 0s that captures the *shape* of the sound
// over time while ignoring absolute loudness — ideal for matching.
package fingerprint

import (
	"math"

	"github.com/bluram/detect-engine/internal/audio"
	"github.com/bluram/detect-engine/internal/dsp"
)

// epsilon guards the logarithm against log(0).
const epsilon = 1e-10

// Config holds the parameters of the MFCC pipeline. The zero value is invalid;
// start from DefaultConfig and override what you need. FrameLen must be a power
// of two (required by the FFT).
type Config struct {
	SampleRate int
	FrameLen   int
	FrameStep  int
	NumFilters int     // number of mel bands
	NumCoeffs  int     // cepstral coefficients computed; coeff 0 is dropped
	LowFreqHz  float64 // lower edge of the mel filterbank
	HighFreqHz float64 // upper edge; 0 means SampleRate/2
}

// DefaultConfig returns sensible parameters for broadcast audio.
func DefaultConfig() Config {
	return Config{
		SampleRate: 22050,
		FrameLen:   512,
		FrameStep:  256,
		NumFilters: 26,
		NumCoeffs:  13,
		LowFreqHz:  0,
		HighFreqHz: 0,
	}
}

func (c Config) normalised() Config {
	if c.SampleRate == 0 {
		c.SampleRate = 22050
	}
	if c.FrameLen == 0 {
		c.FrameLen = 512
	}
	if c.FrameStep == 0 {
		c.FrameStep = 256
	}
	if c.NumFilters == 0 {
		c.NumFilters = 26
	}
	if c.NumCoeffs == 0 {
		c.NumCoeffs = 13
	}
	if c.HighFreqHz == 0 {
		c.HighFreqHz = float64(c.SampleRate) / 2
	}
	return c
}

// Fingerprint is a binary MFCC fingerprint plus the metadata needed to convert
// frame positions back into seconds.
type Fingerprint struct {
	Bits       [][]int8 // [band][frame], each value 0 or 1
	SampleRate int
	HopSamples int // samples advanced between consecutive frames
}

// NumBands reports how many bands (rows) the fingerprint has.
func (f *Fingerprint) NumBands() int { return len(f.Bits) }

// NumFrames reports how many frames (columns) the fingerprint has.
func (f *Fingerprint) NumFrames() int {
	if len(f.Bits) == 0 {
		return 0
	}
	return len(f.Bits[0])
}

// FrameToSeconds converts a frame index into seconds from the start.
func (f *Fingerprint) FrameToSeconds(frame int) float64 {
	return float64(frame) * float64(f.HopSamples) / float64(f.SampleRate)
}

// DurationSeconds returns the fingerprint's length in seconds.
func (f *Fingerprint) DurationSeconds() float64 {
	return f.FrameToSeconds(f.NumFrames())
}

// FromFile decodes a media file and returns its fingerprint. When trim is true
// (recommended for reference clips) leading/trailing silence is removed first.
func FromFile(path string, cfg Config, trim bool) (*Fingerprint, error) {
	cfg = cfg.normalised()
	samples, err := audio.Decode(path, cfg.SampleRate)
	if err != nil {
		return nil, err
	}
	if trim {
		samples = audio.TrimSilence(samples, cfg.SampleRate, -50, 0.05)
	}
	return FromSamples(samples, cfg), nil
}

// FromSamples runs the full MFCC pipeline over mono samples and returns the
// fingerprint.
func FromSamples(samples []float64, cfg Config) *Fingerprint {
	cfg = cfg.normalised()

	frames := dsp.Frames(samples, cfg.FrameLen, cfg.FrameStep)
	window := dsp.Hamming(cfg.FrameLen)
	filterbank := MelFilterbank(cfg.NumFilters, cfg.FrameLen, cfg.SampleRate, cfg.LowFreqHz, cfg.HighFreqHz)
	dct := dctMatrix(cfg.NumCoeffs, cfg.NumFilters)

	bands := cfg.NumCoeffs - 1
	coeffs := make([][]float64, bands)
	for b := range coeffs {
		coeffs[b] = make([]float64, len(frames))
	}

	for t, frame := range frames {
		windowed := dsp.ApplyWindow(frame, window)
		power := dsp.PowerSpectrum(windowed)
		logMel := MelEnergies(power, filterbank)
		for i, e := range logMel {
			if e < epsilon {
				e = epsilon
			}
			logMel[i] = math.Log(e)
		}
		for b, c := range applyDCT(logMel, dct) {
			coeffs[b][t] = c
		}
	}

	return &Fingerprint{
		Bits:       binarize(coeffs),
		SampleRate: cfg.SampleRate,
		HopSamples: cfg.FrameStep,
	}
}

// binarize turns each band into 1s and 0s: 1 where the coefficient is above the
// band's own average, else 0. This discards absolute level and keeps only the
// pattern, which is what makes the fingerprint robust to volume and noise.
func binarize(coeffs [][]float64) [][]int8 {
	bits := make([][]int8, len(coeffs))
	for b, row := range coeffs {
		var sum float64
		for _, v := range row {
			sum += v
		}
		mean := sum / float64(len(row))

		out := make([]int8, len(row))
		for t, v := range row {
			if v > mean {
				out[t] = 1
			}
		}
		bits[b] = out
	}
	return bits
}
