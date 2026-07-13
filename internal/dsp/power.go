package dsp

// PowerSpectrum returns the energy at each frequency "bin" of one frame.
// A frame of length N has N/2+1 bins, at frequencies 0, sr/N, 2*sr/N, ... up to
// sr/2. Energy is the magnitude squared (magnitude * magnitude) — that is what
// the mel-band step needs next.
func PowerSpectrum(frame []float64, sampleRate int) []float64 {
	n := len(frame)
	numBins := n/2 + 1
	power := make([]float64, numBins)
	for k := 0; k < numBins; k++ {
		freq := float64(k) * float64(sampleRate) / float64(n)
		mag := MatchFrequency(frame, sampleRate, freq)
		power[k] = mag * mag
	}
	return power
}
