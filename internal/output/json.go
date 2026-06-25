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

// UpdateNoticesProvider returns the cached update notices to attach to
// meta.notices on every response. The cmd package wires this to a read-only,
// TTL-bounded local cache reader (no network). Nil (or an empty result) leaves
// meta.notices absent. Kept as a hook to avoid an output->cmd import cycle.
var UpdateNoticesProvider func() []any

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
	// Notices carries ambient operational notices (currently the cached
	// update-available notice) read-only from the local cache. Omitted when the
	// cache has nothing to report (CLI-SPEC §3, §14).
	Notices []any `json:"notices,omitempty"`
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

// newMeta builds the meta block, attaching cached update notices (if any) from
// the provider hook. The provider must read only the local cache (no network).
func newMeta() Meta {
	meta := Meta{DurationMS: commandDurationMS()}
	if UpdateNoticesProvider != nil {
		if notices := UpdateNoticesProvider(); len(notices) > 0 {
			meta.Notices = notices
		}
	}
	return meta
}

// NewSuccessEnvelope builds a success envelope for the given data.
func NewSuccessEnvelope(v any) SuccessEnvelope {
	return SuccessEnvelope{
		OK:            true,
		SchemaVersion: SchemaVersion,
		Data:          v,
		Meta:          newMeta(),
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
		Meta: newMeta(),
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
	E_2FA_REQUIRED          ErrorCode = "E_2FA_REQUIRED"
	E_CONFLICT              ErrorCode = "E_CONFLICT"
	E_RATE_LIMIT            ErrorCode = "E_RATE_LIMIT"
	E_SERVER                ErrorCode = "E_SERVER"
	E_NETWORK               ErrorCode = "E_NETWORK"
	E_TIMEOUT               ErrorCode = "E_TIMEOUT"
	E_INTEGRITY             ErrorCode = "E_INTEGRITY"
	E_IO                    ErrorCode = "E_IO"
	E_INTERRUPTED           ErrorCode = "E_INTERRUPTED"
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
	// ExitHumanRequired marks operations that cannot proceed without a human
	// action the agent cannot supply non-interactively (e.g. a fresh 2FA code).
	ExitHumanRequired = 9
	// ExitIO marks a local filesystem failure (disk full, file locked, partial
	// write) during a self-update replace; needs an environment fix, not a retry.
	ExitIO = 1
	// ExitInterrupted marks an operation cancelled by SIGINT/SIGTERM (128+2).
	ExitInterrupted = 130
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
	case 408:
		return E_TIMEOUT
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
	case E_2FA_REQUIRED:
		return "This Archery account has 2FA enabled. Re-run with --otp <6-digit code> (or set ARCHERY_CLI_OTP). The code expires in ~30s, so generate it just before running"
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
	case E_INTEGRITY:
		return "Release integrity verification failed (signature or checksum); do not retry. Re-run update to fetch the current release, or report a possible supply-chain issue"
	case E_IO:
		return "Local filesystem failure during install (permission, disk space, or a locked file); fix the environment, then re-run update"
	case E_INTERRUPTED:
		return "Operation cancelled; nothing was left half-applied. Re-run update (it is idempotent) or run the reported next step"
	default:
		return ""
	}
}

// RetryableErrorCode reports whether the error code represents a transient failure.
func RetryableErrorCode(code ErrorCode) bool {
	switch code {
	case E_RATE_LIMIT, E_SERVER, E_NETWORK, E_TIMEOUT, E_INTERRUPTED:
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

// PrintErrorJSON outputs a machine-readable error envelope as JSON to stdout —
// the failure envelope is the single document agents parse (CLI-SPEC §4).
func PrintErrorJSON(msg string, statusCode int) {
	code := ErrorCodeFromStatus(statusCode)
	if statusCode == 0 {
		code = E_UNKNOWN
	}
	emitErrorPayload(msg, statusCode, code)
}

// PrintErrorJSONWithCode outputs an error envelope with an explicit error code to stdout.
// The exitCode parameter is for the caller's use; it is not emitted in the JSON.
func PrintErrorJSONWithCode(msg string, exitCode int, code ErrorCode) {
	emitErrorPayload(msg, exitCode, code)
}

// PrintErrorJSONWithDetails outputs an error envelope with an explicit error
// code and extra structured details (merged into error.details) to stdout. Used
// by self-update so every failure carries stage/current_version/binary_replaced/
// skill_sync_status (CLI-SPEC §14).
func PrintErrorJSONWithDetails(msg string, code ErrorCode, extra map[string]any) {
	payload := NewErrorEnvelope(msg, 0, code)
	for k, v := range extra {
		payload.Error.Details[k] = v
	}
	if hint := HintForErrorCode(code); hint != "" {
		payload.Error.Details["hint"] = hint
	}
	data, err := emitJSONMarshal(payload)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, `{"ok":false,"schema_version":%q,"error":{"code":%q,"message":%q,"details":{},"retryable":false}}`+"\n", SchemaVersion, code, msg)
		return
	}
	fmt.Println(string(data))
}

func emitErrorPayload(msg string, statusCode int, code ErrorCode) {
	payload := NewErrorEnvelope(msg, statusCode, code)
	if hint := HintForErrorCode(code); hint != "" {
		payload.Error.Details["hint"] = hint
	}
	data, err := emitJSONMarshal(payload)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, `{"ok":false,"schema_version":%q,"error":{"code":%q,"message":%q,"details":{},"retryable":false}}`+"\n", SchemaVersion, code, msg)
		return
	}
	fmt.Println(string(data))
}
