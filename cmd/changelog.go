package cmd

import (
	"fmt"
	"strings"

	archerycli "github.com/fatecannotbealtered/archery-cli"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var changelogContent = archerycli.Changelog

var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Show version changelog",
	Long:  "Display the changelog bundled with this binary. Use --since to show only entries newer than a given version.",
	Args:  cobra.NoArgs,
	RunE:  runChangelog,
}

var changelogSinceFlag string

func init() {
	changelogCmd.Flags().StringVar(&changelogSinceFlag, "since", "", "Show only entries newer than this version (e.g. 1.2.0)")
	rootCmd.AddCommand(changelogCmd)
}

// changelogEntry represents a single version entry in structured form.
type changelogEntry struct {
	Version string              `json:"version"`
	Date    string              `json:"date,omitempty"`
	Changes map[string][]string `json:"changes"`
}

func runChangelog(_ *cobra.Command, _ []string) error {
	entries := parseChangelogEntries(changelogContent)

	if jsonMode {
		result := map[string]any{
			"current_version": version,
			"entries":         entries,
		}
		if changelogSinceFlag != "" {
			result["since"] = changelogSinceFlag
			result["entries"] = filterEntriesSince(entries, changelogSinceFlag)
		}
		output.PrintJSON(result)
		return nil
	}

	// Text mode: print raw markdown
	content := changelogContent
	if changelogSinceFlag != "" {
		content = filterChangelogSince(content, changelogSinceFlag)
	}
	fmt.Println(content)
	return nil
}

// parseChangelogEntries parses CHANGELOG.md into structured entries.
// Expects Keep a Changelog format: ## [version] - date, ### Category.
func parseChangelogEntries(content string) []changelogEntry {
	lines := strings.Split(content, "\n")
	var entries []changelogEntry
	var current *changelogEntry
	var currentCategory string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Version header: ## [1.2.0] - 2026-06-07 or ## [1.2.0]
		if strings.HasPrefix(trimmed, "## [") {
			header := strings.TrimPrefix(trimmed, "## [")
			header = strings.TrimSuffix(header, "]")

			version := header
			date := ""
			if idx := strings.Index(header, "] - "); idx >= 0 {
				version = header[:idx]
				date = strings.TrimSpace(header[idx+4:])
			} else if idx := strings.Index(header, " - "); idx >= 0 {
				version = header[:idx]
				date = strings.TrimSpace(header[idx+3:])
			}
			version = strings.TrimPrefix(version, "v")

			entries = append(entries, changelogEntry{
				Version: version,
				Date:    date,
				Changes: map[string][]string{},
			})
			current = &entries[len(entries)-1]
			currentCategory = ""
			continue
		}

		if current == nil {
			continue
		}

		// Category header: ### Added, ### Changed, ### Fixed, etc.
		if strings.HasPrefix(trimmed, "### ") {
			currentCategory = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")))
			continue
		}

		// Change item: - description
		if currentCategory != "" && strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if item != "" {
				current.Changes[currentCategory] = append(current.Changes[currentCategory], item)
			}
		}
	}

	return entries
}

// filterEntriesSince returns only entries with versions strictly newer than sinceVersion.
func filterEntriesSince(entries []changelogEntry, sinceVersion string) []changelogEntry {
	since := normalizeVersion(sinceVersion)
	result := []changelogEntry{}
	for _, e := range entries {
		if compareVersions(normalizeVersion(e.Version), since) > 0 {
			result = append(result, e)
		}
	}
	return result
}

// filterChangelogSince returns only the changelog entries newer than the given version.
// It looks for "## " headers containing version numbers and stops when it reaches the target version.
// This is the text-mode fallback; JSON mode uses structured parsing.
func filterChangelogSince(content, sinceVersion string) string {
	since := strings.TrimPrefix(strings.TrimSpace(sinceVersion), "v")
	lines := strings.Split(content, "\n")
	var result []string
	included := false

	for _, line := range lines {
		if strings.HasPrefix(line, "## [") || strings.HasPrefix(line, "## ") {
			// Extract version from header like "## [1.2.0] - 2024-01-01" or "## v1.2.0"
			header := strings.TrimPrefix(line, "## [")
			header = strings.TrimPrefix(header, "## ")
			header = strings.TrimSpace(header)
			// Get the version part (before any date or parenthetical)
			version := header
			if idx := strings.Index(header, "]"); idx >= 0 {
				version = header[:idx]
			}
			if idx := strings.IndexAny(version, " ("); idx >= 0 {
				version = version[:idx]
			}
			version = strings.TrimPrefix(version, "v")
			version = strings.TrimSpace(version)

			// Only include entries strictly newer than since
			if compareVersions(normalizeVersion(version), normalizeVersion(since)) > 0 {
				included = true
				result = append(result, line)
			} else {
				included = false
			}
			continue
		}

		if included {
			result = append(result, line)
		}
	}

	if len(result) == 0 {
		return ""
	}
	return strings.Join(result, "\n")
}
