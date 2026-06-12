package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestUpdateCheck_JSON covers the `update --check` contract against a mock
// GitHub API: read-only, reports current/target versions and availability.
func TestUpdateCheck_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assets := []string{
			"archery-cli-9.9.9-windows-amd64.zip",
			"archery-cli-9.9.9-linux-amd64.tar.gz",
			"archery-cli-9.9.9-linux-arm64.tar.gz",
			"archery-cli-9.9.9-darwin-amd64.tar.gz",
			"archery-cli-9.9.9-darwin-arm64.tar.gz",
			"checksums.txt",
			"checksums.txt.sigstore.json",
		}
		var b strings.Builder
		b.WriteString(`{"tag_name":"v9.9.9","html_url":"https://example.com/rel","assets":[`)
		for i, name := range assets {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"name":"` + name + `","browser_download_url":"https://example.com/` + name + `"}`)
		}
		b.WriteString(`]}`)
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	origAPI := updateGitHubAPI
	origJSON := jsonMode
	origExit := lastExit
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		jsonMode = origJSON
		lastExit = origExit
	})
	updateGitHubAPI = srv.URL
	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	jsonMode = true
	lastExit = 0

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	rootCmd.SetArgs([]string{"update", "--check", "--format", "json"})
	execErr := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = origStdout

	var sb strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, readErr := r.Read(buf)
		sb.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	out := sb.String()

	if execErr != nil && execErr != ErrSilent {
		t.Fatalf("update --check: %v\n%s", execErr, out)
	}
	var env struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); jsonErr != nil {
		t.Fatalf("invalid envelope: %v\n%s", jsonErr, out)
	}
	if !env.OK {
		t.Fatalf("ok=false: %s", out)
	}
	if env.Data["target_version"] != "9.9.9" && env.Data["target_version"] != "v9.9.9" {
		t.Fatalf("target_version = %v, want 9.9.9", env.Data["target_version"])
	}
	if avail, _ := env.Data["update_available"].(bool); !avail {
		t.Fatalf("update_available = %v, want true: %s", env.Data["update_available"], out)
	}
}
