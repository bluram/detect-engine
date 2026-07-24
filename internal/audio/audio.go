// Package audio decodes media files into mono float samples using ffmpeg.
//
// ffmpeg is invoked as a subprocess, so any format it understands works —
// including video files, whose audio track is extracted automatically.
package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// mediaExtensions are the file types ffmpeg can decode that we treat as media.
var mediaExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".flac": true,
	".ogg": true, ".mp4": true, ".mkv": true, ".mov": true, ".avi": true,
}

// IsMediaFile reports whether name has a media extension we accept.
func IsMediaFile(name string) bool {
	return mediaExtensions[strings.ToLower(filepath.Ext(name))]
}

// Decode returns mono float64 samples decoded at the given sample rate.
// Sample values are normalised to roughly [-1.0, 1.0].
func Decode(path string, sampleRate int) ([]float64, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("audio: file not found: %s", path)
	}

	cmd := exec.Command("ffmpeg",
		"-v", "error",
		"-nostdin",
		"-i", path,
		// Preserve the true timeline. Broadcast captures can contain gaps
		// (dropped samples during recording); a plain decode packs samples
		// contiguously and collapses those gaps, so every later timestamp
		// drifts earlier than real (container) time. aresample=async fills
		// gaps with silence and first_pts=0 anchors the start, keeping decoded
		// sample-time equal to real record time — so match timestamps are accurate.
		"-af", "aresample=async=1:first_pts=0",
		"-ac", "1", // mono
		"-ar", fmt.Sprint(sampleRate), // target sample rate
		"-f", "f32le", // raw 32-bit float, little-endian
		"-",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("audio: ffmpeg failed on %s: %w\n%s", path, err, stderr.String())
	}

	raw := stdout.Bytes()
	samples := make([]float64, len(raw)/4)
	for i := range samples {
		bits := binary.LittleEndian.Uint32(raw[i*4:])
		samples[i] = float64(math.Float32frombits(bits))
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("audio: no samples decoded from %s", path)
	}
	return samples, nil
}

// TrimSilence removes leading and trailing near-silence from a clip so its
// fingerprint aligns with the recording. thresholdDB sets the silence level
// (e.g. -50), padSeconds keeps a little audio around the trimmed edges.
func TrimSilence(samples []float64, sampleRate int, thresholdDB, padSeconds float64) []float64 {
	if len(samples) == 0 {
		return samples
	}
	amplitude := math.Pow(10, thresholdDB/20)

	first := -1
	for i, v := range samples {
		if math.Abs(v) > amplitude {
			first = i
			break
		}
	}
	if first < 0 {
		return samples // entirely silent
	}

	last := len(samples)
	for i := len(samples) - 1; i >= 0; i-- {
		if math.Abs(samples[i]) > amplitude {
			last = i + 1
			break
		}
	}

	pad := int(padSeconds * float64(sampleRate))
	start := max(0, first-pad)
	end := min(len(samples), last+pad)
	return samples[start:end]
}
