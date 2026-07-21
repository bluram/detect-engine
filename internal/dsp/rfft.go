package dsp

import (
	"math"
	"math/cmplx"
)

// Real-input FFT.
//
// The spectrum of a real sequence is conjugate-symmetric (X[n-k] = conj(X[k])),
// so half of a full complex FFT's output is redundant — and so is half of its
// work. A RealPlan transforms a length-n real sequence with a half-length (n/2)
// complex Plan, then unpacks the n/2+1 non-redundant coefficients. It costs
// roughly half of a complex FFT on the same data.

// RealPlan transforms real sequences of a fixed even power-of-two length using a
// half-length complex Plan plus precomputed unpacking twiddles.
type RealPlan struct {
	n  int
	c  *Plan
	tw []complex128 // unpack twiddles: tw[k] = e^{-i·2π·k/n}, k = 0 .. n/2
}

// NewRealPlan builds a reusable real-FFT plan for length n, an even power of two.
func NewRealPlan(n int) *RealPlan {
	if n < 2 || n&(n-1) != 0 {
		panic("dsp: real FFT length must be an even power of two")
	}
	m := n / 2
	tw := make([]complex128, m+1)
	for k := range tw {
		angle := -2 * math.Pi * float64(k) / float64(n)
		tw[k] = complex(math.Cos(angle), math.Sin(angle))
	}
	return &RealPlan{n: n, c: NewPlan(m), tw: tw}
}

// Len reports the real transform length this plan is built for.
func (p *RealPlan) Len() int { return p.n }

// Coefficients writes the n/2+1 DFT coefficients of the real sequence x into dst
// (len n/2+1), using scratch (len n/2) for the internal complex FFT.
func (p *RealPlan) Coefficients(dst, scratch []complex128, x []float64) {
	m := p.n / 2
	for j := 0; j < m; j++ { // even samples → real, odd samples → imaginary
		scratch[j] = complex(x[2*j], x[2*j+1])
	}
	p.c.FFT(scratch)
	for k := 0; k <= m; k++ {
		zk := scratch[k%m]
		zmk := cmplx.Conj(scratch[(m-k)%m])
		even := (zk + zmk) * 0.5
		odd := (zk - zmk) * complex(0, -0.5) // divide by 2i
		dst[k] = even + p.tw[k]*odd
	}
}

// Sequence reconstructs the n real samples from coeff (len ≥ n/2+1) into dst
// (len n), using scratch (len n/2).
func (p *RealPlan) Sequence(dst []float64, scratch, coeff []complex128) {
	m := p.n / 2
	for k := 0; k < m; k++ {
		xk := coeff[k]
		xmk := cmplx.Conj(coeff[m-k]) // for k==0 this reads coeff[m]
		even := (xk + xmk) * 0.5
		odd := (xk - xmk) * 0.5 * cmplx.Conj(p.tw[k]) // multiply by conj(W_n^k)
		scratch[k] = even + complex(0, 1)*odd
	}
	p.c.IFFT(scratch)
	for j := 0; j < m; j++ {
		dst[2*j] = real(scratch[j])
		dst[2*j+1] = imag(scratch[j])
	}
}

// RFFT returns the n/2+1 non-redundant DFT coefficients of a real sequence whose
// length is an even power of two. It builds a throwaway RealPlan; reuse a
// *RealPlan from NewRealPlan for repeated transforms of the same length.
func RFFT(x []float64) []complex128 {
	dst := make([]complex128, len(x)/2+1)
	scratch := make([]complex128, len(x)/2)
	NewRealPlan(len(x)).Coefficients(dst, scratch, x)
	return dst
}

// IRFFT reconstructs the n real samples from the n/2+1 coefficients produced by
// RFFT. n must be an even power of two.
func IRFFT(coeff []complex128, n int) []float64 {
	dst := make([]float64, n)
	scratch := make([]complex128, n/2)
	NewRealPlan(n).Sequence(dst, scratch, coeff)
	return dst
}
