// Package video is the video path of the engine: it decodes a media file's
// picture track into small grayscale frames using ffmpeg (decode.go) and turns
// those frames into a binary fingerprint via a perceptual hash (dhash.go).
//
// It mirrors package audio and produces the same media-neutral
// fingerprint.Fingerprint, so the matcher and the .bin store work on its output
// unchanged — the only difference from audio is how the bit grid is filled.
package video

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// Frame is one decoded frame: Width×Height 8-bit grayscale pixels in row-major
// order, so the pixel at (x, y) is Pix[y*Width+x] (0 = black, 255 = white).
type Frame struct {
	Width  int
	Height int
	Pix    []byte
}

// Decode extracts frames from a media file as small grayscale images using
// ffmpeg. It samples fps frames per second and scales every frame down to
// width×height, grayscale — two steps that make the later hash robust:
//   - shrinking discards resolution, so 360p and 720p of the same ad decode to
//     the same tiny image;
//   - grayscale drops colour, keeping only the stable brightness structure.
//
// ffmpeg does the sampling, scaling and grayscale in one filter, so here we only
// read the raw bytes and slice them into fixed-size frames.
func Decode(path string, fps, width, height int) ([]Frame, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("video: file not found: %s", path)
	}

	// fps=N picks N frames per second; scale forces the tiny size; format=gray
	// makes each pixel a single byte. -pix_fmt gray on output keeps it 1 byte.
	filter := fmt.Sprintf("fps=%d,scale=%d:%d,format=gray", fps, width, height)
	cmd := exec.Command("ffmpeg",
		"-v", "error",
		"-nostdin",
		"-i", path,
		"-vf", filter,
		"-pix_fmt", "gray",
		"-f", "rawvideo",
		"-",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("video: ffmpeg failed on %s: %w\n%s", path, err, stderr.String())
	}

	raw := stdout.Bytes()
	frameSize := width * height
	if frameSize == 0 {
		return nil, fmt.Errorf("video: invalid frame size %dx%d", width, height)
	}
	count := len(raw) / frameSize // whole frames only; drop any trailing partial
	if count == 0 {
		return nil, fmt.Errorf("video: no frames decoded from %s (no video track?)", path)
	}

	frames := make([]Frame, count)
	for i := range count {
		off := i * frameSize
		frames[i] = Frame{
			Width:  width,
			Height: height,
			Pix:    raw[off : off+frameSize],
		}
	}
	return frames, nil
}
