// Package cli wires the pieces together into a command-line program: parse
// flags, fingerprint the inputs, run the matcher, and print the results.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bluram/advert-detect/internal/deps"
	"github.com/bluram/advert-detect/internal/fingerprint"
	"github.com/bluram/advert-detect/internal/matcher"
)

// recorder filenames often begin with a timestamp like 2026-07-13-14-00-05_...
var timestampRE = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2})`)

// advertExtensions are the media types accepted from -adverts-dir.
var advertExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".flac": true,
	".ogg": true, ".mp4": true, ".mkv": true, ".mov": true, ".avi": true,
}

type options struct {
	record         string
	advert         string
	advertsDir     string
	minScore       float64
	minSecondScore float64
	minGap         float64
	asJSON         bool
	verbose        bool
}

// advertResult holds the outcome of searching one advert.
type advertResult struct {
	label      string
	detections []matcher.Detection
	err        error
}

// Run parses args, executes the search, and returns a process exit code:
// 0 = at least one advert found, 1 = none found, 2 = usage or runtime error.
func Run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if err := deps.Verify(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	logf := func(format string, a ...any) {
		if opts.verbose {
			fmt.Fprintf(os.Stderr, format+"\n", a...)
		}
	}

	advertPaths, err := collectAdverts(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	cfg := fingerprint.DefaultConfig()

	// Fingerprint every advert first; this also tells us the longest one, so the
	// record can be prepared to fit them all.
	logf("fingerprinting %d advert(s)", len(advertPaths))
	type advertFP struct {
		label string
		fp    *fingerprint.Fingerprint
	}
	var adverts []advertFP
	maxFrames := 0
	for _, path := range advertPaths {
		fp, err := fingerprint.FromFile(path, cfg, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", path, err)
			continue
		}
		if fp.NumFrames() > maxFrames {
			maxFrames = fp.NumFrames()
		}
		adverts = append(adverts, advertFP{label: filepath.Base(path), fp: fp})
	}
	if len(adverts) == 0 {
		fmt.Fprintln(os.Stderr, "error: no adverts could be fingerprinted")
		return 2
	}

	logf("fingerprinting recording %s", opts.record)
	recordFP, err := fingerprint.FromFile(opts.record, cfg, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	// Prepare the record once, then match every advert against it.
	prepared := matcher.Prepare(recordFP, maxFrames)
	params := matcher.DefaultParams()
	params.MinScore = opts.minScore
	params.MinSecondScore = opts.minSecondScore
	params.MinGapSeconds = opts.minGap

	results := make([]advertResult, 0, len(adverts))
	for _, a := range adverts {
		detections, err := prepared.Find(a.fp, params)
		results = append(results, advertResult{label: a.label, detections: detections, err: err})
	}

	recordStart := parseRecordStart(opts.record)
	if opts.asJSON {
		printJSON(os.Stdout, opts, results, recordStart)
	} else {
		printHuman(os.Stdout, opts, results, recordStart)
	}

	for _, r := range results {
		if len(r.detections) > 0 {
			return 0
		}
	}
	return 1
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("advert-detect", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.record, "record", "", "path to the recording (audio or video)")
	fs.StringVar(&opts.advert, "advert", "", "path to a single advert to find")
	fs.StringVar(&opts.advertsDir, "adverts-dir", "", "directory of adverts to search against the recording")
	fs.Float64Var(&opts.minScore, "min-score", 0.62, "absolute floor on overall agreement (0..1)")
	fs.Float64Var(&opts.minSecondScore, "min-second-score", 0.40, "every 1-second block must agree at least this much")
	fs.Float64Var(&opts.minGap, "min-gap", 0, "minimum seconds between hits (0 = 80% of advert length)")
	fs.BoolVar(&opts.asJSON, "json", false, "emit results as JSON")
	fs.BoolVar(&opts.verbose, "v", false, "verbose logging to stderr")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.record == "" {
		return options{}, fmt.Errorf("-record is required")
	}
	if opts.advert == "" && opts.advertsDir == "" {
		return options{}, fmt.Errorf("provide -advert or -adverts-dir")
	}
	return opts, nil
}

// collectAdverts gathers advert file paths from -advert and/or -adverts-dir.
func collectAdverts(opts options) ([]string, error) {
	var paths []string
	if opts.advert != "" {
		paths = append(paths, opts.advert)
	}
	if opts.advertsDir != "" {
		entries, err := os.ReadDir(opts.advertsDir)
		if err != nil {
			return nil, fmt.Errorf("reading adverts dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if advertExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
				paths = append(paths, filepath.Join(opts.advertsDir, e.Name()))
			}
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no advert files found")
	}
	return paths, nil
}

func parseRecordStart(recordPath string) *time.Time {
	m := timestampRE.FindString(filepath.Base(recordPath))
	if m == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02-15-04-05", m)
	if err != nil {
		return nil
	}
	return &t
}

func printHuman(w io.Writer, opts options, results []advertResult, start *time.Time) {
	fmt.Fprintf(w, "\n  Recording : %s\n", opts.record)
	fmt.Fprintf(w, "  Adverts   : %d searched\n\n", len(results))

	for _, r := range results {
		switch {
		case r.err != nil:
			fmt.Fprintf(w, "  ── %-28s  error: %v\n", r.label, r.err)
		case len(r.detections) == 0:
			fmt.Fprintf(w, "  ── %-28s  ✗ not found\n", r.label)
		default:
			fmt.Fprintf(w, "  ── %-28s  ✓ %d airing(s)\n", r.label, len(r.detections))
			for i, d := range r.detections {
				line := fmt.Sprintf("        %2d  %s → %s  (%4.1fs)   score %.3f   worst/sec %.3f   %-7s",
					i+1, formatTimestamp(d.TimeSeconds), formatTimestamp(d.EndSeconds),
					d.EndSeconds-d.TimeSeconds, d.Score, d.WorstSecond, d.Confidence)
				if start != nil {
					from := start.Add(time.Duration(d.TimeSeconds * float64(time.Second)))
					to := start.Add(time.Duration(d.EndSeconds * float64(time.Second)))
					line += fmt.Sprintf("   %s → %s", from.Format("15:04:05"), to.Format("15:04:05"))
				}
				fmt.Fprintln(w, line)
			}
		}
	}
	fmt.Fprintln(w)
}

func printJSON(w io.Writer, opts options, results []advertResult, start *time.Time) {
	type jsonDetection struct {
		StartSeconds float64 `json:"start_seconds"`
		EndSeconds   float64 `json:"end_seconds"`
		Start        string  `json:"start"`
		End          string  `json:"end"`
		Score        float64 `json:"score"`
		WorstSecond  float64 `json:"worst_second"`
		Confidence   string  `json:"confidence"`
		ClockStart   string  `json:"clock_start,omitempty"`
		ClockEnd     string  `json:"clock_end,omitempty"`
	}
	type jsonResult struct {
		Advert     string          `json:"advert"`
		Found      int             `json:"found"`
		Error      string          `json:"error,omitempty"`
		Detections []jsonDetection `json:"detections"`
	}

	out := struct {
		Record          string       `json:"record"`
		AdvertsSearched int          `json:"adverts_searched"`
		Results         []jsonResult `json:"results"`
	}{Record: opts.record, AdvertsSearched: len(results)}

	for _, r := range results {
		jr := jsonResult{Advert: r.label, Found: len(r.detections)}
		if r.err != nil {
			jr.Error = r.err.Error()
		}
		for _, d := range r.detections {
			jd := jsonDetection{
				StartSeconds: round(d.TimeSeconds, 2),
				EndSeconds:   round(d.EndSeconds, 2),
				Start:        formatTimestamp(d.TimeSeconds),
				End:          formatTimestamp(d.EndSeconds),
				Score:        round(d.Score, 4),
				WorstSecond:  round(d.WorstSecond, 4),
				Confidence:   d.Confidence,
			}
			if start != nil {
				jd.ClockStart = start.Add(time.Duration(d.TimeSeconds * float64(time.Second))).Format(time.RFC3339)
				jd.ClockEnd = start.Add(time.Duration(d.EndSeconds * float64(time.Second))).Format(time.RFC3339)
			}
			jr.Detections = append(jr.Detections, jd)
		}
		out.Results = append(out.Results, jr)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func formatTimestamp(seconds float64) string {
	m := int(seconds) / 60
	s := seconds - float64(m*60)
	return fmt.Sprintf("%02d:%04.1f", m, s)
}

func round(v float64, places int) float64 {
	scale := 1.0
	for i := 0; i < places; i++ {
		scale *= 10
	}
	return float64(int(v*scale+0.5)) / scale
}
