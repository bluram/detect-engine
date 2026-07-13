package dsp

// PowerSpectrum returns the energy at each frequency bin of one frame, computed
// with the FFT. A frame of length N (which must be a power of two) yields
// N/2+1 bins; bin k corresponds to frequency k*sampleRate/N. Energy is the
// squared magnitude of the complex FFT value.
func PowerSpectrum(frame []float64) []float64 {
	n := len(frame)

	buf := make([]complex128, n)
	for i, s := range frame {
		buf[i] = complex(s, 0)
	}
	FFT(buf)

	bins := n/2 + 1
	power := make([]float64, bins)
	for k := 0; k < bins; k++ {
		re, im := real(buf[k]), imag(buf[k])
		power[k] = re*re + im*im
	}
	return power
}
