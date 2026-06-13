package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/audit"
	"github.com/fatecannotbealtered/archery-cli/internal/config"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

// Exit codes for machine-readable error classification.
const (
	ExitOK        = 0
	ExitError     = 1
	ExitBadArgs   = 2
	ExitNotFound  = 3
	ExitAuth      = 4
	ExitForbidden = 4
	ExitConfirm   = 5
	ExitCancelled = 5
	ExitConflict  = 6
	ExitRateLimit = 7
	ExitNetwork   = 7
	ExitTimeout   = 8
)

// ErrSilent indicates the error has been printed; cobra should not print again.
var ErrSilent = errors.New("")

// version is injected by goreleaser ldflags.
var version = "dev"

// Global flags.
var (
	jsonMode       = true
	jsonAlias      bool
	compactJSON    bool
	quietMode      bool
	dryRun         bool
	dangerousMode  bool
	regionFlag     string
	formatMode     = formatJSON
	insecureTLS    bool
	timeoutSeconds int
)

const defaultTimeoutSeconds = 30

const (
	formatJSON = "json"
	formatText = "text"
	formatRaw  = "raw"
)

// lastExit tracks the exit code for the current command execution.
var lastExit int

// cmdStartTime records when the current command began (for audit logging).
var cmdStartTime time.Time

// activeCmd is the innermost command currently running (for context propagation).
var activeCmd *cobra.Command

// LastExitCode returns the exit code from the last command execution.
func LastExitCode() int { return lastExit }

// apiCtx returns the context for API calls from CLI commands (honours SIGINT when set).
func apiCtx() context.Context {
	if activeCmd != nil {
		if ctx := activeCmd.Context(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

// setExitCode sets the exit code (only increases severity, never decreases).
func setExitCode(code int) {
	if code > lastExit {
		lastExit = code
	}
}

var rootCmd = &cobra.Command{
	Use:           "archery-cli",
	Short:         "Archery SQL audit platform CLI for AI Agents",
	Version:       version,
	SilenceErrors: true,
	SilenceUsage:  true,
	Long: fmt.Sprintf("\n  %s\n  %s",
		output.FormatCyanBold("archery-cli"),
		output.FormatGray("Agent-native Archery SQL audit platform control")),
}

func init() {
	rootCmd.Version = version
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.PersistentFlags().StringVar(&formatMode, "format", formatJSON, "Output format: json|text|raw")
	rootCmd.PersistentFlags().BoolVar(&jsonAlias, "json", false, "Compatibility alias for --format json")
	rootCmd.PersistentFlags().BoolVar(&compactJSON, "compact", false, "Compact JSON (no indentation; only affects --format json)")
	rootCmd.PersistentFlags().BoolVar(&quietMode, "quiet", false, "Suppress non-JSON stdout output (for scripts and AI Agents)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without executing")
	rootCmd.PersistentFlags().BoolVar(&dangerousMode, "dangerous", false, "Enable high/critical risk write commands; required in both dry-run and confirm steps")
	rootCmd.PersistentFlags().StringVar(&regionFlag, "region", "", "Override active region (default: config default_region)")
	rootCmd.PersistentFlags().BoolVar(&insecureTLS, "insecure", false, "Skip TLS certificate verification (corporate/self-signed CA)")
	rootCmd.PersistentFlags().IntVar(&timeoutSeconds, "timeout", defaultTimeoutSeconds, "HTTP request timeout in seconds")
	initConfirmFlag()
	installUpdateNoticeHelp(rootCmd)

	cobra.OnInitialize(func() {
		output.Quiet = quietMode
		output.Compact = compactJSON
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cmdStartTime = time.Now()
		activeCmd = cmd
		if err := applyFormatFlags(cmd); err != nil {
			return err
		}
		initClientOptions(cmd)
		return nil
	}

	rootCmd.PersistentPostRunE = func(cmd *cobra.Command, args []string) error {
		if !isWriteCommand(cmd) {
			return nil
		}
		duration := time.Since(cmdStartTime)
		audit.Log(cmd.CommandPath(), os.Args[1:], lastExit, duration.Milliseconds())
		return nil
	}
}

// Execute runs the root command with a background context.
func Execute() error {
	return ExecuteContext(context.Background())
}

// ExecuteContext runs the root command with the given context (e.g. signal.NotifyContext).
func ExecuteContext(ctx context.Context) error {
	lastExit = 0
	cmdStartTime = time.Now()
	output.DurationMS = func() int64 {
		if cmdStartTime.IsZero() {
			return 0
		}
		return time.Since(cmdStartTime).Milliseconds()
	}
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if errors.Is(err, ErrSilent) {
			return err
		}
		emitError(err.Error(), ExitBadArgs, output.E_VALIDATION)
		return ErrSilent
	}
	return nil
}

// handleAPIError handles API errors with JSON mode support.
func handleAPIError(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		msg := apiErr.Error()
		code := output.ErrorCodeFromStatus(apiErr.StatusCode)
		if jsonMode {
			output.PrintErrorJSONWithCode(msg, apiErr.StatusCode, code)
		} else {
			output.Error(msg)
		}
		setExitCode(exitCodeForStatus(apiErr.StatusCode))
		return ErrSilent
	}
	msg := err.Error()
	if jsonMode {
		output.PrintErrorJSONWithCode(msg, 0, output.E_NETWORK)
	} else {
		output.Error(msg)
	}
	setExitCode(ExitNetwork)
	return ErrSilent
}

// exitCodeForStatus maps HTTP status codes to semantic exit codes.
func exitCodeForStatus(status int) int {
	switch {
	case status == 401:
		return ExitAuth
	case status == 403:
		return ExitForbidden
	case status == 404:
		return ExitNotFound
	case status == 409:
		return ExitConflict
	case status == 429:
		return ExitRateLimit
	case status >= 500:
		return ExitNetwork
	default:
		return ExitBadArgs
	}
}

func dryRunOutputWithPayload(action string, detail map[string]any, confirmPayload any) bool {
	if !dryRun {
		return false
	}
	cmdPath := action
	if activeCmd != nil {
		cmdPath = activeCmd.CommandPath()
	}
	if jsonMode {
		if detail == nil {
			detail = map[string]any{}
		}
		changes := []map[string]any{}
		if len(detail) > 0 {
			changes = append(changes, detail)
		}
		confirmCtx := ""
		if cfg, err := config.Load(); err == nil {
			confirmCtx = confirmContext(cfg)
		}
		token, expires := newConfirmToken(cmdPath, confirmCtx, confirmPayload)
		output.PrintJSON(map[string]any{
			"preview": map[string]any{
				"action":  action,
				"changes": changes,
			},
			"confirm_token": token,
			"expires_at":    expires.Format(time.RFC3339),
		})
	} else {
		output.Info("[dry-run] " + action)
	}
	return true
}

// markDryRunOrConfirm is a convenience that handles both --dry-run preview and
// --confirm token validation for write commands. Returns true if the command
// should return immediately (dry-run was shown, or an error was emitted).
// The confirm token binds the command path, operation args, and region context.
func markDryRunOrConfirm(action string, detail map[string]any) bool {
	return markDryRunOrConfirmWithPayload(action, detail, detail)
}

func markDryRunOrConfirmWithPayload(action string, detail map[string]any, confirmPayload any) bool {
	cmdPath := action
	if activeCmd != nil {
		cmdPath = activeCmd.CommandPath()
	}
	if requiresDangerousGate(activeCmd) {
		if !dangerousMode {
			return failDangerousRequired(activeCmd)
		}
		detail = withDangerousPreview(detail)
		confirmPayload = map[string]any{
			"dangerous": true,
			"operation": confirmPayload,
		}
	}
	confirmCtx := ""
	if cfg, err := config.Load(); err == nil {
		confirmCtx = confirmContext(cfg)
	}
	if dryRunOutputWithPayload(action, detail, confirmPayload) {
		return true
	}
	if err := requireConfirm(activeCmd, cmdPath, confirmCtx, confirmPayload); err != nil {
		return true
	}
	return false
}

func requiresDangerousGate(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Annotations == nil || cmd.Annotations["write"] != "true" {
		return false
	}
	risk := strings.ToLower(strings.TrimSpace(cmd.Annotations["riskLevel"]))
	return risk == "high" || risk == "critical"
}

func failDangerousRequired(cmd *cobra.Command) bool {
	name := "this command"
	if cmd != nil {
		name = cmd.CommandPath()
	}
	emitError(name+" is high risk and requires --dangerous in both dry-run and confirm steps", ExitConfirm, output.E_CONFIRMATION_REQUIRED)
	return true
}

func withDangerousPreview(detail map[string]any) map[string]any {
	out := make(map[string]any, len(detail)+1)
	for k, v := range detail {
		out[k] = v
	}
	out["dangerous"] = true
	return out
}

func confirmContext(cfg *config.Config) string {
	regionName := regionFlag
	if regionName == "" && cfg != nil {
		regionName = config.ActiveRegion(cfg)
	}
	if regionName == "" {
		return ""
	}
	if cfg == nil {
		return regionName
	}
	region, ok := cfg.Regions[regionName]
	if !ok || strings.TrimSpace(region.Username) == "" {
		return regionName
	}
	return regionName + "|" + strings.TrimSpace(region.Username)
}

func applyFormatFlags(cmd *cobra.Command) error {
	requested := strings.ToLower(strings.TrimSpace(formatMode))
	if requested == "" {
		requested = formatJSON
	}
	formatFlag := rootCmd.PersistentFlags().Lookup("format")
	jsonFlag := rootCmd.PersistentFlags().Lookup("json")
	jsonChanged := jsonFlag != nil && jsonFlag.Changed
	formatChanged := formatFlag != nil && formatFlag.Changed

	if !jsonChanged && !formatChanged && !jsonMode {
		requested = formatText
	}

	if jsonChanged && jsonAlias && formatChanged && requested != formatJSON {
		jsonMode = true
		return failArg("--json cannot be used with --format " + requested)
	}
	if jsonChanged && jsonAlias {
		requested = formatJSON
	}
	if requested != formatJSON && requested != formatText && requested != formatRaw {
		jsonMode = true
		return failArg("--format must be one of: json, text, raw")
	}

	formatMode = requested
	jsonMode = formatMode == formatJSON

	if formatMode != formatJSON {
		if f := cmd.Flags().Lookup("fields"); f != nil && f.Changed {
			return failArg("--fields is only supported with --format json")
		}
	}
	if !commandSupportsFormat(cmd, formatMode) {
		return failArg(fmt.Sprintf("%s does not support --format %s", cmd.CommandPath(), formatMode))
	}
	return nil
}

func commandSupportsFormat(cmd *cobra.Command, format string) bool {
	for _, allowed := range commandOutputFormats(cmd) {
		if allowed == format {
			return true
		}
	}
	return false
}

func commandOutputFormats(cmd *cobra.Command) []string {
	if cmd == nil || cmd.Annotations == nil {
		return []string{formatJSON, formatText}
	}
	raw := strings.TrimSpace(cmd.Annotations["formats"])
	if raw == "" {
		return []string{formatJSON, formatText}
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{formatJSON, formatText}
	}
	return out
}

// isWriteCommand returns true if the command has the "write" annotation.
func isWriteCommand(cmd *cobra.Command) bool {
	return cmd.Annotations["write"] == "true"
}

// markWrite sets the "write" annotation on a command for audit logging.
func markWrite(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["write"] = "true"
	markConfirm(cmd)
}

// markConfirm marks commands that require the non-interactive dry-run/confirm flow.
func markConfirm(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["confirm"] = "true"
}

// markRiskLevel sets agent risk metadata: low, medium, high, critical.
func markRiskLevel(cmd *cobra.Command, level string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["riskLevel"] = level
}

func markOutputFormats(cmd *cobra.Command, formats ...string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["formats"] = strings.Join(formats, ",")
}

// newClient loads config and creates an API client for the active region.
func newClient() (*api.Client, *config.Config, *config.RegionConfig, error) {
	cfg, err := config.MustLoad()
	if err != nil {
		code := output.E_CONFIG
		exit := ExitAuth
		if strings.Contains(err.Error(), "not logged in") || strings.Contains(err.Error(), "no credentials") {
			code = output.E_AUTH
		}
		if jsonMode {
			output.PrintErrorJSONWithCode(err.Error(), 0, code)
		} else {
			output.Error(err.Error())
		}
		setExitCode(exit)
		return nil, nil, nil, ErrSilent
	}

	regionName := regionFlag
	if regionName == "" {
		regionName = config.ActiveRegion(cfg)
	}
	region, ok := cfg.Regions[regionName]
	if !ok {
		msg := fmt.Sprintf("region %q not found in config", regionName)
		if jsonMode {
			output.PrintErrorJSONWithCode(msg, 0, output.E_NOT_FOUND)
		} else {
			output.Error(msg)
		}
		setExitCode(ExitNotFound)
		return nil, nil, nil, ErrSilent
	}

	client := api.NewClient(region.URL)
	if region.AccessToken != "" {
		client.SetTokens(region.AccessToken, region.RefreshToken)
	} else if strings.TrimSpace(region.Username) != "" && strings.TrimSpace(region.Password) != "" {
		accessToken, refreshToken, err := client.Auth.Login(apiCtx(), region.Username, region.Password)
		if err != nil {
			return nil, nil, nil, handleAPIError(err)
		}
		client.SetTokens(accessToken, refreshToken)
	}
	// Session-mode commands (legacy Django endpoints) need a form login; pass
	// the credentials when available. JWT-only configs leave these empty and
	// such commands return a clear "needs username/password" error.
	client.SetSessionCredentials(region.Username, region.Password)
	return client, cfg, &region, nil
}

// activeRegionName returns the effective region name from the flag or config.
func activeRegionName(cfg *config.Config) string {
	if regionFlag != "" {
		return regionFlag
	}
	return config.ActiveRegion(cfg)
}

// getFieldsFlag returns the parsed []string for a --fields flag, or nil if absent.
func getFieldsFlag(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	raw, _ := cmd.Flags().GetString("fields")
	if raw == "" {
		return nil
	}
	out := []string{}
	for _, part := range splitCSV(raw) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitCSV(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, trim(cur))
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, trim(cur))
	return out
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// InsecureTLS returns true if --insecure is active (for doctor checks).
func InsecureTLS() bool { return insecureTLS }

func applyInsecureFromEnv() {
	if insecureTLS {
		return
	}
	v := strings.TrimSpace(os.Getenv("ARCHERY_CLI_INSECURE"))
	if v == "1" || strings.EqualFold(v, "true") {
		insecureTLS = true
	}
}

func timeoutExplicitlySet(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("timeout")
	return f != nil && f.Changed
}

func applyTimeoutFromEnv(cmd *cobra.Command) int {
	if timeoutExplicitlySet(cmd) {
		if timeoutSeconds > 0 {
			return timeoutSeconds
		}
		return defaultTimeoutSeconds
	}
	if s := strings.TrimSpace(os.Getenv("ARCHERY_CLI_TIMEOUT")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return defaultTimeoutSeconds
}

func initClientOptions(cmd *cobra.Command) {
	applyInsecureFromEnv()
	sec := applyTimeoutFromEnv(cmd)
	api.SetClientOptions(api.ClientOptions{
		Timeout:            time.Duration(sec) * time.Second,
		InsecureSkipVerify: insecureTLS,
	})
}
