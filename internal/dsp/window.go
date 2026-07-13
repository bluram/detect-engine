package dsp

import "math"

func Hamming(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

func ApplyWindow(frame, window []float64) []float64 {
	out := make([]float64, len(frame))
	for i := range frame {
		out[i] = frame[i] * window[i]
	}
	return out
}
