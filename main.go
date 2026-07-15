// Command detect-engine finds where a short reference clip occurs inside a long
// record (audio or video).
//
//	detect-engine -record record.mp3 -reference clip.mp3
//	detect-engine -record record.mp3 -references-dir ./clips
//
// See -help for all flags. Both inputs may be audio or video files.
package main

import (
	"os"

	"github.com/bluram/detect-engine/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
