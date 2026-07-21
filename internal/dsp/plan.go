package dsp

import (
	"math"
	"math/cmplx"
)

// Plan holds precomputed twiddle factors for a fixed power-of-two FFT length.
// Repeated transforms of that length reuse one accurate table instead of
// regenerating the twiddles by successive multiplication, which slowly drifts off
// the unit circle and loses precision on large inputs.
type Plan struct {
	n  int
	tw []complex128 // forward twiddles: tw[j] = e^{-i·2π·j/n}, j = 0 .. n/2-1
}

// NewPlan builds a reusable FFT plan for length n, which must be a power of two.
func NewPlan(n int) *Plan {
	if n < 1 || n&(n-1) != 0 {
		panic("dsp: FFT length must be a power of two")
	}
	tw := make([]complex128, max(1, n/2))
	for j := range tw {
		angle := -2 * math.Pi * float64(j) / float64(n)
		tw[j] = complex(math.Cos(angle), math.Sin(angle))
	}
	return &Plan{n: n, tw: tw}
}

// Len reports the transform length this plan is built for.
func (p *Plan) Len() int { return p.n }

// FFT runs the in-place forward transform. len(x) must equal the plan length.
func (p *Plan) FFT(x []complex128) { p.forward(x) }

// IFFT runs the in-place inverse transform, normalised by 1/n. It reuses the
// forward path via conjugation: IFFT(x) = conj(FFT(conj(x)))/n, so there is only
// one twiddle table and no per-butterfly branch.
func (p *Plan) IFFT(x []complex128) {
	for i := range x {
		x[i] = cmplx.Conj(x[i])
	}
	p.forward(x)
	n := complex(float64(p.n), 0)
	for i := range x {
		x[i] = cmplx.Conj(x[i]) / n
	}
}

// forward is the iterative radix-2 Cooley-Tukey FFT using the precomputed table.
func (p *Plan) forward(x []complex128) {
	n := p.n
	if n <= 1 {
		return
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
	// Butterfly stages. The twiddle for butterfly k of a size-`size` stage is
	// e^{-i·2π·k/size} = tw[k·(n/size)], so we index the table with a stride.
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		stride := n / size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				w := p.tw[k*stride]
				a := x[start+k]
				b := x[start+k+half] * w
				x[start+k] = a + b
				x[start+k+half] = a - b
			}
		}
	}
}
