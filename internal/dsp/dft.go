package dsp

import "math"

func MatchFrequency(signal []float64, sampleRate int, freq float64) float64 {
	var re, im float64
	for n, s := range signal {
		angle := 2 * math.Pi * freq * float64(n) / float64(sampleRate)
		re += s * math.Cos(angle)
		im += s * math.Sin(angle)
	}
	return math.Sqrt(re*re+im*im) / float64(len(signal))
}
