// Package digital signal processing
package dsp

type Bin struct {
	Freq     float64
	Strength float64
}

func Spectrum(signal []float64, sampleRate int, fromHz, toHz, stepHz float64) []Bin {
	var bins []Bin
	for f := fromHz; f <= toHz; f += stepHz {
		bins = append(bins, Bin{
			Freq:     f,
			Strength: MatchFrequency(signal, sampleRate, f),
		})
	}
	return bins
}
