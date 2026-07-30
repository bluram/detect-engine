// Command fingerprinter turns media files into .bin fingerprints that the
// detect-engine search can load instantly (skipping the decode + fingerprint
// math). Meant to run right after recording, once per record.
//
//	fingerprinter -in record.mp3                 # one file → record.mp3.bin
//	fingerprinter -in record.mp3 -out r.bin      # custom output path
//	fingerprinter -in-dir ./records              # a whole folder → <file>.bin each
//
// Inputs may be audio or video (ffmpeg extracts the audio track). Files that
// already have an up-to-date .bin are skipped unless -force is given.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bluram/detect-engine/internal/audio"
	"github.com/bluram/detect-engine/internal/deps"
	"github.com/bluram/detect-engine/internal/fingerprint"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type job struct{ src, dst string }

func run(args []string) int {
	fs := flag.NewFlagSet("fingerprinter", flag.ContinueOnError)
	in := fs.String("in", "", "a single media file to fingerprint")
	inDir := fs.String("in-dir", "", "a folder of media files to fingerprint (writes <file>.bin next to each)")
	out := fs.String("out", "", "output .bin path for -in (default: <in>.bin)")
	force := fs.Bool("force", false, "re-fingerprint even if an up-to-date .bin already exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" && *inDir == "" {
		fmt.Fprintln(os.Stderr, "error: provide -in <file> or -in-dir <folder>")
		fs.Usage()
		return 2
	}
	if err := deps.Verify(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	jobs, err := collectJobs(*in, *out, *inDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if len(jobs) == 0 {
		fmt.Fprintln(os.Stderr, "error: no media files found")
		return 2
	}

	cfg := audio.DefaultConfig()
	var done, skipped, failed int
	for _, j := range jobs {
		if !*force && upToDate(j.dst, j.src) {
			fmt.Printf("·  %-40s  skipped (up to date)\n", filepath.Base(j.src))
			skipped++
			continue
		}
		start := time.Now()
		fp, err := audio.FromFile(j.src, cfg, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗  %-40s  %v\n", filepath.Base(j.src), err)
			failed++
			continue
		}
		if err := fingerprint.SaveFile(fp, j.dst); err != nil {
			fmt.Fprintf(os.Stderr, "✗  %-40s  %v\n", filepath.Base(j.src), err)
			failed++
			continue
		}
		info, _ := os.Stat(j.dst)
		fmt.Printf("✓  %-40s  %.0f KB · %s\n", filepath.Base(j.dst),
			float64(info.Size())/1024, time.Since(start).Round(time.Millisecond))
		done++
	}

	if len(jobs) > 1 {
		fmt.Printf("\n%d done · %d skipped · %d failed\n", done, skipped, failed)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// collectJobs builds the list of (source, .bin destination) pairs to process.
func collectJobs(in, out, inDir string) ([]job, error) {
	var jobs []job

	if in != "" {
		dst := out
		if dst == "" {
			dst = in + ".bin"
		}
		jobs = append(jobs, job{src: in, dst: dst})
	}

	if inDir != "" {
		entries, err := os.ReadDir(inDir)
		if err != nil {
			return nil, fmt.Errorf("reading folder: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() || !audio.IsMediaFile(e.Name()) {
				continue
			}
			src := filepath.Join(inDir, e.Name())
			jobs = append(jobs, job{src: src, dst: src + ".bin"})
		}
	}

	return jobs, nil
}

// upToDate reports whether the .bin exists and is at least as new as its source.
func upToDate(dst, src string) bool {
	di, err := os.Stat(dst)
	if err != nil {
		return false
	}
	si, err := os.Stat(src)
	if err != nil {
		return false
	}
	return !di.ModTime().Before(si.ModTime())
}
