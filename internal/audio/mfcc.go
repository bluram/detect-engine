package audio

import (
	"math"

	"github.com/bluram/detect-engine/internal/dsp"
	"github.com/bluram/detect-engine/internal/fingerprint"
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

// FromFile decodes a media file and returns its fingerprint via the MFCC recipe:
//
//	frames → window → power spectrum → mel bands → log → DCT → binarize
//
// When trim is true (recommended for reference clips) leading/trailing silence
// is removed first.
func FromFile(path string, cfg Config, trim bool) (*fingerprint.Fingerprint, error) {
	cfg = cfg.normalised()
	samples, err := Decode(path, cfg.SampleRate)
	if err != nil {
		return nil, err
	}
	if trim {
		samples = TrimSilence(samples, cfg.SampleRate, -50, 0.05)
	}
	return FromSamples(samples, cfg), nil
}

// FromSamples runs the full MFCC pipeline over mono samples and returns the
// fingerprint — a matrix of 1s and 0s that captures the *shape* of the sound
// over time while ignoring absolute loudness.
func FromSamples(samples []float64, cfg Config) *fingerprint.Fingerprint {
	cfg = cfg.normalised()

	frames := dsp.Frames(samples, cfg.FrameLen, cfg.FrameStep)
	window := dsp.Hamming(cfg.FrameLen)
	filterbank := melFilterbank(cfg.NumFilters, cfg.FrameLen, cfg.SampleRate, cfg.LowFreqHz, cfg.HighFreqHz)
	dct := dctMatrix(cfg.NumCoeffs, cfg.NumFilters)

	bands := cfg.NumCoeffs - 1
	coeffs := make([][]float64, bands)
	for b := range coeffs {
		coeffs[b] = make([]float64, len(frames))
	}

	for t, frame := range frames {
		windowed := dsp.ApplyWindow(frame, window)
		power := dsp.PowerSpectrum(windowed)
		logMel := melEnergies(power, filterbank)
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

	return &fingerprint.Fingerprint{
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
