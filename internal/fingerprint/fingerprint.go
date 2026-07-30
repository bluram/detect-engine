// Package fingerprint defines the binary fingerprint the engine matches: a grid
// of 0/1 bits ([band][frame]) plus the timing metadata needed to turn frame
// positions back into seconds. It also serialises fingerprints to disk (.bin,
// see store.go).
//
// The type is media-neutral: the bits may come from audio (the MFCC pipeline in
// package audio) or, later, from video frame hashes. Whatever fills the grid,
// the matcher and the .bin store operate on it unchanged.
package fingerprint

// Fingerprint is a binary fingerprint plus the metadata needed to convert
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
