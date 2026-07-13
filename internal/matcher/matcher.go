// Package matcher slides an advert fingerprint across a recording fingerprint
// and reports where the advert aired.
//
// The match score at each offset is the fraction of fingerprint cells that
// agree (1.0 = identical, 0.5 = unrelated), computed efficiently with an FFT
// cross-correlation. A detection must clear three bars: it must stand out above
// the noise floor, clear an absolute minimum score, and agree consistently
// second-by-second.
package matcher

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/bluram/advert-detect/internal/dsp"
	"github.com/bluram/advert-detect/internal/fingerprint"
)

// Params configures detection thresholds.
type Params struct {
	MinScore         float64 // absolute floor on overall agreement (0..1)
	ThresholdStdDevs float64 // a peak must exceed mean + this many std devs
	MinSecondScore   float64 // every 1-second block must agree at least this much
	MinGapSeconds    float64 // minimum spacing between hits; 0 → 80% of advert length
}

// DefaultParams returns conservative thresholds tuned to catch real airings
// (even with a different music bed) while rejecting unrelated audio.
func DefaultParams() Params {
	return Params{
		MinScore:         0.62,
		ThresholdStdDevs: 5,
		MinSecondScore:   0.40,
		MinGapSeconds:    0,
	}
}

// Detection is one detected airing of the advert inside the recording.
type Detection struct {
	FrameOffset int
	TimeSeconds float64 // start of the airing, seconds from recording start
	EndSeconds  float64 // end of the airing (start + advert length)
	Score       float64 // overall agreement at the peak (0..1)
	WorstSecond float64 // weakest 1-second agreement within the match (0..1)
	Confidence  string  // high | medium | review | low
}

// Find returns every detected airing of advert inside record, sorted by time.
func Find(record, advert *fingerprint.Fingerprint, p Params) ([]Detection, error) {
	switch {
	case advert.NumFrames() == 0:
		return nil, errors.New("matcher: advert fingerprint is empty")
	case advert.NumFrames() > record.NumFrames():
		return nil, fmt.Errorf("matcher: advert (%.1fs) is longer than recording (%.1fs)",
			advert.DurationSeconds(), record.DurationSeconds())
	case advert.NumBands() != record.NumBands():
		return nil, errors.New("matcher: advert and recording have different band counts")
	}

	curve := scoreCurve(record, advert)
	mean, std := meanStd(curve)
	height := math.Max(p.MinScore, mean+p.ThresholdStdDevs*std)

	gapSeconds := p.MinGapSeconds
	if gapSeconds <= 0 {
		gapSeconds = math.Max(0.5, advert.DurationSeconds()*0.8)
	}
	minGap := max(1, int(gapSeconds*float64(record.SampleRate)/float64(record.HopSamples)))

	var detections []Detection
	for _, peak := range findPeaks(curve, height, minGap) {
		worst := worstSecondScore(record, advert, peak)
		if worst < p.MinSecondScore {
			continue
		}
		start := record.FrameToSeconds(peak)
		detections = append(detections, Detection{
			FrameOffset: peak,
			TimeSeconds: start,
			EndSeconds:  start + advert.DurationSeconds(),
			Score:       curve[peak],
			WorstSecond: worst,
			Confidence:  confidence(curve[peak], worst),
		})
	}
	return detections, nil
}

// scoreCurve returns the agreement fraction at every start offset of the advert
// inside the recording. Cells are mapped 0/1 → -1/+1 so a cross-correlation
// sums to (#agree − #disagree); rescaling turns it into a fraction in [0, 1].
func scoreCurve(record, advert *fingerprint.Fingerprint) []float64 {
	n := record.NumFrames()
	m := advert.NumFrames()
	bands := record.NumBands()
	length := dsp.NextPow2(n + m)
	totalCells := float64(bands * m)

	acc := make([]complex128, length)
	advBuf := make([]complex128, length)

	for b := 0; b < bands; b++ {
		recBuf := make([]complex128, length)
		for i := 0; i < n; i++ {
			recBuf[i] = complex(float64(2*record.Bits[b][i]-1), 0)
		}
		dsp.FFT(recBuf)

		for i := range advBuf {
			advBuf[i] = 0
		}
		for j := 0; j < m; j++ { // reversed → correlation via convolution
			advBuf[j] = complex(float64(2*advert.Bits[b][m-1-j]-1), 0)
		}
		dsp.FFT(advBuf)

		for k := 0; k < length; k++ {
			acc[k] += recBuf[k] * advBuf[k]
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
func worstSecondScore(record, advert *fingerprint.Fingerprint, offset int) float64 {
	framesPerSecond := max(1, int(math.Round(float64(record.SampleRate)/float64(record.HopSamples))))
	m := advert.NumFrames()
	bands := advert.NumBands()

	worst := 1.0
	for start := 0; start < m; start += framesPerSecond {
		end := min(start+framesPerSecond, m)
		var same, count int
		for b := 0; b < bands; b++ {
			for j := start; j < end; j++ {
				if record.Bits[b][offset+j] == advert.Bits[b][j] {
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
