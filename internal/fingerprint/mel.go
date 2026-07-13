// Package fingerprint turns audio into a compact fingerprint using the MFCC
// recipe (mel bands → log → DCT → binarize). It builds on the generic tools in
// the dsp package.
package fingerprint

import "math"

// hzToMel / melToHz convert between real frequency (Hz) and the "mel" scale,
// which matches how humans hear (equal steps in mel sound equally spaced to us).
func hzToMel(hz float64) float64  { return 2595 * math.Log10(1+hz/700) }
func melToHz(mel float64) float64 { return 700 * (math.Pow(10, mel/2595) - 1) }

// MelFilterbank builds numFilters triangular filters, spaced evenly on the mel
// scale between lowFreq and highFreq. Each returned row has numBins weights
// (numBins = frameLen/2 + 1) to be multiplied with a power spectrum.
// Low filters come out narrow, high filters wide — just like human hearing.
func MelFilterbank(numFilters, frameLen, sampleRate int, lowFreq, highFreq float64) [][]float64 {
	numBins := frameLen/2 + 1

	lowMel := hzToMel(lowFreq)
	highMel := hzToMel(highFreq)

	// numFilters+2 points, evenly spaced on the mel scale, mapped back to bins.
	points := make([]int, numFilters+2)
	for i := range points {
		mel := lowMel + (highMel-lowMel)*float64(i)/float64(numFilters+1)
		hz := melToHz(mel)
		points[i] = int(math.Floor(float64(frameLen+1) * hz / float64(sampleRate)))
	}

	filterbank := make([][]float64, numFilters)
	for f := 0; f < numFilters; f++ {
		row := make([]float64, numBins)
		left, center, right := points[f], points[f+1], points[f+2]
		// rising edge of the triangle
		for k := left; k < center; k++ {
			if k >= 0 && k < numBins && center > left {
				row[k] = float64(k-left) / float64(center-left)
			}
		}
		// falling edge of the triangle
		for k := center; k < right; k++ {
			if k >= 0 && k < numBins && right > center {
				row[k] = float64(right-k) / float64(right-center)
			}
		}
		filterbank[f] = row
	}
	return filterbank
}

// MelEnergies applies the filterbank to a power spectrum, returning one energy
// number per mel band (power weighted by each triangle, then summed).
func MelEnergies(power []float64, filterbank [][]float64) []float64 {
	energies := make([]float64, len(filterbank))
	for f, row := range filterbank {
		var sum float64
		for k, w := range row {
			sum += power[k] * w
		}
		energies[f] = sum
	}
	return energies
}
