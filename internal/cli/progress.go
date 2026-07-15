package cli

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// progress reports work-in-progress to stderr. It only animates when stderr is
// an interactive terminal, so piped or redirected output stays clean.
type progress struct {
	enabled bool
}

func newProgress(quiet bool) *progress {
	return &progress{enabled: !quiet && stderrIsTerminal()}
}

func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// step runs fn while showing a spinner labelled label, then prints a final
// ✓/✗ line with the elapsed time. When disabled it just runs fn.
func (p *progress) step(label string, fn func() error) error {
	if !p.enabled {
		return fn()
	}

	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r  %c %s  %.1fs ", frames[i%len(frames)], label, time.Since(start).Seconds())
				i++
			}
		}
	}()

	err := fn()
	close(done)
	wg.Wait()

	symbol := "✓"
	if err != nil {
		symbol = "✗"
	}
	fmt.Fprintf(os.Stderr, "\r  %s %s  (%s)          \n", symbol, label, time.Since(start).Round(time.Millisecond))
	return err
}

// note prints a one-line status to stderr (no spinner).
func (p *progress) note(format string, a ...any) {
	if !p.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}
