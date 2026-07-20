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
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/bluram/detect-engine/internal/fingerprint"
	"github.com/bluram/detect-engine/internal/matcher"
)

// record filenames often begin with a timestamp like 2026-07-13-14-00-05_...
var timestampRE = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2})`)

type options struct {
	record         string
	reference      string
	referencesDir  string
	minScore       float64
	minSecondScore float64
	minGap         float64
	asJSON         bool
	quiet          bool
}

// referenceFP is a fingerprinted reference with its display label.
type referenceFP struct {
	label string
	fp    *fingerprint.Fingerprint
}

// referenceResult holds the outcome of searching one reference.
type referenceResult struct {
	label      string
	detections []matcher.Detection
	err        error
}

// Run parses args, executes the search, and returns a process exit code:
// 0 = at least one reference found, 1 = none found, 2 = usage or runtime error.
func Run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	prog := newProgress(opts.quiet)

	referencePaths, err := collectReferences(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	// Load every reference fingerprint (also gives the longest, to size the record).
	var references []referenceFP
	var warnings []string
	maxFrames := 0
	_ = prog.step(fmt.Sprintf("loading %d reference(s)", len(referencePaths)), func() error {
		for _, path := range referencePaths {
			fp, err := fingerprint.LoadFile(path)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skipping %s: %v", path, err))
				continue
			}
			if fp.NumFrames() > maxFrames {
				maxFrames = fp.NumFrames()
			}
			references = append(references, referenceFP{label: filepath.Base(path), fp: fp})
		}
		return nil
	})
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if len(references) == 0 {
		fmt.Fprintln(os.Stderr, "error: no references could be loaded")
		return 2
	}

	// Load the record fingerprint.
	var recordFP *fingerprint.Fingerprint
	err = prog.step("loading record", func() error {
		var e error
		recordFP, e = fingerprint.LoadFile(opts.record)
		return e
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	// Prepare the record once, then match every reference against it.
	prepared := matcher.Prepare(recordFP, maxFrames)
	params := matcher.DefaultParams()
	params.MinScore = opts.minScore
	params.MinSecondScore = opts.minSecondScore
	params.MinGapSeconds = opts.minGap
	recordStart := parseRecordStart(opts.record)

	// JSON mode: collect everything, then emit one object.
	if opts.asJSON {
		results := matchAll(prepared, references, params)
		printJSON(os.Stdout, opts, results, recordStart)
		return exitCode(results)
	}

	// Human mode: match all references (in parallel), then print in order.
	prog.note("matching %d reference(s)...", len(references))
	printHeader(os.Stdout, opts.record, len(references))
	matchStart := time.Now()
	results := matchAll(prepared, references, params)
	for _, r := range results {
		printReferenceBlock(os.Stdout, r, recordStart)
	}
	fmt.Fprintln(os.Stdout)
	prog.note("matched %d reference(s) in %s", len(references), time.Since(matchStart).Round(time.Millisecond))
	return exitCode(results)
}

// matchAll searches every reference against the prepared record concurrently,
// using one worker per CPU. Results are returned in the original order.
// This is safe because Find only reads the shared PreparedRecord and allocates
// its own scratch buffers; the dsp FFT holds no shared state.
func matchAll(prepared *matcher.PreparedRecord, references []referenceFP, params matcher.Params) []referenceResult {
	results := make([]referenceResult, len(references))

	workers := runtime.NumCPU()
	if workers > len(references) {
		workers = len(references)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				q := references[i]
				detections, err := prepared.Find(q.fp, params)
				results[i] = referenceResult{label: q.label, detections: detections, err: err}
			}
		}()
	}
	for i := range references {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	return results
}

func exitCode(results []referenceResult) int {
	for _, r := range results {
		if len(r.detections) > 0 {
			return 0
		}
	}
	return 1
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("detect-engine", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.record, "record", "", "path to the record fingerprint (.bin, made by fingerprinter)")
	fs.StringVar(&opts.reference, "reference", "", "path to a single reference fingerprint (.bin)")
	fs.StringVar(&opts.referencesDir, "references-dir", "", "directory of reference .bin files")
	fs.Float64Var(&opts.minScore, "min-score", 0.62, "absolute floor on overall agreement (0..1)")
	fs.Float64Var(&opts.minSecondScore, "min-second-score", 0.40, "every 1-second block must agree at least this much")
	fs.Float64Var(&opts.minGap, "min-gap", 0, "minimum seconds between matches (0 = 80% of reference length)")
	fs.BoolVar(&opts.asJSON, "json", false, "emit results as JSON")
	fs.BoolVar(&opts.quiet, "quiet", false, "suppress progress output")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.record == "" {
		return options{}, fmt.Errorf("-record is required")
	}
	if opts.reference == "" && opts.referencesDir == "" {
		return options{}, fmt.Errorf("provide -reference or -references-dir")
	}
	return opts, nil
}

// collectReferences gathers reference file paths from -reference and/or -references-dir.
func collectReferences(opts options) ([]string, error) {
	var paths []string
	if opts.reference != "" {
		paths = append(paths, opts.reference)
	}
	if opts.referencesDir != "" {
		entries, err := os.ReadDir(opts.referencesDir)
		if err != nil {
			return nil, fmt.Errorf("reading references dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if filepath.Ext(e.Name()) == ".bin" {
				paths = append(paths, filepath.Join(opts.referencesDir, e.Name()))
			}
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no reference files found")
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

func printHeader(w io.Writer, record string, nReferences int) {
	fmt.Fprintf(w, "\n  %-10s : %s\n", "Record", record)
	fmt.Fprintf(w, "  %-10s : %d searched\n\n", "References", nReferences)
}

func printReferenceBlock(w io.Writer, r referenceResult, start *time.Time) {
	switch {
	case r.err != nil:
		fmt.Fprintf(w, "  ── %-28s  error: %v\n", r.label, r.err)
	case len(r.detections) == 0:
		fmt.Fprintf(w, "  ── %-28s  ✗ not found\n", r.label)
	default:
		fmt.Fprintf(w, "  ── %-28s  ✓ %d match(es)\n", r.label, len(r.detections))
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

func printJSON(w io.Writer, opts options, results []referenceResult, start *time.Time) {
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
		Reference  string          `json:"reference"`
		Found      int             `json:"found"`
		Error      string          `json:"error,omitempty"`
		Detections []jsonDetection `json:"detections"`
	}

	out := struct {
		Record             string       `json:"record"`
		ReferencesSearched int          `json:"references_searched"`
		Results            []jsonResult `json:"results"`
	}{Record: opts.record, ReferencesSearched: len(results)}

	for _, r := range results {
		jr := jsonResult{Reference: r.label, Found: len(r.detections)}
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
	for range places {
		scale *= 10
	}
	return float64(int(v*scale+0.5)) / scale
}
