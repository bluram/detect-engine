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

	"github.com/bluram/detect-engine/internal/deps"
	"github.com/bluram/detect-engine/internal/fingerprint"
	"github.com/bluram/detect-engine/internal/matcher"
)

// record filenames often begin with a timestamp like 2026-07-13-14-00-05_...
var timestampRE = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2})`)

// mediaExtensions are the file types accepted from -queries-dir.
var mediaExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".m4a": true, ".aac": true, ".flac": true,
	".ogg": true, ".mp4": true, ".mkv": true, ".mov": true, ".avi": true,
}

type options struct {
	record         string
	query          string
	queriesDir     string
	minScore       float64
	minSecondScore float64
	minGap         float64
	asJSON         bool
	quiet          bool
}

// queryFP is a fingerprinted query with its display label.
type queryFP struct {
	label string
	fp    *fingerprint.Fingerprint
}

// queryResult holds the outcome of searching one query.
type queryResult struct {
	label      string
	detections []matcher.Detection
	err        error
}

// Run parses args, executes the search, and returns a process exit code:
// 0 = at least one query found, 1 = none found, 2 = usage or runtime error.
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

	prog := newProgress(opts.quiet)

	queryPaths, err := collectQueries(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	cfg := fingerprint.DefaultConfig()

	// Fingerprint every query first (also gives the longest, to size the record).
	var queries []queryFP
	var warnings []string
	maxFrames := 0
	_ = prog.step(fmt.Sprintf("fingerprinting %d query(ies)", len(queryPaths)), func() error {
		for _, path := range queryPaths {
			fp, err := fingerprint.FromFile(path, cfg, true)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skipping %s: %v", path, err))
				continue
			}
			if fp.NumFrames() > maxFrames {
				maxFrames = fp.NumFrames()
			}
			queries = append(queries, queryFP{label: filepath.Base(path), fp: fp})
		}
		return nil
	})
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if len(queries) == 0 {
		fmt.Fprintln(os.Stderr, "error: no queries could be fingerprinted")
		return 2
	}

	// Fingerprint the record — the slow step, so it gets its own spinner.
	var recordFP *fingerprint.Fingerprint
	err = prog.step("fingerprinting record", func() error {
		var e error
		recordFP, e = fingerprint.FromFile(opts.record, cfg, false)
		return e
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	// Prepare the record once, then match every query against it.
	prepared := matcher.Prepare(recordFP, maxFrames)
	params := matcher.DefaultParams()
	params.MinScore = opts.minScore
	params.MinSecondScore = opts.minSecondScore
	params.MinGapSeconds = opts.minGap
	recordStart := parseRecordStart(opts.record)

	// JSON mode: collect everything, then emit one object.
	if opts.asJSON {
		results := make([]queryResult, 0, len(queries))
		for _, q := range queries {
			detections, err := prepared.Find(q.fp, params)
			results = append(results, queryResult{label: q.label, detections: detections, err: err})
		}
		printJSON(os.Stdout, opts, results, recordStart)
		return exitCode(results)
	}

	// Human mode: stream each query's result as it finishes.
	prog.note("matching %d query(ies)...", len(queries))
	printHeader(os.Stdout, opts.record, len(queries))
	results := make([]queryResult, 0, len(queries))
	for _, q := range queries {
		detections, err := prepared.Find(q.fp, params)
		r := queryResult{label: q.label, detections: detections, err: err}
		results = append(results, r)
		printQueryBlock(os.Stdout, r, recordStart)
	}
	fmt.Fprintln(os.Stdout)
	return exitCode(results)
}

func exitCode(results []queryResult) int {
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
	fs.StringVar(&opts.record, "record", "", "path to the record to search in (audio or video)")
	fs.StringVar(&opts.query, "query", "", "path to a single query to find")
	fs.StringVar(&opts.queriesDir, "queries-dir", "", "directory of queries to search against the record")
	fs.Float64Var(&opts.minScore, "min-score", 0.62, "absolute floor on overall agreement (0..1)")
	fs.Float64Var(&opts.minSecondScore, "min-second-score", 0.40, "every 1-second block must agree at least this much")
	fs.Float64Var(&opts.minGap, "min-gap", 0, "minimum seconds between matches (0 = 80% of query length)")
	fs.BoolVar(&opts.asJSON, "json", false, "emit results as JSON")
	fs.BoolVar(&opts.quiet, "quiet", false, "suppress progress output")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.record == "" {
		return options{}, fmt.Errorf("-record is required")
	}
	if opts.query == "" && opts.queriesDir == "" {
		return options{}, fmt.Errorf("provide -query or -queries-dir")
	}
	return opts, nil
}

// collectQueries gathers query file paths from -query and/or -queries-dir.
func collectQueries(opts options) ([]string, error) {
	var paths []string
	if opts.query != "" {
		paths = append(paths, opts.query)
	}
	if opts.queriesDir != "" {
		entries, err := os.ReadDir(opts.queriesDir)
		if err != nil {
			return nil, fmt.Errorf("reading queries dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if mediaExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
				paths = append(paths, filepath.Join(opts.queriesDir, e.Name()))
			}
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no query files found")
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

func printHeader(w io.Writer, record string, nQueries int) {
	fmt.Fprintf(w, "\n  Record  : %s\n", record)
	fmt.Fprintf(w, "  Queries : %d searched\n\n", nQueries)
}

func printQueryBlock(w io.Writer, r queryResult, start *time.Time) {
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

func printJSON(w io.Writer, opts options, results []queryResult, start *time.Time) {
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
		Query      string          `json:"query"`
		Found      int             `json:"found"`
		Error      string          `json:"error,omitempty"`
		Detections []jsonDetection `json:"detections"`
	}

	out := struct {
		Record          string       `json:"record"`
		QueriesSearched int          `json:"queries_searched"`
		Results         []jsonResult `json:"results"`
	}{Record: opts.record, QueriesSearched: len(results)}

	for _, r := range results {
		jr := jsonResult{Query: r.label, Found: len(r.detections)}
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
