package cmd

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/config"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// test hooks for interactive login
var (
	stdinIsTerminalForAuth = func() bool { return term.IsTerminal(int(syscall.Stdin)) }
	readPasswordForAuth    = func() ([]byte, error) { return term.ReadPassword(int(syscall.Stdin)) }
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Configure Archery authentication",
	Long: `Configure Archery URL, credentials, and JWT tokens.

Credentials are resolved with this precedence (highest first):
  ARCHERY_CLI_URL / ARCHERY_CLI_USERNAME / ARCHERY_CLI_PASSWORD  (env vars)
  Active region in ~/.archery-cli/config.json                     (saved by 'archery-cli auth login')`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Archery and cache JWT tokens",
	Long: `Authenticate using username and password, obtain JWT access/refresh tokens,
store tokens in the OS keyring, and save only non-secret region metadata to config.

Examples:
  archery-cli auth login --url https://archery.example.com --username admin --password secret --region prod --dry-run
  archery-cli auth login --url https://archery.example.com --username admin --password secret --region prod --confirm <confirm_token>
  archery-cli auth login  # interactive mode`,
	RunE: runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear cached JWT tokens for a region",
	RunE:  runAuthLogout,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication state",
	RunE:  runAuthStatus,
}

var (
	authLoginUsernameFlag string
	authLoginPasswordFlag string
	authLoginRegionFlag   string
	authLoginURLFlag      string
)

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)

	authLoginCmd.Flags().StringVar(&authLoginUsernameFlag, "username", "", "Archery username")
	authLoginCmd.Flags().StringVar(&authLoginPasswordFlag, "password", "", "Archery password")
	authLoginCmd.Flags().StringVar(&authLoginRegionFlag, "region", "", "Region name to save credentials under")
	authLoginCmd.Flags().StringVar(&authLoginURLFlag, "url", "", "Archery URL")

	markWrite(authLoginCmd)
	markWrite(authLogoutCmd)
	markRiskLevel(authLoginCmd, "medium")
	markRiskLevel(authLogoutCmd, "low")
	markRiskLevel(authStatusCmd, "low")
}

func runAuthLogin(_ *cobra.Command, _ []string) error {
	// Determine region name
	cfg, err := config.Load()
	if err != nil {
		return failWithCode("reading config: "+err.Error(), ExitAuth, output.E_CONFIG)
	}
	regionName := authLoginRegionFlag
	if regionName == "" {
		regionName = regionFlag
	}
	if regionName == "" && cfg != nil {
		regionName = config.ActiveRegion(cfg)
	}
	if regionName == "" {
		regionName = "default"
	}

	// Determine credentials
	username := authLoginUsernameFlag
	password := authLoginPasswordFlag
	regionURL := strings.TrimSpace(authLoginURLFlag)
	if regionURL == "" && cfg != nil {
		if r, ok := cfg.Regions[regionName]; ok {
			regionURL = r.URL
		}
	}
	if regionURL == "" {
		regionURL = firstNonEmpty(os.Getenv("ARCHERY_CLI_URL"))
	}

	if jsonMode {
		if regionURL == "" || username == "" || password == "" {
			return failArg("auth login requires --url, --username, and --password in json mode; use --format text for interactive login")
		}
		if err := validateAuthURL(regionURL); err != nil {
			return err
		}
		return doAuthLogin(cfg, regionName, regionURL, username, password)
	}

	// Interactive mode
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	output.Bold("  archery-cli Login")
	output.Gray("  ────────────────────────────────────────")
	fmt.Println()

	if regionURL == "" {
		fmt.Print("  Archery URL (e.g. https://archery.example.com): ")
		regionURL, _ = reader.ReadString('\n')
		regionURL = strings.TrimSpace(regionURL)
	}
	if regionURL == "" {
		return failArg("URL is required")
	}
	if err := validateAuthURL(regionURL); err != nil {
		return err
	}

	if username == "" {
		fmt.Print("  Username: ")
		username, _ = reader.ReadString('\n')
		username = strings.TrimSpace(username)
	}
	if username == "" {
		return failArg("username cannot be empty")
	}

	if password == "" {
		fmt.Print("  Password: ")
		var passwordBytes []byte
		var err error
		if stdinIsTerminalForAuth() {
			passwordBytes, err = readPasswordForAuth()
			fmt.Println()
			if err != nil {
				return failWithCode("failed to read password: "+err.Error(), ExitNetwork, output.E_NETWORK)
			}
		} else {
			line, _ := reader.ReadString('\n')
			passwordBytes = []byte(strings.TrimSpace(line))
		}
		password = strings.TrimSpace(string(passwordBytes))
	}
	if password == "" {
		return failArg("password cannot be empty")
	}

	return doAuthLogin(cfg, regionName, regionURL, username, password)
}

func doAuthLogin(cfg *config.Config, regionName, regionURL, username, password string) error {
	if cfg == nil {
		cfg = &config.Config{Regions: make(map[string]config.RegionConfig)}
	}
	if cfg.Regions == nil {
		cfg.Regions = make(map[string]config.RegionConfig)
	}

	// Resolve transport mode: --mode flag wins, else the region's saved mode,
	// else the default (session).
	mode := effectiveMode(cfg.Regions[regionName])

	detail := map[string]any{
		"region":   regionName,
		"url":      regionURL,
		"username": username,
		"mode":     mode,
	}
	confirmPayload := map[string]any{
		"region":   regionName,
		"url":      regionURL,
		"username": username,
		"password": password,
		"mode":     mode,
	}
	action := "log in via session cookie and cache it"
	if mode == config.ModeJWT {
		action = "log in and cache JWT tokens"
	}
	if markDryRunOrConfirmWithPayload(action, detail, confirmPayload) {
		return nil
	}
	if !config.KeyringAvailable() {
		return failWithCode("OS credential store unavailable; cannot persist credentials securely. Enable the OS keyring or use ARCHERY_CLI_URL, ARCHERY_CLI_USERNAME, and ARCHERY_CLI_PASSWORD for one-shot commands.", ExitAuth, output.E_CONFIG)
	}

	if !jsonMode {
		output.Gray("  Authenticating...")
	}

	client := api.NewClient(regionURL)
	client.SetMode(mode)
	// Pass any 2FA code through to the session login handshake.
	client.SetOTP(effectiveOTP())

	region := cfg.Regions[regionName]
	region.URL = regionURL
	region.Username = username
	region.Password = ""
	region.Mode = mode

	if mode == config.ModeJWT {
		accessToken, refreshToken, err := client.Auth.Login(apiCtx(), username, password)
		if err != nil {
			return handleAPIError(err)
		}
		region.AccessToken = accessToken
		region.RefreshToken = refreshToken
		region.SessionID = ""
		region.CSRFToken = ""
	} else {
		sessionID, csrfToken, err := client.Auth.LoginWithSession(apiCtx(), username, password)
		if err != nil {
			return handleAPIError(err)
		}
		region.SessionID = sessionID
		region.CSRFToken = csrfToken
		region.AccessToken = ""
		region.RefreshToken = ""
	}

	cfg.Regions[regionName] = region
	if cfg.DefaultRegion == "" {
		cfg.DefaultRegion = regionName
	}
	if err := config.Save(cfg); err != nil {
		return failWithCode("failed to save credentials: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	cachedLabel := "session cookie cached"
	if mode == config.ModeJWT {
		cachedLabel = "JWT tokens cached"
	}

	if jsonMode {
		output.PrintJSON(map[string]any{
			"status":  "ok",
			"region":  regionName,
			"url":     regionURL,
			"mode":    mode,
			"message": cachedLabel + " successfully",
		})
		return nil
	}

	fmt.Println()
	output.Success(fmt.Sprintf("Logged in to %s (region: %s, mode: %s)", regionURL, regionName, mode))
	storeLabel := config.CredentialStoreLabel(cfg)
	if storeLabel == "" {
		storeLabel = config.CredentialStoreNone
	}
	output.Info(fmt.Sprintf("%s (%s)", cachedLabel, storeLabel))
	fmt.Println()
	output.Gray("  Try: archery-cli doctor")
	fmt.Println()
	return nil
}

func runAuthLogout(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return failWithCode("reading config: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	regionName := activeRegionName(cfg)

	region, ok := cfg.Regions[regionName]
	if !ok {
		return failNotFound(fmt.Sprintf("region %q not found", regionName))
	}

	if markDryRunOrConfirm("clear cached tokens", map[string]any{"region": regionName, "username": region.Username}) {
		return nil
	}

	// Clear both JWT tokens and session cookies from keyring + in-memory fields.
	ts := config.NewTokenStore()
	_ = ts.DeleteTokens(regionName, region.Username)
	_ = ts.DeleteSession(regionName, region.Username)

	region.AccessToken = ""
	region.RefreshToken = ""
	region.SessionID = ""
	region.CSRFToken = ""
	cfg.Regions[regionName] = region

	if err := config.Save(cfg); err != nil {
		return failWithCode("failed to save config: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	if jsonMode {
		output.PrintJSON(map[string]any{
			"status": "logged_out",
			"region": regionName,
		})
		return nil
	}

	output.Success(fmt.Sprintf("Logged out (region: %s). Tokens cleared.", regionName))
	return nil
}

func runAuthStatus(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return failWithCode("reading config: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	regionName := activeRegionName(cfg)

	type statusResult struct {
		Configured bool   `json:"configured"`
		Region     string `json:"region"`
		Mode       string `json:"mode,omitempty"`
		URL        string `json:"url,omitempty"`
		HasTokens  bool   `json:"hasTokens"`
		HasSession bool   `json:"hasSession"`
		Username   string `json:"username,omitempty"`
	}

	result := statusResult{
		Region: regionName,
	}

	if region, ok := cfg.Regions[regionName]; ok {
		result.Configured = region.URL != ""
		result.Mode = effectiveMode(region)
		result.URL = region.URL
		result.HasTokens = region.AccessToken != ""
		result.HasSession = region.SessionID != ""
		result.Username = region.Username
	}

	if jsonMode {
		output.PrintJSON(result)
		if !result.Configured {
			setExitCode(ExitAuth)
			return ErrSilent
		}
		return nil
	}

	fmt.Println()
	output.Bold("  archery-cli Auth Status")
	output.Gray("  ────────────────────────────────────────")
	fmt.Println()
	if result.Configured {
		credStatus := "not cached"
		if result.Mode == config.ModeJWT {
			if result.HasTokens {
				credStatus = "tokens cached"
			}
		} else if result.HasSession {
			credStatus = "session cached"
		}
		output.Success(fmt.Sprintf("Configured (region=%s, mode=%s, url=%s, %s)", result.Region, result.Mode, result.URL, credStatus))
		if result.Username != "" {
			output.Gray(fmt.Sprintf("  Username: %s", result.Username))
		}
	} else {
		output.Warn(fmt.Sprintf("Not configured for region %q. Run 'archery-cli auth login'.", regionName))
		setExitCode(ExitAuth)
		return ErrSilent
	}
	return nil
}

// firstNonEmpty returns the first non-empty trimmed string from candidates.
func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if v := strings.TrimSpace(c); v != "" {
			return v
		}
	}
	return ""
}

func validateAuthURL(regionURL string) error {
	if !strings.HasPrefix(regionURL, "https://") && !strings.HasPrefix(regionURL, "http://") {
		return failArg("URL must start with https:// (or http:// for local development)")
	}
	parsed, err := url.Parse(regionURL)
	if err != nil || parsed.Host == "" {
		return failArg("URL must be a valid Archery base URL")
	}
	if parsed.Scheme == "http" {
		host := strings.ToLower(parsed.Hostname())
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return failArg("http:// is only allowed for loopback hosts (localhost, 127.0.0.1, [::1])")
		}
	}
	return nil
}
