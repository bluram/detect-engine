package fingerprint

import (
	"bytes"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	orig := &Fingerprint{SampleRate: 22050, HopSamples: 256}
	orig.Bits = make([][]int8, 12)
	for b := range orig.Bits {
		row := make([]int8, 37) // not a multiple of 8, to exercise the partial byte
		for f := range row {
			if (b*7+f*13)%3 == 0 {
				row[f] = 1
			}
		}
		orig.Bits[b] = row
	}

	var buf bytes.Buffer
	if err := Save(orig, &buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.SampleRate != orig.SampleRate || got.HopSamples != orig.HopSamples {
		t.Fatalf("metadata mismatch: got %+v", got)
	}
	if got.NumBands() != orig.NumBands() || got.NumFrames() != orig.NumFrames() {
		t.Fatalf("shape mismatch: got %dx%d", got.NumBands(), got.NumFrames())
	}
	for b := range orig.Bits {
		for f := range orig.Bits[b] {
			if got.Bits[b][f] != orig.Bits[b][f] {
				t.Fatalf("bit mismatch at [%d][%d]", b, f)
			}
		}
	}
}
