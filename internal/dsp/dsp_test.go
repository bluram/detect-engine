package dsp

import (
	"math"
	"testing"
)

func TestFFTInverseRoundTrip(t *testing.T) {
	orig := []complex128{1, 2, 3, 4, 5, 6, 7, 8}
	x := append([]complex128(nil), orig...)

	FFT(x)
	IFFT(x)

	for i := range orig {
		if math.Abs(real(x[i])-real(orig[i])) > 1e-9 || math.Abs(imag(x[i])) > 1e-9 {
			t.Fatalf("bin %d: got %v, want %v", i, x[i], orig[i])
		}
	}
}

func TestPowerSpectrumFindsTone(t *testing.T) {
	const n, bin = 512, 20

	frame := make([]float64, n)
	for i := range frame {
		frame[i] = math.Cos(2 * math.Pi * float64(bin) * float64(i) / float64(n))
	}

	power := PowerSpectrum(frame)
	peak := 0
	for k := range power {
		if power[k] > power[peak] {
			peak = k
		}
	}
	if peak != bin {
		t.Fatalf("expected peak at bin %d, got %d", bin, peak)
	}
}

func TestFramesOverlap(t *testing.T) {
	signal := make([]float64, 1000)
	frames := Frames(signal, 512, 256)
	if len(frames) != 2 { // starts at 0 and 256; 512 would overshoot 1000
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if len(frames[0]) != 512 {
		t.Fatalf("expected frame length 512, got %d", len(frames[0]))
	}
}
