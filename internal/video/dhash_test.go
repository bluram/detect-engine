package video

import (
	"math"
	"testing"
)

// synthFrames builds n deterministic w×h grayscale frames with a varied pattern
// so the hash has a mix of 0s and 1s (not a degenerate all-zero grid).
func synthFrames(n, w, h int) []Frame {
	frames := make([]Frame, n)
	for i := range n {
		pix := make([]byte, w*h)
		for y := range h {
			for x := range w {
				pix[y*w+x] = byte((x*53 + y*97 + i*29) % 256)
			}
		}
		frames[i] = Frame{Width: w, Height: h, Pix: pix}
	}
	return frames
}

func TestFromFramesShapeTimingDeterminism(t *testing.T) {
	cfg := DefaultConfig()
	frames := synthFrames(5, cfg.HashW+1, cfg.HashH)

	a := FromFrames(frames, cfg)
	b := FromFrames(frames, cfg)

	if got := a.NumBands(); got != 64 { // 8×8 dHash
		t.Fatalf("expected 64 bands, got %d", got)
	}
	if got := a.NumFrames(); got != 5 {
		t.Fatalf("expected 5 frames, got %d", got)
	}

	// 5 frames at 3 fps → 5/3 seconds.
	if got := a.DurationSeconds(); math.Abs(got-5.0/3.0) > 1e-9 {
		t.Fatalf("expected duration 5/3s, got %v", got)
	}

	for band := range a.Bits {
		for f := range a.Bits[band] {
			if a.Bits[band][f] != b.Bits[band][f] {
				t.Fatalf("fingerprint not deterministic at band %d frame %d", band, f)
			}
		}
	}
}
