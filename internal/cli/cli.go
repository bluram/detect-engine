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
	"time"

	"github.com/bluram/advert-detect/internal/deps"
	"github.com/bluram/advert-detect/internal/fingerprint"
	"github.com/bluram/advert-detect/internal/matcher"
)

// recorder filenames often begin with a timestamp like 2026-07-13-14-00-05_...
var timestampRE = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}-\d{2}-\d{2}-\d{2})`)

type options struct {
	record         string
	advert         string
	minScore       float64
	minSecondScore float64
	minGap         float64
	asJSON         bool
	verbose        bool
}

// Run parses args, executes the search, and returns a process exit code:
// 0 = advert found, 1 = not found, 2 = usage or runtime error.
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

	cfg := fingerprint.DefaultConfig()

	logf("fingerprinting advert %s", opts.advert)
	advertFP, err := fingerprint.FromFile(opts.advert, cfg, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	logf("fingerprinting recording %s", opts.record)
	recordFP, err := fingerprint.FromFile(opts.record, cfg, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	params := matcher.DefaultParams()
	params.MinScore = opts.minScore
	params.MinSecondScore = opts.minSecondScore
	params.MinGapSeconds = opts.minGap

	detections, err := matcher.Find(recordFP, advertFP, params)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	recordStart := parseRecordStart(opts.record)
	if opts.asJSON {
		printJSON(os.Stdout, opts, detections, recordStart)
	} else {
		printHuman(os.Stdout, opts, detections, recordStart)
	}

	if len(detections) > 0 {
		return 0
	}
	return 1
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("advert-detect", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.record, "record", "", "path to the recording (audio or video)")
	fs.StringVar(&opts.advert, "advert", "", "path to the advert to find (audio or video)")
	fs.Float64Var(&opts.minScore, "min-score", 0.62, "absolute floor on overall agreement (0..1)")
	fs.Float64Var(&opts.minSecondScore, "min-second-score", 0.40, "every 1-second block must agree at least this much")
	fs.Float64Var(&opts.minGap, "min-gap", 0, "minimum seconds between hits (0 = 80% of advert length)")
	fs.BoolVar(&opts.asJSON, "json", false, "emit results as JSON")
	fs.BoolVar(&opts.verbose, "v", false, "verbose logging to stderr")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.record == "" || opts.advert == "" {
		return options{}, fmt.Errorf("both -record and -advert are required")
	}
	return opts, nil
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

func printHuman(w io.Writer, opts options, detections []matcher.Detection, start *time.Time) {
	fmt.Fprintf(w, "\n  Recording : %s\n", opts.record)
	fmt.Fprintf(w, "  Advert    : %s\n\n", opts.advert)

	if len(detections) == 0 {
		fmt.Fprintln(w, "  ✗ Advert not found in this recording.")
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintf(w, "  ✓ Found %d airing(s):\n\n", len(detections))
	for i, d := range detections {
		line := fmt.Sprintf("    %2d  at %s   score %.3f   worst/sec %.3f   %-7s",
			i+1, formatTimestamp(d.TimeSeconds), d.Score, d.WorstSecond, d.Confidence)
		if start != nil {
			clock := start.Add(time.Duration(d.TimeSeconds * float64(time.Second)))
			line += "   " + clock.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
}

func printJSON(w io.Writer, opts options, detections []matcher.Detection, start *time.Time) {
	type jsonDetection struct {
		TimeSeconds float64 `json:"time_seconds"`
		Timestamp   string  `json:"timestamp"`
		Score       float64 `json:"score"`
		WorstSecond float64 `json:"worst_second"`
		Confidence  string  `json:"confidence"`
		ClockTime   string  `json:"clock_time,omitempty"`
	}

	out := struct {
		Record     string          `json:"record"`
		Advert     string          `json:"advert"`
		Found      int             `json:"found"`
		Detections []jsonDetection `json:"detections"`
	}{Record: opts.record, Advert: opts.advert, Found: len(detections)}

	for _, d := range detections {
		jd := jsonDetection{
			TimeSeconds: round(d.TimeSeconds, 2),
			Timestamp:   formatTimestamp(d.TimeSeconds),
			Score:       round(d.Score, 4),
			WorstSecond: round(d.WorstSecond, 4),
			Confidence:  d.Confidence,
		}
		if start != nil {
			jd.ClockTime = start.Add(time.Duration(d.TimeSeconds * float64(time.Second))).Format(time.RFC3339)
		}
		out.Detections = append(out.Detections, jd)
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
