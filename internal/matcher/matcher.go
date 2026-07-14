// Package matcher slides a query fingerprint across a record fingerprint and
// reports where the query occurs.
//
// The match score at each offset is the fraction of fingerprint cells that
// agree (1.0 = identical, 0.5 = unrelated), computed efficiently with an FFT
// cross-correlation. A match must clear three bars: it must stand out above the
// noise floor, clear an absolute minimum score, and agree consistently
// second-by-second.
//
// To match many queries against one record, Prepare the record once (its
// per-band FFTs are the expensive part) and reuse the PreparedRecord for every
// query.
//
// The package is domain-neutral: it knows nothing about what a query or record
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
	MinGapSeconds    float64 // minimum spacing between matches; 0 → 80% of query length
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

// Detection is one detected occurrence of the query inside the record.
type Detection struct {
	FrameOffset int
	TimeSeconds float64 // start of the match, seconds from the record start
	EndSeconds  float64 // end of the match (start + query length)
	Score       float64 // overall agreement at the peak (0..1)
	WorstSecond float64 // weakest 1-second agreement within the match (0..1)
	Confidence  string  // high | medium | review | low
}

// PreparedRecord is a record fingerprint with its per-band FFTs precomputed,
// ready to be matched against many queries without recomputing the record.
type PreparedRecord struct {
	record         *fingerprint.Fingerprint
	length         int            // FFT length (power of two)
	maxQueryFrames int            // longest query this preparation supports
	recFFT         [][]complex128 // FFT of each ±1 record band, padded to length
}

// Prepare precomputes the record's transforms. maxQueryFrames must be at least
// as large as the longest query that will be matched against it.
func Prepare(record *fingerprint.Fingerprint, maxQueryFrames int) *PreparedRecord {
	n := record.NumFrames()
	bands := record.NumBands()
	length := dsp.NextPow2(n + maxQueryFrames)

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
		record:         record,
		length:         length,
		maxQueryFrames: maxQueryFrames,
		recFFT:         recFFT,
	}
}

// Find returns every detected occurrence of query inside the prepared record,
// sorted by time.
func (pr *PreparedRecord) Find(query *fingerprint.Fingerprint, p Params) ([]Detection, error) {
	record := pr.record
	switch {
	case query.NumFrames() == 0:
		return nil, errors.New("matcher: query fingerprint is empty")
	case query.NumFrames() > record.NumFrames():
		return nil, fmt.Errorf("matcher: query (%.1fs) is longer than record (%.1fs)",
			query.DurationSeconds(), record.DurationSeconds())
	case query.NumFrames() > pr.maxQueryFrames:
		return nil, fmt.Errorf("matcher: query has %d frames but record was prepared for max %d",
			query.NumFrames(), pr.maxQueryFrames)
	case query.NumBands() != record.NumBands():
		return nil, errors.New("matcher: query and record have different band counts")
	}

	curve := pr.scoreCurve(query)
	mean, std := meanStd(curve)
	height := math.Max(p.MinScore, mean+p.ThresholdStdDevs*std)

	gapSeconds := p.MinGapSeconds
	if gapSeconds <= 0 {
		gapSeconds = math.Max(0.5, query.DurationSeconds()*0.8)
	}
	minGap := max(1, int(gapSeconds*float64(record.SampleRate)/float64(record.HopSamples)))

	var detections []Detection
	for _, peak := range findPeaks(curve, height, minGap) {
		worst := worstSecondScore(record, query, peak)
		if worst < p.MinSecondScore {
			continue
		}
		start := record.FrameToSeconds(peak)
		detections = append(detections, Detection{
			FrameOffset: peak,
			TimeSeconds: start,
			EndSeconds:  start + query.DurationSeconds(),
			Score:       curve[peak],
			WorstSecond: worst,
			Confidence:  confidence(curve[peak], worst),
		})
	}
	return detections, nil
}

// Find matches a single query against a single record. It is a convenience
// wrapper around Prepare + Find for the one-off case.
func Find(record, query *fingerprint.Fingerprint, p Params) ([]Detection, error) {
	return Prepare(record, query.NumFrames()).Find(query, p)
}

// scoreCurve returns the agreement fraction at every start offset of the query
// inside the record, reusing the record's precomputed FFTs. Cells are mapped
// 0/1 → -1/+1 so a cross-correlation sums to (#agree − #disagree); rescaling
// turns it into a fraction in [0, 1].
func (pr *PreparedRecord) scoreCurve(query *fingerprint.Fingerprint) []float64 {
	n := pr.record.NumFrames()
	m := query.NumFrames()
	bands := pr.record.NumBands()
	totalCells := float64(bands * m)

	acc := make([]complex128, pr.length)
	queryBuf := make([]complex128, pr.length)

	for b := 0; b < bands; b++ {
		for i := range queryBuf {
			queryBuf[i] = 0
		}
		for j := 0; j < m; j++ { // reversed → correlation via convolution
			queryBuf[j] = complex(float64(2*query.Bits[b][m-1-j]-1), 0)
		}
		dsp.FFT(queryBuf)

		recF := pr.recFFT[b]
		for k := 0; k < pr.length; k++ {
			acc[k] += recF[k] * queryBuf[k]
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
func worstSecondScore(record, query *fingerprint.Fingerprint, offset int) float64 {
	framesPerSecond := max(1, int(math.Round(float64(record.SampleRate)/float64(record.HopSamples))))
	m := query.NumFrames()
	bands := query.NumBands()

	worst := 1.0
	for start := 0; start < m; start += framesPerSecond {
		end := min(start+framesPerSecond, m)
		var same, count int
		for b := 0; b < bands; b++ {
			for j := start; j < end; j++ {
				if record.Bits[b][offset+j] == query.Bits[b][j] {
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
	for i := 1; i < len(curve)-1; i++ {
		if curve[i] >= height && curve[i] >= curve[i-1] && curve[i] >= curve[i+1] {
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
