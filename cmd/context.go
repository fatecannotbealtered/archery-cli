package cmd

import (
	"fmt"
	"os"

	"github.com/fatecannotbealtered/archery-cli/internal/config"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Show current region, account, and configuration",
	Long: `Show a composite snapshot of the current Archery authentication state and configuration.

Useful as the FIRST command in an AI Agent workflow to bootstrap region, URL,
and authenticated user before issuing other commands.

By default exits with code 4 when Archery is not authenticated (use --no-strict to always exit 0).`,
	RunE: runContext,
}

var contextStrict bool

func init() {
	contextCmd.Flags().BoolVar(&contextStrict, "strict", true, "Exit with code 4 when not authenticated")
	contextCmd.Flags().Bool("no-strict", false, "Do not fail when unauthenticated (overrides --strict)")
	rootCmd.AddCommand(contextCmd)
}

// contextRegion is the JSON shape for a region in the context output.
type contextRegion struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Username  string `json:"username,omitempty"`
	HasTokens bool   `json:"hasTokens"`
	IsActive  bool   `json:"isActive"`
}

// contextCredentials reports credential status without leaking secrets.
type contextCredentials struct {
	Configured bool `json:"configured"`
	HasTokens  bool `json:"hasTokens,omitempty"`
	HasBasic   bool `json:"hasBasic,omitempty"`
}

// contextResult is the top-level JSON envelope.
type contextResult struct {
	Version       string             `json:"version"`
	Env           string             `json:"env"`
	DefaultRegion string             `json:"defaultRegion"`
	ActiveRegion  string             `json:"activeRegion"`
	Regions       []contextRegion    `json:"regions"`
	ConfigPath    string             `json:"configPath"`
	Credentials   contextCredentials `json:"credentials"`
	Notices       []updateNotice     `json:"notices,omitempty"`
}

func runContext(cmd *cobra.Command, _ []string) error {
	noStrict, _ := cmd.Flags().GetBool("no-strict")
	if noStrict {
		contextStrict = false
	}

	cfg, err := config.Load()
	if err != nil {
		result := &contextResult{
			Version:     version,
			Env:         "local",
			ConfigPath:  config.FilePath(),
			Credentials: contextCredentials{Configured: false},
			Notices:     readCachedUpdateNotices(),
		}
		return renderContext(result, true)
	}

	activeRegion := activeRegionName(cfg)
	result := &contextResult{
		Version:       version,
		Env:           "local",
		DefaultRegion: cfg.DefaultRegion,
		ActiveRegion:  activeRegion,
		ConfigPath:    config.FilePath(),
		Notices:       readCachedUpdateNotices(),
	}

	for name, r := range cfg.Regions {
		result.Regions = append(result.Regions, contextRegion{
			Name:      name,
			URL:       r.URL,
			Username:  r.Username,
			HasTokens: r.AccessToken != "",
			IsActive:  name == activeRegion,
		})
	}

	// Check if the active region is properly configured
	if region, ok := cfg.Regions[activeRegion]; ok {
		hasTokens := region.AccessToken != ""
		hasBasic := region.Username != "" && region.Password != ""
		result.Credentials = contextCredentials{
			Configured: region.URL != "" && (hasTokens || hasBasic),
			HasTokens:  hasTokens,
			HasBasic:   hasBasic,
		}
		if region.URL == "" || (region.AccessToken == "" && (region.Username == "" || region.Password == "")) {
			return renderContext(result, true)
		}
	} else {
		result.Credentials = contextCredentials{Configured: false}
		return renderContext(result, true)
	}

	return renderContext(result, false)
}

func renderContext(r *contextResult, unauthenticated bool) error {
	if jsonMode {
		output.PrintJSON(r)
		if unauthenticated && contextStrict {
			setExitCode(ExitAuth)
			return ErrSilent
		}
		return nil
	}

	fmt.Println()
	output.Bold("  archery-cli Context")
	output.Gray("  ────────────────────────────────────────")
	fmt.Println()

	output.Bold("  Configuration")
	output.Gray(fmt.Sprintf("    Config:  %s", r.ConfigPath))
	output.Gray(fmt.Sprintf("    Default: %s", r.DefaultRegion))
	output.Gray(fmt.Sprintf("    Active:  %s", r.ActiveRegion))
	fmt.Println()

	output.Bold("  Regions")
	if len(r.Regions) == 0 {
		output.Gray("    No regions configured (run 'archery-cli auth login')")
	} else {
		for _, reg := range r.Regions {
			mark := ""
			if reg.IsActive {
				mark = " (active)"
			}
			tokensStatus := "no tokens"
			if reg.HasTokens {
				tokensStatus = "tokens cached"
			}
			userInfo := ""
			if reg.Username != "" {
				userInfo = ", user=" + reg.Username
			}
			output.Gray(fmt.Sprintf("    %s%s: %s [%s%s]", reg.Name, mark, reg.URL, tokensStatus, userInfo))
		}
	}
	fmt.Println()

	if unauthenticated && !jsonMode {
		if contextStrict {
			printUpdateNoticeHint(os.Stdout, r.Notices)
			output.Warn("Not authenticated. Run 'archery-cli auth login'.")
			setExitCode(ExitAuth)
			return ErrSilent
		}
		output.Warn("Not fully authenticated for active region.")
	}
	printUpdateNoticeHint(os.Stdout, r.Notices)

	return nil
}
