// Command advert-detect finds where a short advert airs inside a long recording.
//
//	advert-detect -record recording.mp3 -advert advert.mp3
//
// See -help for all flags. Both inputs may be audio or video files.
package main

import (
	"os"

	"github.com/bluram/advert-detect/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
