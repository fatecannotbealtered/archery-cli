package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/config"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check configuration, connectivity, and API availability",
	RunE:  runDoctor,
}

const skillMinVersion = "1.0.0"

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	type doctorCheck struct {
		Check  string  `json:"check"`
		Status string  `json:"status"`
		Fix    *string `json:"fix"`
	}
	type doctorResult struct {
		Version         string         `json:"version"`
		Notices         []updateNotice `json:"notices,omitempty"`
		Checks          []doctorCheck  `json:"checks"`
		ConfigExists    bool           `json:"configExists"`
		ReadOnly        bool           `json:"readOnly"`
		AuthValid       bool           `json:"authValid"`
		LatencyMs       int64          `json:"latencyMs"`
		Region          string         `json:"region,omitempty"`
		Mode            string         `json:"mode,omitempty"`
		URL             string         `json:"url,omitempty"`
		Username        string         `json:"username,omitempty"`
		CredentialStore string         `json:"credentialStore,omitempty"`
		Error           string         `json:"error,omitempty"`
	}

	result := doctorResult{Version: version, ReadOnly: ReadOnly(), Notices: refreshUpdateNotices(apiCtx(), "doctor")}
	check := func(name, status, fix string) {
		var fixPtr *string
		if fix != "" {
			fixPtr = &fix
		}
		result.Checks = append(result.Checks, doctorCheck{Check: name, Status: status, Fix: fixPtr})
	}

	if versionMeetsSkillMinimum(version, skillMinVersion) {
		check("version", "pass", "")
	} else {
		check("version", "fail", "upgrade archery-cli to at least "+skillMinVersion)
	}
	check("release_readiness", releaseReadinessCheckStatus(), releaseReadinessCheckFix())

	if ReadOnly() {
		check("read-only", "warn", "read-only mode is on; write commands are disabled. Unset --read-only / ARCHERY_CLI_READONLY to enable writes")
	}

	cfg, err := config.Load()
	if err != nil {
		result.Error = err.Error()
		check("config", "fail", "run archery-cli auth login or fix the config file")
		if jsonMode {
			output.PrintJSON(result)
		} else {
			output.Error("Reading config: " + err.Error())
		}
		setExitCode(ExitAuth)
		return ErrSilent
	}

	regionName := activeRegionName(cfg)
	result.Region = regionName

	region, ok := cfg.Regions[regionName]
	if !ok || region.URL == "" {
		result.ConfigExists = false
		result.Error = fmt.Sprintf("not configured for region %q: run 'archery-cli auth login'", regionName)
		check("config", "fail", "run archery-cli auth login or set ARCHERY_CLI_URL")
		if jsonMode {
			output.PrintJSON(result)
		} else {
			fmt.Println()
			output.Bold("  archery-cli Doctor")
			output.Gray("  ────────────────────────────────────────")
			fmt.Println()
			output.Error(result.Error)
			fmt.Println()
		}
		setExitCode(ExitAuth)
		return ErrSilent
	}
	result.ConfigExists = true
	result.URL = region.URL
	result.Username = region.Username
	result.Mode = effectiveMode(region)
	check("config", "pass", "")

	// TLS security check
	if InsecureTLS() {
		check("tls", "warn", "TLS certificate verification is disabled (--insecure); re-enable for production use")
	} else {
		check("tls", "pass", "")
	}

	// Credential store check
	if config.KeyringAvailable() {
		result.CredentialStore = config.CredentialStoreKeyring
		check("credential-store", "pass", "")
	} else {
		result.CredentialStore = config.CredentialStoreNone
		check("credential-store", "warn", "OS keyring unavailable; auth login cannot persist credentials securely. Use env vars for one-shot commands or enable the OS credential store")
	}

	// Test connectivity by exercising the region's transport: a JWT verify/login
	// for jwt mode, or a session establishment for the default session mode.
	client := api.NewClient(region.URL)
	client.SetMode(result.Mode)
	client.SetOTP(effectiveOTP())

	hasCreds, authFix, verifyErr := doctorVerify(client, region, result.Mode, &result.LatencyMs)

	if verifyErr != nil {
		result.AuthValid = false
		result.Error = verifyErr.Error()
		var apiErr *api.APIError
		if asAPI(verifyErr, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
			check("network", "pass", "")
			check("auth", "fail", authFix)
		} else {
			check("network", "fail", "set HTTP_PROXY or check VPN/connectivity")
			check("auth", "fail", "retry after network connectivity is restored")
		}
		if jsonMode {
			output.PrintJSON(result)
		} else {
			fmt.Println()
			output.Bold("  archery-cli Doctor")
			output.Gray("  ────────────────────────────────────────")
			fmt.Println()
			output.Error("Connection failed: " + verifyErr.Error())
			fmt.Println()
		}
		if asAPI(verifyErr, &apiErr) {
			setExitCode(exitCodeForStatus(apiErr.StatusCode))
		} else {
			setExitCode(ExitNetwork)
		}
		return ErrSilent
	}

	if !hasCreds {
		result.LatencyMs = -1
		check("network", "pass", "")
		check("auth", "skip", "no credentials configured; run 'archery-cli auth login'")
	} else {
		result.AuthValid = true
		check("network", "pass", "")
		check("auth", "pass", "")
	}

	if jsonMode {
		output.PrintJSON(result)
		return nil
	}

	fmt.Println()
	output.Bold("  archery-cli Doctor")
	output.Gray("  ────────────────────────────────────────")
	fmt.Println()
	output.Success("Config found")
	if InsecureTLS() {
		output.Warn("TLS verification disabled (--insecure)")
	}
	if ReadOnly() {
		output.Warn("Read-only mode: write commands disabled")
	}
	if result.AuthValid {
		if result.Mode == config.ModeJWT {
			output.Success("JWT token valid")
		} else {
			output.Success("Session valid")
		}
	}
	output.Success(fmt.Sprintf("Connected to %s (mode: %s)", region.URL, result.Mode))
	if result.Username != "" {
		output.Success(fmt.Sprintf("Authenticated as %s", result.Username))
	}
	output.Gray(fmt.Sprintf("  Credential store: %s", result.CredentialStore))
	if result.LatencyMs >= 0 {
		output.Gray(fmt.Sprintf("  Latency: %dms", result.LatencyMs))
	}
	printUpdateNoticeHint(os.Stdout, result.Notices)
	fmt.Println()
	return nil
}

// doctorVerify exercises the region's transport to confirm credentials work.
// Returns the verification error (nil on success), whether any credentials were
// present to verify, and the fix hint to show on an auth failure.
func doctorVerify(client *api.Client, region config.RegionConfig, mode string, latencyMs *int64) (hasCreds bool, authFix string, err error) {
	if mode == config.ModeJWT {
		switch {
		case region.AccessToken != "":
			client.SetTokens(region.AccessToken, region.RefreshToken)
			start := time.Now()
			e := client.Auth.Verify(apiCtx(), region.AccessToken)
			*latencyMs = time.Since(start).Milliseconds()
			return true, "token expired or invalid; run 'archery-cli auth login' to re-authenticate", e
		case region.Username != "" && region.Password != "":
			start := time.Now()
			_, _, e := client.Auth.Login(apiCtx(), region.Username, region.Password)
			*latencyMs = time.Since(start).Milliseconds()
			return true, "check username and password", e
		default:
			return false, "", nil
		}
	}

	// Session mode: a cached cookie proves nothing until the server accepts it,
	// but re-establishing the session needs the password. Verify by logging in
	// when credentials are available; otherwise trust the cached cookie.
	switch {
	case region.Username != "" && region.Password != "":
		start := time.Now()
		_, _, e := client.Auth.LoginWithSession(apiCtx(), region.Username, region.Password)
		*latencyMs = time.Since(start).Milliseconds()
		return true, "check username and password", e
	case region.SessionID != "":
		*latencyMs = -1
		return true, "session cookie may be expired; run 'archery-cli auth login'", nil
	default:
		return false, "", nil
	}
}

func versionMeetsSkillMinimum(current, minimum string) bool {
	current = strings.TrimSpace(current)
	minimum = strings.TrimSpace(minimum)
	if minimum == "" || strings.EqualFold(minimum, "Unreleased") {
		return true
	}
	if current == "" || current == "dev" || current == "(devel)" || strings.Contains(current, "devel") {
		return true
	}
	return compareVersions(current, minimum) >= 0
}

// asAPI is a small helper to keep doctor.go decoupled from errors.As call site sprawl.
func asAPI(err error, target **api.APIError) bool {
	type unwrapper interface{ Unwrap() error }
	for cur := err; cur != nil; {
		if v, ok := cur.(*api.APIError); ok {
			*target = v
			return true
		}
		u, ok := cur.(unwrapper)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}
