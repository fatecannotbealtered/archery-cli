package output

import (
	"encoding/json"
	"fmt"
	"os"
)

const SchemaVersion = "1.0"

// Compact controls whether JSON is emitted without indentation (set by --compact).
var Compact bool

// DurationMS returns the current command duration for response metadata.
// The cmd package sets this hook; package-level tests and helpers default to 0.
var DurationMS func() int64

// marshalJSON encodes v according to the global Compact setting.
func marshalJSON(v any) ([]byte, error) {
	if Compact {
		return json.Marshal(v)
	}
	return json.MarshalIndent(v, "", "  ")
}

// emitJSONMarshal is the encoder for emitErrorPayload (overridable in tests).
var emitJSONMarshal = marshalJSON

// SuccessEnvelope is the JSON shape for successful responses.
type SuccessEnvelope struct {
	OK            bool   `json:"ok"`
	SchemaVersion string `json:"schema_version"`
	Data          any    `json:"data,omitempty"`
	Meta          Meta   `json:"meta,omitempty"`
}

// ErrorEnvelope is the JSON shape for error responses.
type ErrorEnvelope struct {
	OK            bool          `json:"ok"`
	SchemaVersion string        `json:"schema_version"`
	Error         EnvelopeError `json:"error"`
	Meta          Meta          `json:"meta,omitempty"`
}

// Meta holds command execution metadata.
type Meta struct {
	DurationMS int64 `json:"duration_ms"`
}

// EnvelopeError holds structured error details.
type EnvelopeError struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	Retryable bool           `json:"retryable"`
}

func commandDurationMS() int64 {
	if DurationMS == nil {
		return 0
	}
	return DurationMS()
}

// NewSuccessEnvelope builds a success envelope for the given data.
func NewSuccessEnvelope(v any) SuccessEnvelope {
	return SuccessEnvelope{
		OK:            true,
		SchemaVersion: SchemaVersion,
		Data:          v,
		Meta:          Meta{DurationMS: commandDurationMS()},
	}
}

// NewErrorEnvelope builds an error envelope with the given code.
func NewErrorEnvelope(msg string, statusCode int, code ErrorCode) ErrorEnvelope {
	details := map[string]any{}
	if statusCode != 0 {
		details["status_code"] = statusCode
	}
	return ErrorEnvelope{
		OK:            false,
		SchemaVersion: SchemaVersion,
		Error: EnvelopeError{
			Code:      code,
			Message:   msg,
			Details:   details,
			Retryable: RetryableErrorCode(code),
		},
		Meta: Meta{DurationMS: commandDurationMS()},
	}
}

// ErrorCode classifies errors for machine consumption.
type ErrorCode string

const (
	E_CONFIG                ErrorCode = "E_CONFIG"
	E_AUTH                  ErrorCode = "E_AUTH"
	E_FORBIDDEN             ErrorCode = "E_FORBIDDEN"
	E_NOT_FOUND             ErrorCode = "E_NOT_FOUND"
	E_USAGE                 ErrorCode = "E_USAGE"
	E_VALIDATION            ErrorCode = "E_VALIDATION"
	E_CONFIRMATION_REQUIRED ErrorCode = "E_CONFIRMATION_REQUIRED"
	E_CONFLICT              ErrorCode = "E_CONFLICT"
	E_RATE_LIMIT            ErrorCode = "E_RATE_LIMIT"
	E_SERVER                ErrorCode = "E_SERVER"
	E_NETWORK               ErrorCode = "E_NETWORK"
	E_TIMEOUT               ErrorCode = "E_TIMEOUT"
	E_UNKNOWN               ErrorCode = "E_UNKNOWN"
)

// ExitCode constants for process exit codes.
const (
	ExitOK              = 0
	ExitBadArgs         = 2
	ExitNotFound        = 3
	ExitAuth            = 4
	ExitConfirmRequired = 5
	ExitConflict        = 6
	ExitTransient       = 7
	ExitTimeout         = 8
)

// ErrorCodeFromStatus maps HTTP status codes to error codes.
func ErrorCodeFromStatus(statusCode int) ErrorCode {
	switch statusCode {
	case 401:
		return E_AUTH
	case 403:
		return E_FORBIDDEN
	case 404:
		return E_NOT_FOUND
	case 409:
		return E_CONFLICT
	case 429:
		return E_RATE_LIMIT
	default:
		if statusCode >= 500 {
			return E_SERVER
		}
		if statusCode >= 400 {
			return E_USAGE
		}
		return E_UNKNOWN
	}
}

// HintForErrorCode returns an actionable hint for the given error code.
func HintForErrorCode(code ErrorCode) string {
	switch code {
	case E_CONFIG:
		return "Run 'archery-cli auth login --url <url> --username <user> --password <pass>' or set ARCHERY_CLI_URL, ARCHERY_CLI_USERNAME, and ARCHERY_CLI_PASSWORD"
	case E_AUTH:
		return "Check your credentials; run 'archery-cli auth login' to re-authenticate"
	case E_FORBIDDEN:
		return "Check your permissions and role on the target project or group"
	case E_NOT_FOUND:
		return "Verify the resource exists and you have permission to view it"
	case E_USAGE:
		return "Check command arguments and flags"
	case E_VALIDATION:
		return "Check command arguments and flags"
	case E_CONFIRMATION_REQUIRED:
		return "Run the same command with --dry-run, inspect the preview, then retry with --confirm <confirm_token>"
	case E_CONFLICT:
		return "Resource conflict; another change may have happened concurrently. Re-fetch and retry"
	case E_RATE_LIMIT:
		return "Wait and retry; reduce request frequency"
	case E_SERVER:
		return "Server error; try again later"
	case E_NETWORK:
		return "Check host URL and network connectivity"
	case E_TIMEOUT:
		return "The operation timed out; retry with backoff"
	default:
		return ""
	}
}

// RetryableErrorCode reports whether the error code represents a transient failure.
func RetryableErrorCode(code ErrorCode) bool {
	switch code {
	case E_RATE_LIMIT, E_SERVER, E_NETWORK, E_TIMEOUT:
		return true
	default:
		return false
	}
}

// PrintJSON outputs v as a success envelope JSON to stdout.
// Marshal errors are written to stderr and terminate the process with exit code 2.
func PrintJSON(v any) {
	if err := PrintJSONErr(v); err != nil {
		os.Exit(2)
	}
}

// PrintJSONErr outputs v as a success envelope JSON to stdout.
// On marshal failure it writes to stderr and returns the error.
func PrintJSONErr(v any) error {
	data, err := marshalJSON(NewSuccessEnvelope(v))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "json marshal error: %v\n", err)
		return err
	}
	fmt.Println(string(data))
	return nil
}

// PrintErrorJSON outputs a machine-readable error envelope as JSON to stderr.
func PrintErrorJSON(msg string, statusCode int) {
	code := ErrorCodeFromStatus(statusCode)
	if statusCode == 0 {
		code = E_UNKNOWN
	}
	emitErrorPayload(msg, statusCode, code)
}

// PrintErrorJSONWithCode outputs an error envelope with an explicit error code to stderr.
// The exitCode parameter is for the caller's use; it is not emitted in the JSON.
func PrintErrorJSONWithCode(msg string, exitCode int, code ErrorCode) {
	emitErrorPayload(msg, exitCode, code)
}

func emitErrorPayload(msg string, statusCode int, code ErrorCode) {
	payload := NewErrorEnvelope(msg, statusCode, code)
	if hint := HintForErrorCode(code); hint != "" {
		payload.Error.Details["hint"] = hint
	}
	data, err := emitJSONMarshal(payload)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, `{"ok":false,"schema_version":%q,"error":{"code":%q,"message":%q,"details":{},"retryable":false}}`+"\n", SchemaVersion, code, msg)
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, string(data))
}
