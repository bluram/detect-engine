package matcher

import (
	"testing"

	"github.com/bluram/detect-engine/internal/fingerprint"
)

// deterministic but well-mixed pattern (hash-based) so the baseline correlation
// is ~0.5 and a true match clearly stands out. Stable across runs.
func bitAt(band, frame int) int8 {
	h := uint32(band)*2654435761 + uint32(frame)*40503
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return int8(h & 1)
}

func TestFindEmbeddedReference(t *testing.T) {
	const bands, recFrames, referenceFrames, offset = 12, 500, 80, 150

	record := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	record.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		record.Bits[b] = make([]int8, recFrames)
		for f := 0; f < recFrames; f++ {
			record.Bits[b][f] = bitAt(b, f)
		}
	}

	// reference is an exact slice of the record at a known offset.
	reference := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	reference.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		reference.Bits[b] = append([]int8(nil), record.Bits[b][offset:offset+referenceFrames]...)
	}

	detections, err := Find(record, reference, DefaultParams())
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if len(detections) != 1 {
		t.Fatalf("expected exactly 1 detection, got %d", len(detections))
	}
	if detections[0].FrameOffset != offset {
		t.Fatalf("expected detection at frame %d, got %d", offset, detections[0].FrameOffset)
	}
	if detections[0].Score < 0.99 {
		t.Fatalf("expected near-perfect score, got %.3f", detections[0].Score)
	}
}

func TestPreparedRecordMatchesMultipleReferences(t *testing.T) {
	const bands, recFrames = 12, 600

	record := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	record.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		record.Bits[b] = make([]int8, recFrames)
		for f := 0; f < recFrames; f++ {
			record.Bits[b][f] = bitAt(b, f)
		}
	}

	// two references of different lengths, embedded at different offsets.
	sliceAt := func(offset, length int) *fingerprint.Fingerprint {
		fp := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256, Bits: make([][]int8, bands)}
		for b := 0; b < bands; b++ {
			fp.Bits[b] = append([]int8(nil), record.Bits[b][offset:offset+length]...)
		}
		return fp
	}
	referenceA := sliceAt(100, 80)
	referenceB := sliceAt(400, 50)

	prepared := Prepare(record, 80) // 80 = longest reference

	for name, tc := range map[string]struct {
		reference *fingerprint.Fingerprint
		want      int
	}{
		"referenceA": {referenceA, 100},
		"referenceB": {referenceB, 400},
	} {
		got, err := prepared.Find(tc.reference, DefaultParams())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 || got[0].FrameOffset != tc.want {
			t.Fatalf("%s: expected 1 detection at %d, got %+v", name, tc.want, got)
		}
	}
}

func TestFindRejectsLongerReference(t *testing.T) {
	record := &fingerprint.Fingerprint{Bits: make([][]int8, 12), SampleRate: 22050, HopSamples: 256}
	reference := &fingerprint.Fingerprint{Bits: make([][]int8, 12), SampleRate: 22050, HopSamples: 256}
	for b := 0; b < 12; b++ {
		record.Bits[b] = make([]int8, 10)
		reference.Bits[b] = make([]int8, 20)
	}
	if _, err := Find(record, reference, DefaultParams()); err == nil {
		t.Fatal("expected an error when reference is longer than record")
	}
}

// Regression test: peaks at the very first and very last curve positions must
// be detectable (a record can start or end exactly on a match).
func TestFindReferenceAtRecordBoundaries(t *testing.T) {
	const bands, recFrames, referenceFrames = 12, 500, 80

	record := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	record.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		record.Bits[b] = make([]int8, recFrames)
		for f := 0; f < recFrames; f++ {
			record.Bits[b][f] = bitAt(b, f)
		}
	}

	sliceAt := func(offset int) *fingerprint.Fingerprint {
		fp := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256, Bits: make([][]int8, bands)}
		for b := 0; b < bands; b++ {
			fp.Bits[b] = append([]int8(nil), record.Bits[b][offset:offset+referenceFrames]...)
		}
		return fp
	}

	for name, offset := range map[string]int{
		"at record start": 0,
		"at record end":   recFrames - referenceFrames,
	} {
		got, err := Find(record, sliceAt(offset), DefaultParams())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 || got[0].FrameOffset != offset {
			t.Fatalf("%s: expected 1 detection at frame %d, got %+v", name, offset, got)
		}
	}
}
