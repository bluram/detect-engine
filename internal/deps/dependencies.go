package deps

import (
	"log"
	"os/exec"
)

func Verify() {

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		log.Fatal("ffmpeg not found in PATH")
	}

}
