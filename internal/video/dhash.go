package video

import "github.com/bluram/detect-engine/internal/fingerprint"

// Config holds the video fingerprint parameters. The zero value is invalid;
// start from DefaultConfig and override what you need.
type Config struct {
	Fps   int // frames sampled per second
	HashW int // hash bits per row; frames are decoded HashW+1 wide (dHash needs the extra column)
	HashH int // hash rows; frames are decoded HashH tall
}

// DefaultConfig returns an 8×8 dHash (64 bits) sampled at 3 frames per second —
// enough time resolution for short clips, and a tiny, robust fingerprint.
func DefaultConfig() Config {
	return Config{Fps: 3, HashW: 8, HashH: 8}
}

func (c Config) normalised() Config {
	if c.Fps == 0 {
		c.Fps = 3
	}
	if c.HashW == 0 {
		c.HashW = 8
	}
	if c.HashH == 0 {
		c.HashH = 8
	}
	return c
}

// FromFile decodes a media file's picture track and returns its video
// fingerprint. Frames are decoded HashW+1 wide so dHash has a right neighbour
// for every hash column.
func FromFile(path string, cfg Config) (*fingerprint.Fingerprint, error) {
	cfg = cfg.normalised()
	frames, err := Decode(path, cfg.Fps, cfg.HashW+1, cfg.HashH)
	if err != nil {
		return nil, err
	}
	return FromFrames(frames, cfg), nil
}

// FromFrames turns decoded frames into a fingerprint using dHash (difference
// hash): within each frame, every pixel is compared to its right neighbour —
// brighter → 1, darker → 0 — giving HashW*HashH bits per frame. Those bits form
// the bands (rows) of the fingerprint; the frames form the columns (time). The
// result is the same media-neutral Fingerprint the matcher already consumes.
func FromFrames(frames []Frame, cfg Config) *fingerprint.Fingerprint {
	cfg = cfg.normalised()

	bands := cfg.HashW * cfg.HashH
	bits := make([][]int8, bands)
	for b := range bits {
		bits[b] = make([]int8, len(frames))
	}

	for t, frame := range frames {
		for y := range cfg.HashH {
			row := y * frame.Width
			for x := range cfg.HashW {
				if frame.Pix[row+x] > frame.Pix[row+x+1] { // brighter than right neighbour
					bits[y*cfg.HashW+x][t] = 1
				}
			}
		}
	}

	// SampleRate=Fps with HopSamples=1 makes FrameToSeconds = frame/Fps, so the
	// matcher's timing and one-second window logic work unchanged.
	return &fingerprint.Fingerprint{
		Bits:       bits,
		SampleRate: cfg.Fps,
		HopSamples: 1,
	}
}
