package dsp

// NextPow2 returns the smallest power of two that is >= n (minimum 1).
func NextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// FFT computes the in-place forward Fast Fourier Transform. len(x) must be a power
// of two. It builds a throwaway Plan; to transform many sequences of the same
// length, reuse a *Plan from NewPlan instead.
func FFT(x []complex128) { NewPlan(len(x)).FFT(x) }

// IFFT computes the in-place inverse Fast Fourier Transform (normalised by 1/N).
func IFFT(x []complex128) { NewPlan(len(x)).IFFT(x) }
