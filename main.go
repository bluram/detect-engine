package main

import (
	"fmt"
	"strings"

	"github.com/bluram/advert-detect/internal/audio"
	"github.com/bluram/advert-detect/internal/dsp"
)

func main() {
	sr := 22050

	samples, err := audio.Decode("advert.mp3")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// cut into frames, then take ONE frame (~5th second) and fade its edges
	frames := dsp.Frames(samples, 512, 256)
	window := dsp.Hamming(512)
	frame := dsp.ApplyWindow(frames[400], window)

	// spectrum (prism) of this single frame
	power := dsp.PowerSpectrum(frame, sr)

	// find the biggest, to scale the bars
	var max float64
	for _, p := range power {
		if p > max {
			max = p
		}
	}
	if max == 0 {
		max = 1
	}

	// print the low bins (that is where most sound energy is)
	fmt.Println("power spectrum of ONE frame:")
	for k := 0; k < 60; k++ {
		freq := float64(k) * float64(sr) / 512
		bar := strings.Repeat("#", int(power[k]/max*50))
		fmt.Printf("  %5.0f Hz  %s\n", freq, bar)
	}
}
