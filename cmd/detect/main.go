// Command detect searches one recording for known adverts and reports where they
// aired. Its -mode says what kind of content it is:
//
//   - -mode audio : match the sound only (radio) → one audio score.
//   - -mode video : match picture AND sound (TV) → both scores + a verdict,
//     because a video has both tracks.
//
// It finds fingerprints by name convention. Given -record rec.mp4 it loads
// rec.mp4.audio.bin (and rec.mp4.video.bin in video mode); in -references it
// pairs each advert's .audio.bin ↔ .video.bin by base name. Fingerprint the
// recording and the adverts first with `fingerprinter`.
//
//	detect -mode audio -record rec.mp3 -references references/audio
//	detect -mode video -record rec.mp4 -references references/video
package main

import (
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
	audioDets, err := matchSide(base+".audio.bin", *references, "audio")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	var videoDets map[string][]matcher.Detection
	if *mode == "video" {
		videoDets, err = matchSide(base+".video.bin", *references, "video")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
	}

	rows := combine(audioDets, videoDets)
	printTable(rows, *mode)
	if len(rows) == 0 {
		return 1
	}
	return 0
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

// matchSide loads the record fingerprint for one modality and matches it against
// the references of that same kind, returning detections keyed by advert. A
// missing record fingerprint is a warning, not an error (returns nil).
func matchSide(recordBin, refsDir, kind string) (map[string][]matcher.Detection, error) {
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

	type ref struct {
		key string
		fp  *fingerprint.Fingerprint
	}
	var refs []ref
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
		refs = append(refs, ref{key: advertKey(e.Name()), fp: fp})
	}

	prepared := matcher.Prepare(record, maxFrames)
	params := matcher.DefaultParams()
	out := make(map[string][]matcher.Detection)
	for _, r := range refs {
		dets, err := prepared.Find(r.fp, params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", r.key, err)
			continue
		}
		out[r.key] = append(out[r.key], dets...)
	}
	return out, nil
}

// row is one air-time of one advert, with whichever sides matched.
type row struct {
	advert string
	start  float64
	audio  *matcher.Detection
	video  *matcher.Detection
}

// combine joins audio and video detections of the same advert by overlapping
// air-time. Overlapping detections become one row with both scores; the rest
// stay single-sided.
func combine(audioDets, videoDets map[string][]matcher.Detection) []row {
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
				v := vDets[paired]
				rows = append(rows, row{advert: key, start: a.TimeSeconds, audio: &a, video: &v})
			} else {
				rows = append(rows, row{advert: key, start: a.TimeSeconds, audio: &a})
			}
		}
		for j := range vDets {
			if !usedV[j] {
				v := vDets[j]
				rows = append(rows, row{advert: key, start: v.TimeSeconds, video: &v})
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].start < rows[j].start })
	return rows
}

// overlaps reports whether two detections cover overlapping air-time.
func overlaps(a, b matcher.Detection) bool {
	return a.TimeSeconds < b.EndSeconds && b.TimeSeconds < a.EndSeconds
}

func printTable(rows []row, mode string) {
	fmt.Printf("\n  %-26s %-9s  %-6s  %-6s  %s\n", "Advert", "Time", "Audio", "Video", "Verdict")
	fmt.Printf("  %s\n", strings.Repeat("─", 72))
	if len(rows) == 0 {
		fmt.Println("  (no detections)")
		fmt.Println()
		return
	}
	for _, r := range rows {
		fmt.Printf("  %-26s %-9s  %-6s  %-6s  %s\n",
			displayName(r.advert), clock(r.start),
			scoreCell(r.audio), scoreCell(r.video), verdict(r.audio, r.video, mode))
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

func scoreCell(d *matcher.Detection) string {
	if d == nil {
		return "  –"
	}
	return fmt.Sprintf("%.3f", d.Score)
}

func verdict(a, v *matcher.Detection, mode string) string {
	if mode == "audio" {
		return a.Confidence // audio-only run: the audio confidence is the verdict
	}
	switch {
	case a != nil && v != nil:
		if a.Confidence == "high" && v.Confidence == "high" {
			return "confirmed (audio + video)"
		}
		return "review (weak on one side)"
	case a != nil:
		return "audio-only (no video match)"
	default:
		return "video-only"
	}
}

func clock(seconds float64) string {
	m := int(seconds) / 60
	s := seconds - float64(m*60)
	return fmt.Sprintf("%02d:%04.1f", m, s)
}
