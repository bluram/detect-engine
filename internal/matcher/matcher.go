// Package matcher slides a reference fingerprint across a record fingerprint and
// reports where the reference occurs.
//
// The match score at each offset is the fraction of fingerprint cells that
// agree (1.0 = identical, 0.5 = unrelated), computed efficiently with an FFT
// cross-correlation. A match must clear three bars: it must stand out above the
// noise floor, clear an absolute minimum score, and agree consistently
// second-by-second.
//
// To match many references against one record, Prepare the record once (its
// per-band FFTs are the expensive part) and reuse the PreparedRecord for every
// reference.
//
// The package is domain-neutral: it knows nothing about what a reference or record
// contains (a jingle, a movie, a spoken phrase, …) — only that it looks for one
// fingerprint inside another.
package matcher

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/bluram/detect-engine/internal/dsp"
	"github.com/bluram/detect-engine/internal/fingerprint"
)

// Params configures detection thresholds.
type Params struct {
	MinScore         float64 // absolute floor on overall agreement (0..1)
	ThresholdStdDevs float64 // a peak must exceed mean + this many std devs
	MinSecondScore   float64 // every 1-second block must agree at least this much
	MinGapSeconds    float64 // minimum spacing between matches; 0 → 80% of reference length
}

// DefaultParams returns conservative thresholds tuned to catch real matches
// (even with some variation) while rejecting unrelated audio.
func DefaultParams() Params {
	return Params{
		MinScore:         0.62,
		ThresholdStdDevs: 5,
		MinSecondScore:   0.40,
		MinGapSeconds:    0,
	}
}

// Detection is one detected occurrence of the reference inside the record.
type Detection struct {
	FrameOffset int
	TimeSeconds float64 // start of the match, seconds from the record start
	EndSeconds  float64 // end of the match (start + reference length)
	Score       float64 // overall agreement at the peak (0..1)
	WorstSecond float64 // weakest 1-second agreement within the match (0..1)
	Confidence  string  // high | medium | review | low
}

// PreparedRecord is a record fingerprint with its per-band FFTs precomputed,
// ready to be matched against many references without recomputing the record.
type PreparedRecord struct {
	record             *fingerprint.Fingerprint
	length             int            // FFT length (power of two)
	maxReferenceFrames int            // longest reference this preparation supports
	recFFT             [][]complex128 // FFT of each ±1 record band, padded to length
}

// Prepare precomputes the record's transforms. maxReferenceFrames must be at least
// as large as the longest reference that will be matched against it.
func Prepare(record *fingerprint.Fingerprint, maxReferenceFrames int) *PreparedRecord {
	n := record.NumFrames()
	bands := record.NumBands()
	length := dsp.NextPow2(n + maxReferenceFrames)

	recFFT := make([][]complex128, bands)
	for b := 0; b < bands; b++ {
		buf := make([]complex128, length)
		for i := 0; i < n; i++ {
			buf[i] = complex(float64(2*record.Bits[b][i]-1), 0)
		}
		dsp.FFT(buf)
		recFFT[b] = buf
	}

	return &PreparedRecord{
		record:             record,
		length:             length,
		maxReferenceFrames: maxReferenceFrames,
		recFFT:             recFFT,
	}
}

// Find returns every detected occurrence of reference inside the prepared record,
// sorted by time.
func (pr *PreparedRecord) Find(reference *fingerprint.Fingerprint, p Params) ([]Detection, error) {
	record := pr.record
	switch {
	case reference.NumFrames() == 0:
		return nil, errors.New("matcher: reference fingerprint is empty")
	case reference.NumFrames() > record.NumFrames():
		return nil, fmt.Errorf("matcher: reference (%.1fs) is longer than record (%.1fs)",
			reference.DurationSeconds(), record.DurationSeconds())
	case reference.NumFrames() > pr.maxReferenceFrames:
		return nil, fmt.Errorf("matcher: reference has %d frames but record was prepared for max %d",
			reference.NumFrames(), pr.maxReferenceFrames)
	case reference.NumBands() != record.NumBands():
		return nil, errors.New("matcher: reference and record have different band counts")
	}

	curve := pr.scoreCurve(reference)
	mean, std := meanStd(curve)
	height := math.Max(p.MinScore, mean+p.ThresholdStdDevs*std)

	gapSeconds := p.MinGapSeconds
	if gapSeconds <= 0 {
		gapSeconds = math.Max(0.5, reference.DurationSeconds()*0.8)
	}
	minGap := max(1, int(gapSeconds*float64(record.SampleRate)/float64(record.HopSamples)))

	var detections []Detection
	for _, peak := range findPeaks(curve, height, minGap) {
		worst := worstSecondScore(record, reference, peak)
		if worst < p.MinSecondScore {
			continue
		}
		start := record.FrameToSeconds(peak)
		detections = append(detections, Detection{
			FrameOffset: peak,
			TimeSeconds: start,
			EndSeconds:  start + reference.DurationSeconds(),
			Score:       curve[peak],
			WorstSecond: worst,
			Confidence:  confidence(curve[peak], worst),
		})
	}
	return detections, nil
}

// Find matches a single reference against a single record. It is a convenience
// wrapper around Prepare + Find for the one-off case.
func Find(record, reference *fingerprint.Fingerprint, p Params) ([]Detection, error) {
	return Prepare(record, reference.NumFrames()).Find(reference, p)
}

// scoreCurve returns the agreement fraction at every start offset of the reference
// inside the record, reusing the record's precomputed FFTs. Cells are mapped
// 0/1 → -1/+1 so a cross-correlation sums to (#agree − #disagree); rescaling
// turns it into a fraction in [0, 1].
func (pr *PreparedRecord) scoreCurve(reference *fingerprint.Fingerprint) []float64 {
	n := pr.record.NumFrames()
	m := reference.NumFrames()
	bands := pr.record.NumBands()
	totalCells := float64(bands * m)

	acc := make([]complex128, pr.length)
	referenceBuf := make([]complex128, pr.length)

	for b := 0; b < bands; b++ {
		for i := range referenceBuf {
			referenceBuf[i] = 0
		}
		for j := 0; j < m; j++ { // reversed → correlation via convolution
			referenceBuf[j] = complex(float64(2*reference.Bits[b][m-1-j]-1), 0)
		}
		dsp.FFT(referenceBuf)

		recF := pr.recFFT[b]
		for k := 0; k < pr.length; k++ {
			acc[k] += recF[k] * referenceBuf[k]
		}
	}
	dsp.IFFT(acc)

	curve := make([]float64, n-m+1)
	for t := range curve {
		raw := real(acc[t+m-1])
		curve[t] = (raw + totalCells) / (2 * totalCells)
	}
	return curve
}

// worstSecondScore returns the weakest one-second block agreement at an offset,
// rejecting matches that line up on average but drift somewhere inside.
func worstSecondScore(record, reference *fingerprint.Fingerprint, offset int) float64 {
	framesPerSecond := max(1, int(math.Round(float64(record.SampleRate)/float64(record.HopSamples))))
	m := reference.NumFrames()
	bands := reference.NumBands()

	worst := 1.0
	for start := 0; start < m; start += framesPerSecond {
		end := min(start+framesPerSecond, m)
		var same, count int
		for b := 0; b < bands; b++ {
			for j := start; j < end; j++ {
				if record.Bits[b][offset+j] == reference.Bits[b][j] {
					same++
				}
				count++
			}
		}
		if count == 0 {
			break
		}
		if s := float64(same) / float64(count); s < worst {
			worst = s
		}
	}
	return worst
}

// findPeaks returns local maxima at or above height, greedily thinned so no two
// are closer than minGap (higher score wins), then sorted by position.
func findPeaks(curve []float64, height float64, minGap int) []int {
	var candidates []int
	for i := 0; i < len(curve); i++ {
		if curve[i] < height {
			continue
		}
		// Boundary positions have no neighbour on that side, so they only need
		// to beat the neighbour they do have — a match at the very start or
		// very end of the record must still be detectable.
		leftOK := i == 0 || curve[i] >= curve[i-1]
		rightOK := i == len(curve)-1 || curve[i] >= curve[i+1]
		if leftOK && rightOK {
			candidates = append(candidates, i)
		}
	}
	sort.Slice(candidates, func(a, b int) bool { return curve[candidates[a]] > curve[candidates[b]] })

	var kept []int
	for _, idx := range candidates {
		clash := false
		for _, k := range kept {
			if abs(idx-k) < minGap {
				clash = true
				break
			}
		}
		if !clash {
			kept = append(kept, idx)
		}
	}
	sort.Ints(kept)
	return kept
}

func meanStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))

	var variance float64
	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	return mean, math.Sqrt(variance / float64(len(xs)))
}

func confidence(score, worst float64) string {
	switch {
	case score >= 0.85 && worst >= 0.65:
		return "high"
	case score >= 0.72:
		return "medium"
	case score >= 0.62:
		return "review"
	default:
		return "low"
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
