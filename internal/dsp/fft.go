package dsp

import "math"

// NextPow2 returns the smallest power of two that is >= n (minimum 1).
func NextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// FFT computes the in-place forward Fast Fourier Transform.
// The length of x must be a power of two.
func FFT(x []complex128) { transform(x, false) }

// IFFT computes the in-place inverse Fast Fourier Transform (normalised by 1/N).
// The length of x must be a power of two.
func IFFT(x []complex128) {
	transform(x, true)
	n := complex(float64(len(x)), 0)
	for i := range x {
		x[i] /= n
	}
}

// transform is the iterative radix-2 Cooley-Tukey algorithm shared by FFT/IFFT.
func transform(x []complex128, inverse bool) {
	n := len(x)
	if n <= 1 {
		return
	}
	// A radix-2 FFT silently computes garbage on any other length, so refuse
	// loudly instead — this is a programmer error, not a runtime condition.
	if n&(n-1) != 0 {
		panic("dsp: FFT length must be a power of two")
	}

	// Reorder the input by bit-reversed index.
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}

	// Combine results in butterfly stages of growing size.
	for size := 2; size <= n; size <<= 1 {
		angle := 2 * math.Pi / float64(size)
		if !inverse {
			angle = -angle
		}
		step := complex(math.Cos(angle), math.Sin(angle))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			half := size >> 1
			for k := range half {
				a := x[start+k]
				b := x[start+k+half] * w
				x[start+k] = a + b
				x[start+k+half] = a - b
				w *= step
			}
		}
	}
}
