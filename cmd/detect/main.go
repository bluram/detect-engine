// Command detect searches one recording for known adverts and reports where they
// aired. Its -mode says what kind of content it is:
//
//   - -mode audio : match the sound only (radio) → one audio score.
//   - -mode video : match picture AND sound (TV) → both scores + a verdict,
//     because a video has both tracks.
//
// No score is hidden. When only one modality clears the detection filters, the
// other side's actual score at that time is looked up and shown (in
// parentheses) so a variant — same sound, changed picture, or vice versa — is
// visible instead of a blank.
//
// It finds fingerprints by name convention. Given -record rec.mp4 it loads
// rec.mp4.audio.bin (and rec.mp4.video.bin in video mode); in -references it
// pairs each advert's .audio.bin ↔ .video.bin by base name.
//
//	detect -mode audio -record rec.mp3 -references references/audio
//	detect -mode video -record rec.mp4 -references references/video
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bluram/detect-engine/internal/fingerprint"
	"github.com/bluram/detect-engine/internal/matcher"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	mode := fs.String("mode", "", `"audio" (sound only) or "video" (picture + sound)`)
	record := fs.String("record", "", "recording media path or fingerprint base")
	references := fs.String("references", "", "folder of reference fingerprints")
	asJSON := fs.Bool("json", false, "emit results as JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *mode != "audio" && *mode != "video" {
		fmt.Fprintln(os.Stderr, `error: -mode must be "audio" or "video"`)
		return 2
	}
	if *record == "" || *references == "" {
		fmt.Fprintln(os.Stderr, "error: -record and -references are required")
		return 2
	}

	base := binBase(*record)

	// The audio side runs in both modes (video also checks sound); the video
	// side runs only in video mode.
	audioSide, err := loadSide(base+".audio.bin", *references, "audio")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	var videoSide *side
	if *mode == "video" {
		videoSide, err = loadSide(base+".video.bin", *references, "video")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	}

	rows := combine(audioSide, videoSide)
	if *asJSON {
		printJSON(rows, *mode, *record)
	} else {
		printTable(rows)
	}
	if len(rows) == 0 {
		return 1
	}
	return 0
}

// side holds one modality's prepared record plus its references and detections,
// so a missing score can be looked up later (ScoreAt).
type side struct {
	prepared *matcher.PreparedRecord
	record   *fingerprint.Fingerprint
	refs     map[string]*fingerprint.Fingerprint // advertKey → reference
	dets     map[string][]matcher.Detection      // advertKey → detections
}

// loadSide loads the record fingerprint for one modality and matches it against
// the references of that kind. A missing record fingerprint is a warning, not an
// error (returns nil).
func loadSide(recordBin, refsDir, kind string) (*side, error) {
	if _, err := os.Stat(recordBin); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no %s fingerprint (%s) — skipping %s\n", kind, filepath.Base(recordBin), kind)
		return nil, nil
	}
	record, err := fingerprint.LoadFile(recordBin)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", recordBin, err)
	}

	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return nil, fmt.Errorf("reading references %s: %w", refsDir, err)
	}

	refs := map[string]*fingerprint.Fingerprint{}
	maxFrames := 0
	for _, e := range entries {
		if e.IsDir() || classify(e.Name()) != kind {
			continue
		}
		fp, err := fingerprint.LoadFile(filepath.Join(refsDir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", e.Name(), err)
			continue
		}
		if fp.NumFrames() > maxFrames {
			maxFrames = fp.NumFrames()
		}
		refs[advertKey(e.Name())] = fp
	}

	prepared := matcher.Prepare(record, maxFrames)
	params := matcher.DefaultParams()
	dets := map[string][]matcher.Detection{}
	for key, fp := range refs {
		found, err := prepared.Find(fp, params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", key, err)
			continue
		}
		if len(found) > 0 {
			dets[key] = found
		}
	}
	return &side{prepared: prepared, record: record, refs: refs, dets: dets}, nil
}

// cell is one modality's result for one air-time. real is true when it was an
// actual detection (cleared the filters); false when it was looked up to fill a
// blank (sub-threshold, shown in parentheses).
type cell struct {
	score float64
	worst float64
	conf  string // matcher confidence; "" for a looked-up (sub-threshold) cell
	real  bool
}

func detCell(d matcher.Detection) *cell {
	return &cell{score: d.Score, worst: d.WorstSecond, conf: d.Confidence, real: true}
}

// row is one air-time of one advert, with a cell per modality.
type row struct {
	advert string
	start  float64
	audio  *cell
	video  *cell
}

// combine joins audio and video detections of the same advert by overlapping
// air-time, then fills any blank side by looking up its real score at that time.
func combine(audioSide, videoSide *side) []row {
	var audioDets, videoDets map[string][]matcher.Detection
	if audioSide != nil {
		audioDets = audioSide.dets
	}
	if videoSide != nil {
		videoDets = videoSide.dets
	}

	keys := map[string]bool{}
	for k := range audioDets {
		keys[k] = true
	}
	for k := range videoDets {
		keys[k] = true
	}

	var rows []row
	for key := range keys {
		aDets := audioDets[key]
		vDets := videoDets[key]
		usedV := make([]bool, len(vDets))

		for i := range aDets {
			a := aDets[i]
			paired := -1
			for j := range vDets {
				if !usedV[j] && overlaps(a, vDets[j]) {
					paired = j
					break
				}
			}
			if paired >= 0 {
				usedV[paired] = true
				rows = append(rows, row{advert: key, start: a.TimeSeconds, audio: detCell(a), video: detCell(vDets[paired])})
			} else {
				rows = append(rows, row{advert: key, start: a.TimeSeconds, audio: detCell(a)})
			}
		}
		for j := range vDets {
			if !usedV[j] {
				rows = append(rows, row{advert: key, start: vDets[j].TimeSeconds, video: detCell(vDets[j])})
			}
		}
	}

	// Fill blanks with the real (sub-threshold) score at that time — no hiding.
	for i := range rows {
		r := &rows[i]
		if r.audio == nil {
			r.audio = lookup(audioSide, r.advert, r.start)
		}
		if r.video == nil {
			r.video = lookup(videoSide, r.advert, r.start)
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].start < rows[j].start })
	return rows
}

// lookup returns the score of one side's reference at a time, or nil when that
// side has no reference for the advert (a genuine blank).
func lookup(s *side, advert string, timeSec float64) *cell {
	if s == nil {
		return nil
	}
	ref, ok := s.refs[advert]
	if !ok {
		return nil
	}
	// Search ~1.5s around the time: the two modalities' start times differ by a
	// fraction of a second, and audio scoring is very alignment-sensitive.
	center := s.record.SecondsToFrame(timeSec)
	radius := s.record.SecondsToFrame(1.5)
	score, worst := s.prepared.BestScoreNear(ref, center, radius)
	return &cell{score: score, worst: worst, real: false}
}

// overlaps reports whether two detections cover overlapping air-time.
func overlaps(a, b matcher.Detection) bool {
	return a.TimeSeconds < b.EndSeconds && b.TimeSeconds < a.EndSeconds
}

func printTable(rows []row) {
	fmt.Printf("\n  %-26s %-9s  %-8s  %-8s  %s\n", "Advert", "Time", "Audio", "Video", "Verdict")
	fmt.Printf("  %s\n", strings.Repeat("─", 74))
	if len(rows) == 0 {
		fmt.Println("  (no detections)")
		fmt.Println()
		return
	}
	for _, r := range rows {
		fmt.Printf("  %-26s %-9s  %-8s  %-8s  %s\n",
			displayName(r.advert), clock(r.start),
			scoreCell(r.audio), scoreCell(r.video), verdict(r.audio, r.video))
	}
	fmt.Println()
}

func displayName(key string) string {
	name := strings.TrimSuffix(key, filepath.Ext(key))
	if len(name) > 26 {
		name = name[:25] + "…"
	}
	return name
}

// scoreCell shows the score plainly; a genuine blank (no reference for that
// side) is a dash.
func scoreCell(c *cell) string {
	if c == nil {
		return "   –"
	}
	return fmt.Sprintf("%.3f", c.score)
}

// verdict is a single word from the sides that actually detected: confident |
// review | low. Looked-up (sub-threshold) cells do not count as confirmations.
func verdict(a, v *cell) string {
	word := func(conf string) string {
		switch conf {
		case "high":
			return "confident"
		case "low":
			return "low"
		default: // medium, review
			return "review"
		}
	}
	aReal := a != nil && a.real
	vReal := v != nil && v.real
	switch {
	case aReal && vReal:
		switch {
		case a.conf == "high" && v.conf == "high":
			return "confident"
		case a.conf == "low" || v.conf == "low":
			return "low"
		default:
			return "review"
		}
	case aReal:
		return word(a.conf)
	case vReal:
		return word(v.conf)
	default:
		return "review"
	}
}

func clock(seconds float64) string {
	m := int(seconds) / 60
	s := seconds - float64(m*60)
	return fmt.Sprintf("%02d:%04.1f", m, s)
}

// printJSON emits the same rows as a machine-readable object. Each side is null
// only when there is no reference for it; detected=false marks a looked-up
// (sub-threshold) score.
func printJSON(rows []row, mode, record string) {
	type jsonSide struct {
		Score       float64 `json:"score"`
		WorstSecond float64 `json:"worst_second"`
		Confidence  string  `json:"confidence,omitempty"`
		Detected    bool    `json:"detected"`
	}
	type jsonDetection struct {
		Advert       string    `json:"advert"`
		StartSeconds float64   `json:"start_seconds"`
		Audio        *jsonSide `json:"audio"`
		Video        *jsonSide `json:"video"`
		Verdict      string    `json:"verdict"`
	}

	toSide := func(c *cell) *jsonSide {
		if c == nil {
			return nil
		}
		return &jsonSide{
			Score:       round(c.score, 4),
			WorstSecond: round(c.worst, 4),
			Confidence:  c.conf,
			Detected:    c.real,
		}
	}

	out := struct {
		Mode       string          `json:"mode"`
		Record     string          `json:"record"`
		Detections []jsonDetection `json:"detections"`
	}{Mode: mode, Record: record, Detections: []jsonDetection{}}

	for _, r := range rows {
		out.Detections = append(out.Detections, jsonDetection{
			Advert:       r.advert,
			StartSeconds: round(r.start, 2),
			Audio:        toSide(r.audio),
			Video:        toSide(r.video),
			Verdict:      verdict(r.audio, r.video),
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// binBase strips a fingerprint suffix so a media path or a .bin path both reduce
// to the same base (rec.mp4, rec.mp4.audio.bin, rec.mp4.video.bin → rec.mp4).
func binBase(path string) string {
	for _, s := range []string{".video.bin", ".audio.bin", ".bin"} {
		if strings.HasSuffix(path, s) {
			return strings.TrimSuffix(path, s)
		}
	}
	return path
}

// classify reports which kind a reference .bin is, from its name. A plain .bin
// (no modality suffix) is treated as audio for backward compatibility.
func classify(name string) string {
	switch {
	case strings.HasSuffix(name, ".video.bin"):
		return "video"
	case strings.HasSuffix(name, ".audio.bin"):
		return "audio"
	case strings.HasSuffix(name, ".bin"):
		return "audio"
	default:
		return ""
	}
}

// advertKey strips the modality suffix from a reference filename so the audio and
// video fingerprints of the same advert share one key.
func advertKey(name string) string {
	name = filepath.Base(name)
	for _, s := range []string{".video.bin", ".audio.bin", ".bin"} {
		if strings.HasSuffix(name, s) {
			return strings.TrimSuffix(name, s)
		}
	}
	return name
}

func round(v float64, places int) float64 {
	scale := 1.0
	for range places {
		scale *= 10
	}
	return float64(int(v*scale+0.5)) / scale
}
