// Command fingerprinter turns media files into .bin fingerprints that detect can
// load instantly (skipping the decode + fingerprint math). Meant to run right
// after recording, once per file.
//
// By default it makes every fingerprint the file supports: a video file yields
// <file>.audio.bin AND <file>.video.bin; an audio-only file yields just
// <file>.audio.bin. Use -type to restrict.
//
//	fingerprinter -in rec.mp4                 # auto → rec.mp4.audio.bin + rec.mp4.video.bin
//	fingerprinter -in ad.mp3                  # auto → ad.mp3.audio.bin
//	fingerprinter -in rec.mp4 -type audio     # force audio only
//	fingerprinter -in-dir ./records           # a whole folder
//
// Up-to-date .bin files are skipped unless -force is given.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bluram/detect-engine/internal/audio"
	"github.com/bluram/detect-engine/internal/deps"
	"github.com/bluram/detect-engine/internal/fingerprint"
	"github.com/bluram/detect-engine/internal/video"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// job is one fingerprint to produce: a source file, which kind, and where to
// write the .bin.
type job struct {
	src  string
	kind string // "audio" | "video"
	dst  string
}

func run(args []string) int {
	fs := flag.NewFlagSet("fingerprinter", flag.ContinueOnError)
	in := fs.String("in", "", "a single media file to fingerprint")
	inDir := fs.String("in-dir", "", "a folder of media files to fingerprint")
	out := fs.String("out", "", "output path (only valid with a single -in and a single -type)")
	typeFlag := fs.String("type", "", `restrict output: "audio", "video", or "audio,video" (default: auto by content)`)
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

	requested, err := parseTypes(*typeFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	jobs, err := collectJobs(*in, *out, *inDir, requested)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	if len(jobs) == 0 {
		fmt.Fprintln(os.Stderr, "error: nothing to fingerprint")
		return 2
	}

	audioCfg := audio.DefaultConfig()
	videoCfg := video.DefaultConfig()
	var done, skipped, failed int
	for _, j := range jobs {
		label := filepath.Base(j.dst)
		if !*force && upToDate(j.dst, j.src) {
			fmt.Printf("·  %-44s  skipped (up to date)\n", label)
			skipped++
			continue
		}
		start := time.Now()

		var fp *fingerprint.Fingerprint
		var e error
		switch j.kind {
		case "audio":
			fp, e = audio.FromFile(j.src, audioCfg, false)
		case "video":
			fp, e = video.FromFile(j.src, videoCfg)
		}
		if e != nil {
			fmt.Fprintf(os.Stderr, "✗  %-44s  %v\n", label, e)
			failed++
			continue
		}
		if e := fingerprint.SaveFile(fp, j.dst); e != nil {
			fmt.Fprintf(os.Stderr, "✗  %-44s  %v\n", label, e)
			failed++
			continue
		}
		info, _ := os.Stat(j.dst)
		fmt.Printf("✓  %-44s  %.0f KB · %s\n", label,
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

// parseTypes validates -type. An empty value means auto (returns nil).
func parseTypes(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "audio" && p != "video" {
			return nil, fmt.Errorf("invalid -type %q (use audio, video, or audio,video)", p)
		}
		out = append(out, p)
	}
	return out, nil
}

// collectJobs builds one job per (source, kind). The kinds come from -type when
// given, otherwise from auto-detecting each file's tracks.
func collectJobs(in, out, inDir string, requested []string) ([]job, error) {
	var sources []string
	if in != "" {
		sources = append(sources, in)
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
			sources = append(sources, filepath.Join(inDir, e.Name()))
		}
	}

	var jobs []job
	for _, src := range sources {
		kinds, err := resolveKinds(src, requested)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", filepath.Base(src), err)
			continue
		}
		for _, k := range kinds {
			jobs = append(jobs, job{src: src, kind: k, dst: src + "." + k + ".bin"})
		}
	}

	if out != "" {
		if len(jobs) != 1 {
			return nil, fmt.Errorf("-out needs exactly one output but %d would be written; add -type audio or -type video", len(jobs))
		}
		jobs[0].dst = out
	}
	return jobs, nil
}

// resolveKinds decides which fingerprints to make for src: the -type list when
// given (filtered to tracks that actually exist), else every track present.
func resolveKinds(src string, requested []string) ([]string, error) {
	hasAudio, hasVideo, err := probeTracks(src)
	if err != nil {
		return nil, err
	}

	if requested == nil { // auto
		var kinds []string
		if hasAudio {
			kinds = append(kinds, "audio")
		}
		if hasVideo {
			kinds = append(kinds, "video")
		}
		if len(kinds) == 0 {
			return nil, fmt.Errorf("no audio or video track")
		}
		return kinds, nil
	}

	var kinds []string
	for _, k := range requested {
		if k == "audio" && !hasAudio {
			fmt.Fprintf(os.Stderr, "warning: %s has no audio track\n", filepath.Base(src))
			continue
		}
		if k == "video" && !hasVideo {
			fmt.Fprintf(os.Stderr, "warning: %s has no video track\n", filepath.Base(src))
			continue
		}
		kinds = append(kinds, k)
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("requested type(s) not present")
	}
	return kinds, nil
}

// probeTracks reports whether a file has a real audio and/or video track, using
// ffprobe. Cover-art "video" streams (attached_pic) are ignored, so an mp3 with
// embedded artwork is not mistaken for a video.
func probeTracks(path string) (hasAudio, hasVideo bool, err error) {
	audioStreams, err := ffprobe(path, "a", "stream=codec_type")
	if err != nil {
		return false, false, err
	}
	hasAudio = len(audioStreams) > 0

	pics, err := ffprobe(path, "v", "stream_disposition=attached_pic")
	if err != nil {
		return false, false, err
	}
	for _, p := range pics {
		if strings.TrimSpace(p) == "0" { // 0 = not cover art → a real video stream
			hasVideo = true
		}
	}
	return hasAudio, hasVideo, nil
}

// ffprobe returns one CSV line per selected stream for the requested entries.
func ffprobe(path, streams, entries string) ([]string, error) {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", streams, "-show_entries", entries, "-of", "csv=p=0", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed on %s: %w", filepath.Base(path), err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
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
