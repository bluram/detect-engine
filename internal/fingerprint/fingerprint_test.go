package fingerprint

import (
	"math"
	"testing"
)

func TestFromSamplesShapeAndDeterminism(t *testing.T) {
	samples := make([]float64, 22050) // 1 second
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * 440 * float64(i) / 22050)
	}

	a := FromSamples(samples, DefaultConfig())
	b := FromSamples(samples, DefaultConfig())

	if got := a.NumBands(); got != 12 { // NumCoeffs (13) minus dropped coeff 0
		t.Fatalf("expected 12 bands, got %d", got)
	}
	if a.NumFrames() == 0 {
		t.Fatal("expected non-empty fingerprint")
	}

	for band := range a.Bits {
		for f := range a.Bits[band] {
			if a.Bits[band][f] != b.Bits[band][f] {
				t.Fatalf("fingerprint is not deterministic at band %d frame %d", band, f)
			}
		}
	}
}
