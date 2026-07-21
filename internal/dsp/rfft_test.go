package dsp

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
)

// RFFT must agree with a full complex FFT on the same real data, for the
// non-redundant half of the spectrum.
func TestRFFTMatchesFFT(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, n := range []int{2, 4, 8, 16, 64, 256, 1024, 4096} {
		x := make([]float64, n)
		cx := make([]complex128, n)
		for i := range x {
			x[i] = rng.NormFloat64()
			cx[i] = complex(x[i], 0)
		}
		FFT(cx)
		r := RFFT(x)
		if len(r) != n/2+1 {
			t.Fatalf("n=%d: got %d coeffs, want %d", n, len(r), n/2+1)
		}
		for k := 0; k <= n/2; k++ {
			if cmplx.Abs(r[k]-cx[k]) > 1e-9 {
				t.Fatalf("n=%d k=%d: RFFT=%v, FFT=%v", n, k, r[k], cx[k])
			}
		}
	}
}

// IRFFT(RFFT(x)) must return x (checks the inverse and its normalisation).
func TestIRFFTRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for _, n := range []int{2, 4, 8, 16, 64, 256, 1024, 4096} {
		x := make([]float64, n)
		for i := range x {
			x[i] = rng.NormFloat64()
		}
		got := IRFFT(RFFT(x), n)
		for i := range x {
			if math.Abs(got[i]-x[i]) > 1e-9 {
				t.Fatalf("n=%d i=%d: got %v, want %v", n, i, got[i], x[i])
			}
		}
	}
}
