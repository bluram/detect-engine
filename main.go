package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/bluram/advert-detect/internal/deps"
	"github.com/bluram/advert-detect/internal/dsp"
)

func main() {
	deps.Verify()

	sr := 22050
	N := 4096

	signal := make([]float64, N)
	for n := range signal {
		t := float64(n) / float64(sr)
		signal[n] = math.Sin(2*math.Pi*240*t) + 0.5*math.Sin(2*math.Pi*880*t)
	}

	spectrum := dsp.Spectrum(signal, sr, 200, 1200, 40)

	fmt.Println("частота   сила")
	for _, bin := range spectrum {
		bar := strings.Repeat("#", int(bin.Strength*120))
		fmt.Printf("  %4.0f Hz  %s\n", bin.Freq, bar)
	}

}
