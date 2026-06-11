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

const skillMinVersion = "Unreleased"

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
		AuthValid       bool           `json:"authValid"`
		LatencyMs       int64          `json:"latencyMs"`
		Region          string         `json:"region,omitempty"`
		URL             string         `json:"url,omitempty"`
		Username        string         `json:"username,omitempty"`
		CredentialStore string         `json:"credentialStore,omitempty"`
		Error           string         `json:"error,omitempty"`
	}

	result := doctorResult{Version: version, Notices: refreshUpdateNotices(apiCtx(), "doctor")}
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

	// Test connectivity by attempting to authenticate or verify token
	client := api.NewClient(region.URL)
	if region.AccessToken != "" {
		client.SetTokens(region.AccessToken, region.RefreshToken)
	}

	// Try to verify the token if we have one
	if region.AccessToken != "" {
		start := time.Now()
		err := client.Auth.Verify(apiCtx(), region.AccessToken)
		latency := time.Since(start).Milliseconds()
		result.LatencyMs = latency

		if err != nil {
			result.AuthValid = false
			result.Error = err.Error()
			var apiErr *api.APIError
			if asAPI(err, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
				check("network", "pass", "")
				check("auth", "fail", "token expired or invalid; run 'archery-cli auth login' to re-authenticate")
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
				output.Error("Connection failed: " + err.Error())
				fmt.Println()
			}
			if asAPI(err, &apiErr) {
				setExitCode(exitCodeForStatus(apiErr.StatusCode))
			} else {
				setExitCode(ExitNetwork)
			}
			return ErrSilent
		}

		result.AuthValid = true
		check("network", "pass", "")
		check("auth", "pass", "")
	} else if region.Username != "" && region.Password != "" {
		// No cached token; try logging in to verify credentials
		start := time.Now()
		_, _, loginErr := client.Auth.Login(apiCtx(), region.Username, region.Password)
		latency := time.Since(start).Milliseconds()
		result.LatencyMs = latency

		if loginErr != nil {
			result.AuthValid = false
			result.Error = loginErr.Error()
			var apiErr *api.APIError
			if asAPI(loginErr, &apiErr) && (apiErr.StatusCode == 401 || apiErr.StatusCode == 403) {
				check("network", "pass", "")
				check("auth", "fail", "check username and password")
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
				output.Error("Connection failed: " + loginErr.Error())
				fmt.Println()
			}
			if asAPI(loginErr, &apiErr) {
				setExitCode(exitCodeForStatus(apiErr.StatusCode))
			} else {
				setExitCode(ExitNetwork)
			}
			return ErrSilent
		}

		result.AuthValid = true
		check("network", "pass", "")
		check("auth", "pass", "")
	} else {
		result.LatencyMs = -1
		check("network", "pass", "")
		check("auth", "skip", "no credentials configured; run 'archery-cli auth login'")
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
	if result.AuthValid {
		output.Success("JWT token valid")
	}
	output.Success(fmt.Sprintf("Connected to %s", region.URL))
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
