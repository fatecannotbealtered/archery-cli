package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
)

// enableNoticeCacheForTest lets the cache read/write path run under the `.test`
// binary and points HOME/USERPROFILE at a temp dir.
func enableNoticeCacheForTest(t *testing.T) {
	t.Helper()
	orig := updateNoticeTestModeDisabled
	updateNoticeTestModeDisabled = func() bool { return false }
	t.Cleanup(func() { updateNoticeTestModeDisabled = orig })
	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func seedUpdateNoticeCache(t *testing.T) {
	t.Helper()
	writeUpdateNoticeCache([]updateNotice{{
		Type:               "update_available",
		Severity:           "info",
		Message:            "archery-cli 9.9.9 is available (current 1.0.0)",
		CurrentVersion:     "1.0.0",
		LatestVersion:      "9.9.9",
		UpdateAvailable:    true,
		RecommendedCommand: "archery-cli update --dry-run --compact",
		CheckedAt:          "set-on-write",
		Source:             "update_check",
		NextSteps:          []string{"run the recommended command"},
	}})
}

// runChangelogJSON runs the no-network `changelog` command in JSON mode and
// returns the raw envelope. changelog touches no upstream, so any meta.notices
// must come from the local cache, proving the piggyback path does no network I/O.
func runChangelogJSON(t *testing.T) string {
	t.Helper()
	origJSON := jsonMode
	origExit := lastExit
	t.Cleanup(func() {
		jsonMode = origJSON
		lastExit = origExit
	})
	jsonMode = true
	lastExit = 0

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 64*1024)
		for {
			n, readErr := r.Read(buf)
			sb.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- sb.String()
	}()

	rootCmd.SetArgs([]string{"changelog", "--format", "json"})
	execErr := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = origStdout
	out := <-done
	if execErr != nil && execErr != ErrSilent {
		t.Fatalf("changelog: %v", execErr)
	}
	return strings.TrimSpace(out)
}

type metaProbe struct {
	OK   bool `json:"ok"`
	Meta struct {
		Notices []map[string]any `json:"notices"`
	} `json:"meta"`
}

// TestMetaNotices_PresentWhenCached asserts the cached notice rides on an
// arbitrary (non-update) command's meta.notices, sourced from cache only.
func TestMetaNotices_PresentWhenCached(t *testing.T) {
	enableNoticeCacheForTest(t)
	seedUpdateNoticeCache(t)

	out := runChangelogJSON(t)
	var env metaProbe
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid envelope: %v\n%s", err, out)
	}
	if len(env.Meta.Notices) != 1 {
		t.Fatalf("meta.notices len = %d, want 1: %s", len(env.Meta.Notices), out)
	}
	n := env.Meta.Notices[0]
	if n["type"] != "update_available" {
		t.Fatalf("meta.notices[0].type = %v, want update_available", n["type"])
	}
	if n["source"] != "cache" {
		t.Fatalf("meta.notices[0].source = %v, want cache (read-only)", n["source"])
	}
}

// TestMetaNotices_AbsentWhenCacheEmpty asserts meta.notices is omitted entirely
// when the cache holds nothing.
func TestMetaNotices_AbsentWhenCacheEmpty(t *testing.T) {
	enableNoticeCacheForTest(t)
	// No cache seeded.

	out := runChangelogJSON(t)
	if strings.Contains(out, "\"notices\"") {
		t.Fatalf("meta.notices should be absent for empty cache: %s", out)
	}
	var env metaProbe
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid envelope: %v\n%s", err, out)
	}
	if len(env.Meta.Notices) != 0 {
		t.Fatalf("meta.notices = %v, want absent", env.Meta.Notices)
	}
}

func TestUpdateNoticeAutoDisabledDetectsWindowsGoTestBinary(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	orig := updateNoticeTestModeDisabled
	t.Cleanup(func() { updateNoticeTestModeDisabled = orig })
	os.Args = []string{`C:\Users\me\AppData\Local\Temp\cmd.test.exe`}

	if !updateNoticeTestModeDisabled() {
		t.Fatal("Windows Go test binary must not write the real update notice cache")
	}
}

// TestMetaNotices_NoNetwork asserts the piggyback path makes zero network calls:
// any GitHub access would go through updateGitHubAPI, so a poisoned base URL must
// not be touched while the cache is consulted.
func TestMetaNotices_NoNetwork(t *testing.T) {
	enableNoticeCacheForTest(t)
	seedUpdateNoticeCache(t)

	origAPI := updateGitHubAPI
	t.Cleanup(func() { updateGitHubAPI = origAPI })
	// A host that fails fast if dialed; the cache path must never reach it.
	updateGitHubAPI = "http://127.0.0.1:0"

	notices := cachedUpdateNoticesAsAny()
	if len(notices) != 1 {
		t.Fatalf("cachedUpdateNoticesAsAny len = %d, want 1 (cache-only)", len(notices))
	}
}

// TestUpdateNoticesProviderWired asserts root init wired the output hook.
func TestUpdateNoticesProviderWired(t *testing.T) {
	if output.UpdateNoticesProvider == nil {
		t.Fatal("output.UpdateNoticesProvider is nil; root init must wire it")
	}
}

// TestUpdateNoticeSeverity covers the changelog-delta severity grading.
func TestUpdateNoticeSeverity(t *testing.T) {
	origCL := changelogContent
	t.Cleanup(func() { changelogContent = origCL })

	cases := []struct {
		name      string
		changelog string
		current   string
		latest    string
		want      string
	}{
		{
			name: "security entry in delta -> warning",
			changelog: "# Changelog\n\n" +
				"## [1.1.0] - 2026-06-21\n\n### Security\n\n- patched an auth bypass\n\n" +
				"## [1.0.0] - 2026-06-01\n\n### Added\n\n- initial\n",
			current: "1.0.0",
			latest:  "1.1.0",
			want:    "warning",
		},
		{
			name: "major bump -> warning",
			changelog: "# Changelog\n\n" +
				"## [2.0.0] - 2026-06-21\n\n### Changed\n\n- rewrote everything\n\n" +
				"## [1.0.0] - 2026-06-01\n\n### Added\n\n- initial\n",
			current: "1.0.0",
			latest:  "2.0.0",
			want:    "warning",
		},
		{
			name: "plain patch -> info",
			changelog: "# Changelog\n\n" +
				"## [1.0.1] - 2026-06-21\n\n### Fixed\n\n- a small bug\n\n" +
				"## [1.0.0] - 2026-06-01\n\n### Added\n\n- initial\n",
			current: "1.0.0",
			latest:  "1.0.1",
			want:    "info",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changelogContent = tc.changelog
			if got := updateNoticeSeverity(tc.current, tc.latest); got != tc.want {
				t.Fatalf("severity(%s -> %s) = %q, want %q", tc.current, tc.latest, got, tc.want)
			}
		})
	}
}
