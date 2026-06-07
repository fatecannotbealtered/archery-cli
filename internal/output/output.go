package output

import (
	"fmt"
	"os"
	"strings"
)

// ANSI color codes
const (
	ansiReset    = "\033[0m"
	ansiBold     = "\033[1m"
	ansiRed      = "\033[31m"
	ansiGreen    = "\033[32m"
	ansiYellow   = "\033[33m"
	ansiBlue     = "\033[34m"
	ansiCyan     = "\033[36m"
	ansiWhite    = "\033[97m"
	ansiGray     = "\033[90m"
	ansiBoldCyan = "\033[1;36m"
)

// isTerminal checks if a file descriptor is a terminal (TTY).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// noColor reports whether colored output should be suppressed.
// Evaluated lazily on each call so that tests / runtime env changes
// (NO_COLOR=1, redirected stdout) take effect without process restart.
func noColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	return !isTerminal(os.Stdout)
}

// Quiet suppresses all non-error output when true.
var Quiet bool

// colorize wraps msg with ANSI color codes when color is enabled.
func colorize(code, msg string) string {
	if noColor() {
		return msg
	}
	return code + msg + ansiReset
}

// Success prints a green checkmark message to stdout.
func Success(msg string) {
	if Quiet {
		return
	}
	fmt.Println(colorize(ansiGreen, "✔ "+msg))
}

// Error prints a red cross message to stderr.
func Error(msg string) {
	fmt.Fprintln(os.Stderr, colorize(ansiRed, "✖ "+msg))
}

// Warn prints a yellow warning message to stderr.
func Warn(msg string) {
	fmt.Fprintln(os.Stderr, colorize(ansiYellow, "⚠ "+msg))
}

// Info prints a blue info message to stdout.
func Info(msg string) {
	if Quiet {
		return
	}
	fmt.Println(colorize(ansiBlue, "ℹ "+msg))
}

// Debug prints a gray debug message to stderr.
func Debug(msg string) {
	if Quiet {
		return
	}
	fmt.Fprintln(os.Stderr, colorize(ansiGray, "[debug] "+msg))
}

// Bold prints a bold message to stdout.
func Bold(msg string) {
	if Quiet {
		return
	}
	fmt.Println(colorize(ansiBold, msg))
}

// Gray prints a gray message to stdout.
func Gray(msg string) {
	if Quiet {
		return
	}
	fmt.Println(colorize(ansiGray, msg))
}

// FormatCyan returns a cyan formatted string.
func FormatCyan(s string) string { return colorize(ansiCyan, s) }

// FormatCyanBold returns a cyan bold formatted string.
func FormatCyanBold(s string) string { return colorize(ansiBoldCyan, s) }

// FormatGray returns a gray formatted string.
func FormatGray(s string) string { return colorize(ansiGray, s) }

// FormatGreen returns a green formatted string.
func FormatGreen(s string) string { return colorize(ansiGreen, s) }

// FormatRed returns a red formatted string.
func FormatRed(s string) string { return colorize(ansiRed, s) }

// FormatYellow returns a yellow formatted string.
func FormatYellow(s string) string { return colorize(ansiYellow, s) }

// FormatBlue returns a blue formatted string.
func FormatBlue(s string) string { return colorize(ansiBlue, s) }

// StatusBadge returns a colored string based on a generic state name.
func StatusBadge(state string) string {
	switch strings.ToLower(state) {
	case "running", "pending", "in_progress", "open":
		return colorize(ansiBlue, state)
	case "success", "closed", "merged", "approved", "resolved":
		return colorize(ansiGreen, state)
	case "failed", "canceled", "rejected", "declined":
		return colorize(ansiRed, state)
	case "skipped", "manual", "scheduled", "draft":
		return colorize(ansiYellow, state)
	case "locked", "created":
		return colorize(ansiWhite, state)
	default:
		return colorize(ansiGray, state)
	}
}
