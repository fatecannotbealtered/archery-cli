package cmd

import (
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

// failArg reports a validation error (exit 2).
func failArg(msg string) error {
	emitError(msg, ExitBadArgs, output.E_VALIDATION)
	return ErrSilent
}

// failNotFound reports a missing resource (exit 3).
func failNotFound(msg string) error {
	emitError(msg, ExitNotFound, output.E_NOT_FOUND)
	return ErrSilent
}

// failAuth reports an authentication error (exit 4).
func failAuth(msg string) error {
	emitError(msg, ExitAuth, output.E_AUTH)
	return ErrSilent
}

// failConfirmRequired reports a missing non-interactive confirmation token (exit 5).
func failConfirmRequired(msg string) error {
	emitError(msg, ExitConfirm, output.E_CONFIRM_REQUIRED)
	return ErrSilent
}

// failConflict reports a resource conflict (exit 6).
func failConflict(msg string) error {
	emitError(msg, ExitConflict, output.E_CONFLICT)
	return ErrSilent
}

// failWithCode reports an error with a custom exit code and error code.
func failWithCode(msg string, exit int, code output.ErrorCode) error {
	emitError(msg, exit, code)
	return ErrSilent
}

func emitError(msg string, exit int, code output.ErrorCode) {
	if jsonMode {
		output.PrintErrorJSONWithCode(msg, 0, code)
	} else {
		output.Error(msg)
	}
	setExitCode(exit)
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
