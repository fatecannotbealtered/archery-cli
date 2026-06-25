package cmd

import (
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

// failArg reports a validation error (exit 2).
func failArg(msg string) error {
	emitError(msg, output.E_VALIDATION)
	return ErrSilent
}

// failNotFound reports a missing resource (exit 3).
func failNotFound(msg string) error {
	emitError(msg, output.E_NOT_FOUND)
	return ErrSilent
}

// failConfirmRequired reports a missing non-interactive confirmation token (exit 5).
func failConfirmRequired(msg string) error {
	emitError(msg, output.E_CONFIRMATION_REQUIRED)
	return ErrSilent
}

// failWithCode reports an error with the given error code. The exit code is
// derived from the error code through the single status->code->exit mapping
// (exitForErrorCode) so the two can never drift apart.
func failWithCode(msg string, code output.ErrorCode) error {
	emitError(msg, code)
	return ErrSilent
}

// failWithDetails reports an error carrying extra structured details (e.g. the
// self-update stage envelope). The process exit code is derived from the error
// code through the single status->code->exit mapping (exitForErrorCode), so the
// exit can never drift from the code a caller hand-picks.
func failWithDetails(msg string, code output.ErrorCode, details map[string]any) error {
	if jsonMode {
		output.PrintErrorJSONWithDetails(msg, code, details)
	} else {
		output.Error(msg)
	}
	setExitCode(exitForErrorCode(code))
	return ErrSilent
}

// emitError emits an error message and sets the exit code derived from code.
// The exit is always derived from the error code via the single canonical mapping,
// so a caller cannot pass a mismatched exit constant.
func emitError(msg string, code output.ErrorCode) {
	if jsonMode {
		output.PrintErrorJSONWithCode(msg, 0, code)
	} else {
		output.Error(msg)
	}
	setExitCode(exitForErrorCode(code))
}

// requireFlagString returns the flag value or fails if empty.
func requireFlagString(cmd *cobra.Command, name, label string) (string, error) {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		return "", failArg(err.Error())
	}
	if v == "" {
		return "", failArg(label + " is required")
	}
	return v, nil
}
