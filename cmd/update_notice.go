package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	updateNoticeCacheTTL       = 24 * time.Hour
	updateNoticeRefreshTimeout = 2 * time.Second
	updateNoticeEnvOptOut      = "ARCHERY_CLI_NO_UPDATE_CHECK"
)

type updateNotice struct {
	Type               string   `json:"type"`
	Severity           string   `json:"severity"`
	Message            string   `json:"message"`
	CurrentVersion     string   `json:"current_version"`
	LatestVersion      string   `json:"latest_version"`
	UpdateAvailable    bool     `json:"update_available"`
	InstallMethod      string   `json:"install_method,omitempty"`
	RecommendedCommand string   `json:"recommended_command"`
	ReleaseURL         string   `json:"release_url,omitempty"`
	CheckedAt          string   `json:"checked_at"`
	Source             string   `json:"source"`
	NextSteps          []string `json:"next_steps"`
}

type updateNoticeCache struct {
	CheckedAt string         `json:"checked_at"`
	Notices   []updateNotice `json:"notices,omitempty"`
}

func installUpdateNoticeHelp(root *cobra.Command) {
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if cmd.Long != "" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), cmd.Long)
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		} else if cmd.Short != "" {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), cmd.Short)
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		}
		_, _ = fmt.Fprint(cmd.OutOrStdout(), cmd.UsageString())
		printUpdateNoticeHint(cmd.OutOrStdout(), readCachedUpdateNotices())
	})
}

func refreshUpdateNotices(ctx context.Context, source string) []updateNotice {
	if updateNoticeAutoDisabled() {
		return nil
	}
	refreshCtx, cancel := context.WithTimeout(ctx, updateNoticeRefreshTimeout)
	defer cancel()

	release, err := fetchUpdateRelease(refreshCtx, "")
	if err != nil {
		return readCachedUpdateNotices()
	}
	plan, err := buildUpdatePlan(release, version)
	if err != nil {
		return readCachedUpdateNotices()
	}
	if exe, err := updateExecutable(); err == nil {
		plan.InstallMethod = detectInstallMethod(exe)
	}
	notices := updateNoticesFromPlan(plan, source)
	writeUpdateNoticeCache(notices)
	return notices
}

func updateNoticesFromPlan(plan updatePlan, source string) []updateNotice {
	current := normalizeVersion(plan.CurrentVersion)
	latest := normalizeVersion(plan.TargetVersion)
	if !plan.UpdateAvailable {
		return nil
	}
	command := updateNoticeRecommendedCommand(plan.InstallMethod, latest)
	notice := updateNotice{
		Type:               "update_available",
		Severity:           updateNoticeSeverity(current, latest),
		CurrentVersion:     current,
		LatestVersion:      latest,
		UpdateAvailable:    true,
		InstallMethod:      plan.InstallMethod,
		RecommendedCommand: command,
		ReleaseURL:         plan.ReleaseURL,
		CheckedAt:          time.Now().UTC().Format(time.RFC3339),
		Source:             source,
		NextSteps: []string{
			"run the recommended command",
			"ask the user before confirming the local self-update",
			"after update, run archery-cli changelog --since " + current + " --compact",
			"refresh archery-cli reference --compact before using new behavior",
		},
	}
	notice.Message = fmt.Sprintf("archery-cli %s is available (current %s)", latest, current)
	return []updateNotice{notice}
}

// updateNoticeSeverity grades the update notice from the embedded CHANGELOG
// delta between the running version (current) and the latest. It returns
// "warning" when any version in the delta carries a non-empty `security`
// category, OR when latest crosses a major version; otherwise "info".
// `critical` is reserved and never emitted here (CLI-SPEC §14).
func updateNoticeSeverity(current, latest string) string {
	if cur, curOK := parseSemver(current); curOK {
		if lat, latOK := parseSemver(latest); latOK && lat.nums[0] > cur.nums[0] {
			return "warning"
		}
	}
	entries := filterEntriesSince(parseChangelogEntries(changelogContent), current)
	for _, e := range entries {
		if len(e.Changes["security"]) > 0 {
			return "warning"
		}
	}
	return "info"
}

func updateNoticeRecommendedCommand(installMethod, latest string) string {
	switch strings.ToLower(strings.TrimSpace(installMethod)) {
	case "npm", "pip":
		return updateInstallCommand(installMethod, latest)
	default:
		return "archery-cli update --dry-run --compact"
	}
}

func readCachedUpdateNotices() []updateNotice {
	if updateNoticeAutoDisabled() {
		return nil
	}
	path, err := updateNoticeCachePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache updateNoticeCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	checkedAt, err := time.Parse(time.RFC3339, cache.CheckedAt)
	if err != nil || time.Since(checkedAt) > updateNoticeCacheTTL {
		return nil
	}
	notices := make([]updateNotice, 0, len(cache.Notices))
	for _, notice := range cache.Notices {
		if notice.Type != "update_available" || !notice.UpdateAvailable {
			continue
		}
		notice.Source = "cache"
		notices = append(notices, notice)
	}
	return notices
}

func writeUpdateNoticeCache(notices []updateNotice) {
	if updateNoticeAutoDisabled() {
		return
	}
	path, err := updateNoticeCachePath()
	if err != nil {
		return
	}
	if len(notices) == 0 {
		_ = os.Remove(path)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	cache := updateNoticeCache{CheckedAt: checkedAt, Notices: notices}
	for i := range cache.Notices {
		cache.Notices[i].CheckedAt = checkedAt
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func updateNoticeCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", err
	}
	return filepath.Join(home, "."+updateBinaryName, "update-check.json"), nil
}

func updateNoticeDisabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(updateNoticeEnvOptOut)))
	return value == "1" || value == "true" || value == "yes"
}

// updateNoticeTestModeDisabled reports whether the process is a test binary, in
// which case the auto update-notice cache I/O is disabled. Overridable so cache
// tests can exercise the read/write path under `go test`.
var updateNoticeTestModeDisabled = func() bool {
	return strings.HasSuffix(os.Args[0], ".test")
}

func updateNoticeAutoDisabled() bool {
	return updateNoticeDisabled() || updateNoticeTestModeDisabled()
}

// cachedUpdateNoticesAsAny reads the local update-notice cache and returns the
// notices as a []any for output.Meta.Notices. Read-only and TTL-bounded; never
// performs network I/O. Returns nil when the cache has nothing to report.
func cachedUpdateNoticesAsAny() []any {
	notices := readCachedUpdateNotices()
	if len(notices) == 0 {
		return nil
	}
	out := make([]any, len(notices))
	for i := range notices {
		out[i] = notices[i]
	}
	return out
}

func printUpdateNoticeHint(w io.Writer, notices []updateNotice) {
	if len(notices) == 0 {
		return
	}
	notice := notices[0]
	_, _ = fmt.Fprintf(w, "\nUpdate available: archery-cli %s -> %s. Run: %s\n", notice.CurrentVersion, notice.LatestVersion, notice.RecommendedCommand)
}
