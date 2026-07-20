package matcher

import (
	"math"
	"math/rand"
	"testing"

	"github.com/bluram/detect-engine/internal/fingerprint"
)

// bruteScoreCurve computes the agreement curve directly (O(n*m*bands)). It is the
// independent reference implementation used to verify the fast overlap-save
// scoreCurve produces identical results.
func bruteScoreCurve(record, reference *fingerprint.Fingerprint) []float64 {
	n := record.NumFrames()
	m := reference.NumFrames()
	bands := record.NumBands()
	total := float64(bands * m)

	curve := make([]float64, n-m+1)
	for t := range curve {
		var raw float64
		for b := 0; b < bands; b++ {
			for j := 0; j < m; j++ {
				rr := float64(2*record.Bits[b][t+j] - 1)
				rq := float64(2*reference.Bits[b][j] - 1)
				raw += rr * rq
			}
		}
		curve[t] = (raw + total) / (2 * total)
	}
	return curve
}

func randomFingerprint(rng *rand.Rand, bands, frames int) *fingerprint.Fingerprint {
	bits := make([][]int8, bands)
	for b := range bits {
		bits[b] = make([]int8, frames)
		for j := range bits[b] {
			bits[b][j] = int8(rng.Intn(2))
		}
	}
	return &fingerprint.Fingerprint{Bits: bits, SampleRate: 22050, HopSamples: 256}
}

func TestScoreCurveMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	cases := []struct{ n, m, bands int }{
		{400, 37, 12},   // typical: many blocks
		{500, 250, 12},  // reference nearly half the record
		{300, 300, 12},  // reference == record (single offset)
		{1000, 13, 6},   // short reference, many blocks
		{128, 1, 4},     // degenerate 1-frame reference
		{2048, 511, 12}, // reference just under a power of two
	}
	for _, c := range cases {
		record := randomFingerprint(rng, c.bands, c.n)
		reference := randomFingerprint(rng, c.bands, c.m)

		got := Prepare(record, c.m).scoreCurve(reference)
		want := bruteScoreCurve(record, reference)

		if len(got) != len(want) {
			t.Fatalf("n=%d m=%d: length %d, want %d", c.n, c.m, len(got), len(want))
		}
		for i := range want {
			if math.Abs(got[i]-want[i]) > 1e-9 {
				t.Fatalf("n=%d m=%d offset %d: got %.12f, want %.12f", c.n, c.m, i, got[i], want[i])
			}
		}
	}
}
