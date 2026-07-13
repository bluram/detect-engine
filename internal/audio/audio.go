package audio

import (
	"encoding/binary"
	"math"
	"os/exec"
)

func Decode(path string) ([]float64, error) {
	cmd := exec.Command("ffmpeg",
		"-v", "error",
		"-i", path,
		"-ac", "1",
		"-ar", "22050",
		"-f", "f32le",
		"-",
	)

	raw, err := cmd.Output()

	if err != nil {
		return nil, err
	}

	samples := make([]float64, len(raw)/4)

	for i := range samples {
		bits := binary.LittleEndian.Uint32(raw[i*4:])
		samples[i] = float64(math.Float32frombits(bits))
	}

	return samples, nil
}
