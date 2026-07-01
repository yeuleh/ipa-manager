package appstore

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

// progressBarWrapper adapts schollz/progressbar to our Progress interface.
type progressBarWrapper struct {
	inner *progressbar.ProgressBar
}

func (w *progressBarWrapper) ChangeMax64(max int64) { w.inner.ChangeMax64(max) }
func (w *progressBarWrapper) Set64(v int64) error   { return w.inner.Set64(v) }

// NewProgress creates a Progress for interactive terminal download.
// Returns nil for non-interactive (pipe/CI) — ipatool handles nil gracefully.
func NewProgress() Progress {
	if !isInteractive() {
		return nil
	}
	pb := progressbar.NewOptions64(1,
		progressbar.OptionSetDescription("downloading"),
		progressbar.OptionSetWriter(os.Stdout),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(20),
		progressbar.OptionFullWidth(),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetRenderBlankState(true),
	)
	return &progressBarWrapper{inner: pb}
}

// isInteractive returns true if stdin is a terminal (TTY).
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// extractVersionFromPath parses the version from an ipatool-generated filename.
// ipatool generates: <bundleID>_<appID>_<version>.ipa
// For custom paths that don't follow this convention, returns "unknown".
func extractVersionFromPath(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".ipa")
	parts := strings.Split(name, "_")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	return "unknown"
}
