package dsp

import (
	"math"
	"math/cmplx"
)

// Real-input FFT.
//
// The spectrum of a real sequence is conjugate-symmetric (X[n-k] = conj(X[k])),
// so half of a full complex FFT's output is redundant — and so is half of its
// work. RFFT exploits this by transforming a length-n real sequence with a
// half-length (n/2) complex FFT, then unpacking the n/2+1 non-redundant
// coefficients. It costs roughly half of a complex FFT on the same data.

// RFFT returns the n/2+1 non-redundant DFT coefficients of a real sequence whose
// length n is an even power of two.
func RFFT(x []float64) []complex128 {
	dst := make([]complex128, len(x)/2+1)
	scratch := make([]complex128, len(x)/2)
	RFFTInto(dst, scratch, x)
	return dst
}

// RFFTInto is RFFT without allocation: it writes the n/2+1 coefficients into dst
// (len n/2+1) using scratch (len n/2). It runs a half-length complex FFT, so
// len(x) must be an even power of two.
func RFFTInto(dst, scratch []complex128, x []float64) {
	n := len(x)
	m := n / 2

	for j := 0; j < m; j++ { // even samples → real, odd samples → imaginary
		scratch[j] = complex(x[2*j], x[2*j+1])
	}
	FFT(scratch)

	wStep := complex(math.Cos(2*math.Pi/float64(n)), -math.Sin(2*math.Pi/float64(n)))
	w := complex(1, 0)
	for k := 0; k <= m; k++ {
		zk := scratch[k%m]
		zmk := cmplx.Conj(scratch[(m-k)%m])
		even := (zk + zmk) * 0.5
		odd := (zk - zmk) * complex(0, -0.5) // divide by 2i
		dst[k] = even + w*odd
		w *= wStep
	}
}

// IRFFT reconstructs the n real samples from the n/2+1 coefficients produced by
// RFFT. n must be an even power of two.
func IRFFT(coeff []complex128, n int) []float64 {
	dst := make([]float64, n)
	scratch := make([]complex128, n/2)
	IRFFTInto(dst, scratch, coeff, n)
	return dst
}

// IRFFTInto is IRFFT without allocation: it writes the n real samples into dst
// (len n) using scratch (len n/2). coeff must have at least n/2+1 entries.
func IRFFTInto(dst []float64, scratch []complex128, coeff []complex128, n int) {
	m := n / 2

	wStep := complex(math.Cos(2*math.Pi/float64(n)), math.Sin(2*math.Pi/float64(n))) // conj twiddle
	w := complex(1, 0)
	for k := 0; k < m; k++ {
		xk := coeff[k]
		xmk := cmplx.Conj(coeff[m-k]) // for k==0 this reads coeff[m]
		even := (xk + xmk) * 0.5
		odd := (xk - xmk) * 0.5 * w // multiply by conj(W_n^k)
		scratch[k] = even + complex(0, 1)*odd
		w *= wStep
	}
	IFFT(scratch)

	for j := 0; j < m; j++ {
		dst[2*j] = real(scratch[j])
		dst[2*j+1] = imag(scratch[j])
	}
}
