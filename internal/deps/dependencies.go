// Package deps verifies that external tools the app relies on are available.
package deps

import (
	"fmt"
	"os/exec"
)

// Verify checks that every required external tool is installed and on PATH.
// It returns an error naming the first missing tool, or nil if all are present.
func Verify() error {
	required := []string{"ffmpeg"}
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool %q not found in PATH", tool)
		}
	}
	return nil
}
