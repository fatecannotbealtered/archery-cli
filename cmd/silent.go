package cmd

// ErrSilent is defined in root.go.
// This file exists to document the convention: commands return ErrSilent
// after printing their own error output so cobra does not print again.
