package matcher

import (
	"testing"

	"github.com/bluram/advert-detect/internal/fingerprint"
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

func TestFindEmbeddedAdvert(t *testing.T) {
	const bands, recFrames, advFrames, offset = 12, 500, 80, 150

	record := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	record.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		record.Bits[b] = make([]int8, recFrames)
		for f := 0; f < recFrames; f++ {
			record.Bits[b][f] = bitAt(b, f)
		}
	}

	// advert is an exact slice of the record at a known offset.
	advert := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	advert.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		advert.Bits[b] = append([]int8(nil), record.Bits[b][offset:offset+advFrames]...)
	}

	detections, err := Find(record, advert, DefaultParams())
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

func TestPreparedRecordMatchesMultipleAdverts(t *testing.T) {
	const bands, recFrames = 12, 600

	record := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256}
	record.Bits = make([][]int8, bands)
	for b := 0; b < bands; b++ {
		record.Bits[b] = make([]int8, recFrames)
		for f := 0; f < recFrames; f++ {
			record.Bits[b][f] = bitAt(b, f)
		}
	}

	// two adverts of different lengths, embedded at different offsets.
	sliceAt := func(offset, length int) *fingerprint.Fingerprint {
		fp := &fingerprint.Fingerprint{SampleRate: 22050, HopSamples: 256, Bits: make([][]int8, bands)}
		for b := 0; b < bands; b++ {
			fp.Bits[b] = append([]int8(nil), record.Bits[b][offset:offset+length]...)
		}
		return fp
	}
	advertA := sliceAt(100, 80)
	advertB := sliceAt(400, 50)

	prepared := Prepare(record, 80) // 80 = longest advert

	for name, tc := range map[string]struct {
		advert *fingerprint.Fingerprint
		want   int
	}{
		"advertA": {advertA, 100},
		"advertB": {advertB, 400},
	} {
		got, err := prepared.Find(tc.advert, DefaultParams())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 1 || got[0].FrameOffset != tc.want {
			t.Fatalf("%s: expected 1 detection at %d, got %+v", name, tc.want, got)
		}
	}
}

func TestFindRejectsLongerAdvert(t *testing.T) {
	record := &fingerprint.Fingerprint{Bits: make([][]int8, 12), SampleRate: 22050, HopSamples: 256}
	advert := &fingerprint.Fingerprint{Bits: make([][]int8, 12), SampleRate: 22050, HopSamples: 256}
	for b := 0; b < 12; b++ {
		record.Bits[b] = make([]int8, 10)
		advert.Bits[b] = make([]int8, 20)
	}
	if _, err := Find(record, advert, DefaultParams()); err == nil {
		t.Fatal("expected an error when advert is longer than recording")
	}
}
