package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
)

// updateMockReleaseServer serves a GitHub-style release response for the given
// version, advertising every platform archive plus checksums and the signature
// bundle. It is the single mock used by the self-update contract tests.
func updateMockReleaseServer(t *testing.T, ver string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assets := []string{
			"archery-cli-" + ver + "-windows-amd64.zip",
			"archery-cli-" + ver + "-linux-amd64.tar.gz",
			"archery-cli-" + ver + "-linux-arm64.tar.gz",
			"archery-cli-" + ver + "-darwin-amd64.tar.gz",
			"archery-cli-" + ver + "-darwin-arm64.tar.gz",
			"checksums.txt",
			"checksums.txt.sigstore.json",
		}
		var b strings.Builder
		b.WriteString(`{"tag_name":"v` + ver + `","html_url":"https://example.com/rel","assets":[`)
		for i, name := range assets {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"name":"` + name + `","browser_download_url":"https://example.com/` + name + `"}`)
		}
		b.WriteString(`]}`)
		_, _ = w.Write([]byte(b.String()))
	}))
}

// runUpdateCapture runs `update` with the given args against a mock release
// server and returns the parsed JSON envelope and the resulting exit code.
func runUpdateCapture(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	origExit := lastExit
	origJSON := jsonMode
	origDryRun := dryRun
	lastExit = 0
	jsonMode = true
	dryRun = false
	t.Cleanup(func() {
		jsonMode = origJSON
		lastExit = origExit
		dryRun = origDryRun
	})

	// Cobra keeps a bool flag's value across Execute() calls when the flag is
	// absent from the new args, so reset update's leaf flags to defaults to keep
	// these tests independent of prior runs (e.g. a previous `update --check`).
	_ = updateCmd.Flags().Set("check", "false")
	_ = updateCmd.Flags().Set("reinstall", "false")
	_ = updateCmd.Flags().Set("target-version", "")

	rootCmd.SetArgs(append([]string{"update", "--format", "json"}, args...))
	_ = rootCmd.Execute()
	exit := lastExit

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
	var env map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(sb.String())), &env); err != nil {
		t.Fatalf("invalid envelope: %v\n%s", err, sb.String())
	}
	return env, exit
}

func envData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, _ := env["data"].(map[string]any)
	if data == nil {
		t.Fatalf("envelope missing data: %v", env)
	}
	return data
}

func envError(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	e, _ := env["error"].(map[string]any)
	if e == nil {
		t.Fatalf("envelope missing error: %v", env)
	}
	return e
}

// TestUpdate_BareExecutesWithoutToken proves a bare `update` runs the whole flow
// in one call with no confirm token: already-on-latest is an idempotent no-op
// success.
func TestUpdate_BareExecutesWithoutToken(t *testing.T) {
	srv := updateMockReleaseServer(t, version)
	defer srv.Close()
	origAPI := updateGitHubAPI
	t.Cleanup(func() { updateGitHubAPI = origAPI })
	updateGitHubAPI = srv.URL
	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("bare update ok=false: %v", env)
	}
	if exit != ExitOK {
		t.Fatalf("exit = %d, want 0", exit)
	}
	data := envData(t, env)
	if data["status"] != "up_to_date" {
		t.Fatalf("status = %v, want up_to_date", data["status"])
	}
	if _, ok := env["confirm_token"]; ok {
		t.Fatal("bare update must not emit a confirm_token")
	}
}

// TestUpdate_DryRunIssuesNoToken proves `update --dry-run` is a read-only
// preview that issues NO confirm_token and NO expires_at.
func TestUpdate_DryRunIssuesNoToken(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()
	origAPI := updateGitHubAPI
	t.Cleanup(func() { updateGitHubAPI = origAPI })
	updateGitHubAPI = srv.URL
	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t, "--dry-run")
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("dry-run ok=false: %v", env)
	}
	if exit != ExitOK {
		t.Fatalf("exit = %d, want 0", exit)
	}
	data := envData(t, env)
	if data["status"] != "dry_run" {
		t.Fatalf("status = %v, want dry_run", data["status"])
	}
	for _, banned := range []string{"confirm_token", "expires_at"} {
		if _, ok := data[banned]; ok {
			t.Fatalf("dry-run must not emit %q", banned)
		}
	}
}

// TestUpdate_IntegrityFailureNonRetryable proves a signature failure surfaces
// E_INTEGRITY, exit 1, retryable:false, with the verify_signature stage.
func TestUpdate_IntegrityFailureNonRetryable(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()

	origAPI := updateGitHubAPI
	origVerify := updateVerifySignature
	origExec := updateExecutable
	origDownload := updateDownloadHook
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateVerifySignature = origVerify
		updateExecutable = origExec
		updateDownloadHook = origDownload
	})
	updateGitHubAPI = srv.URL
	exe := t.TempDir() + "/archery-cli"
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	updateExecutable = func() (string, error) { return exe, nil }
	// Downloads succeed (archive, checksums, signature bundle all no-op); the
	// signature verification itself rejects the release.
	updateDownloadHook = func(context.Context, string, string) error { return nil }
	updateVerifySignature = func(_, _, _ string) error { return errBatch("certificate identity mismatch") }

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if ok, _ := env["ok"].(bool); ok {
		t.Fatalf("integrity failure must not be ok: %v", env)
	}
	if exit != ExitError {
		t.Fatalf("exit = %d, want 1", exit)
	}
	e := envError(t, env)
	if e["code"] != "E_INTEGRITY" {
		t.Fatalf("code = %v, want E_INTEGRITY", e["code"])
	}
	if retry, _ := e["retryable"].(bool); retry {
		t.Fatal("E_INTEGRITY must be retryable:false")
	}
	details, _ := e["details"].(map[string]any)
	if details["stage"] != "verify_signature" {
		t.Fatalf("stage = %v, want verify_signature", details["stage"])
	}
	if br, _ := details["binary_replaced"].(bool); br {
		t.Fatal("binary_replaced must be false on integrity failure")
	}
}

// TestUpdate_SkillSyncFailureIsPartialSuccess proves a skill_sync failure after
// a successful binary swap is partial success: ok:false, binary_replaced:true,
// with skill_sync_command, retryable:true.
func TestUpdate_SkillSyncFailureIsPartialSuccess(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()

	origAPI := updateGitHubAPI
	origApply := updateApply
	origSync := updateSkillSync
	origVerify := updateVerifySignature
	origExec := updateExecutable
	origDownload := updateDownloadHook
	origChecksum := updateChecksumHook
	origExtract := updateExtractHook
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateApply = origApply
		updateSkillSync = origSync
		updateVerifySignature = origVerify
		updateExecutable = origExec
		updateDownloadHook = origDownload
		updateChecksumHook = origChecksum
		updateExtractHook = origExtract
	})
	updateGitHubAPI = srv.URL
	updateExecutable = func() (string, error) { return t.TempDir() + "/archery-cli", nil }
	updateDownloadHook = func(context.Context, string, string) error { return nil }
	updateVerifySignature = func(_, _, _ string) error { return nil }
	updateChecksumHook = func(_, _, _ string) error { return nil }
	updateExtractHook = func(_, _, tmpDir string) (string, error) { return tmpDir + "/bin", nil }
	updateApply = func(_, dst string) (updateApplyResult, error) {
		return updateApplyResult{Status: "installed", Path: dst}, nil
	}
	updateSkillSync = func(context.Context, string) error { return errBatch("npx not found") }

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if ok, _ := env["ok"].(bool); ok {
		t.Fatalf("skill_sync failure must be ok:false (partial): %v", env)
	}
	if exit != ExitNetwork {
		t.Fatalf("exit = %d, want 7", exit)
	}
	e := envError(t, env)
	if retry, _ := e["retryable"].(bool); !retry {
		t.Fatal("skill_sync failure must be retryable:true")
	}
	details, _ := e["details"].(map[string]any)
	if details["stage"] != "skill_sync" {
		t.Fatalf("stage = %v, want skill_sync", details["stage"])
	}
	if br, _ := details["binary_replaced"].(bool); !br {
		t.Fatal("binary_replaced must be true after a successful swap")
	}
	if _, ok := details["skill_sync_command"]; !ok {
		t.Fatal("partial success must include skill_sync_command")
	}
}

// TestUpdate_ReplaceFailureIsIO proves a binary replace failure is E_IO (exit 1),
// not the old misclassified E_NETWORK.
func TestUpdate_ReplaceFailureIsIO(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()

	origAPI := updateGitHubAPI
	origApply := updateApply
	origVerify := updateVerifySignature
	origExec := updateExecutable
	origDownload := updateDownloadHook
	origChecksum := updateChecksumHook
	origExtract := updateExtractHook
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateApply = origApply
		updateVerifySignature = origVerify
		updateExecutable = origExec
		updateDownloadHook = origDownload
		updateChecksumHook = origChecksum
		updateExtractHook = origExtract
	})
	updateGitHubAPI = srv.URL
	updateExecutable = func() (string, error) { return t.TempDir() + "/archery-cli", nil }
	updateDownloadHook = func(context.Context, string, string) error { return nil }
	updateVerifySignature = func(_, _, _ string) error { return nil }
	updateChecksumHook = func(_, _, _ string) error { return nil }
	updateExtractHook = func(_, _, tmpDir string) (string, error) { return tmpDir + "/bin", nil }
	updateApply = func(_, _ string) (updateApplyResult, error) {
		return updateApplyResult{}, errBatch("no space left on device")
	}

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if exit != ExitIO {
		t.Fatalf("exit = %d, want 1 (E_IO)", exit)
	}
	e := envError(t, env)
	if e["code"] != "E_IO" {
		t.Fatalf("code = %v, want E_IO", e["code"])
	}
	if retry, _ := e["retryable"].(bool); retry {
		t.Fatal("E_IO must be retryable:false")
	}
	details, _ := e["details"].(map[string]any)
	if details["stage"] != "replace" {
		t.Fatalf("stage = %v, want replace", details["stage"])
	}
	if br, _ := details["binary_replaced"].(bool); br {
		t.Fatal("binary_replaced must be false when the swap fails")
	}
}

// TestUpdate_DiscoverNotFoundClassified proves a discover-stage HTTP failure is
// classified by status (a 404 from the release endpoint -> E_NOT_FOUND, exit 3,
// non-retryable) instead of collapsing every transport failure into E_NETWORK.
func TestUpdate_DiscoverNotFoundClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	origAPI := updateGitHubAPI
	t.Cleanup(func() { updateGitHubAPI = origAPI })
	updateGitHubAPI = srv.URL
	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t, "--target-version", "1.2.3")
	if ok, _ := env["ok"].(bool); ok {
		t.Fatalf("discover 404 must not be ok: %v", env)
	}
	if exit != ExitNotFound {
		t.Fatalf("exit = %d, want 3 (E_NOT_FOUND)", exit)
	}
	e := envError(t, env)
	if e["code"] != "E_NOT_FOUND" {
		t.Fatalf("code = %v, want E_NOT_FOUND", e["code"])
	}
	if retry, _ := e["retryable"].(bool); retry {
		t.Fatal("E_NOT_FOUND must be retryable:false")
	}
	details, _ := e["details"].(map[string]any)
	if details["stage"] != "discover" {
		t.Fatalf("stage = %v, want discover", details["stage"])
	}
}

// TestUpdate_DiscoverServerErrorRetryable proves a 503 from discover stays a
// retryable network class (E_SERVER, exit 7), distinct from the 404 case above.
func TestUpdate_DiscoverServerErrorRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	origAPI := updateGitHubAPI
	t.Cleanup(func() { updateGitHubAPI = origAPI })
	updateGitHubAPI = srv.URL
	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if exit != ExitNetwork {
		t.Fatalf("exit = %d, want 7", exit)
	}
	e := envError(t, env)
	if e["code"] != "E_SERVER" {
		t.Fatalf("code = %v, want E_SERVER", e["code"])
	}
	if retry, _ := e["retryable"].(bool); !retry {
		t.Fatal("E_SERVER must be retryable:true")
	}
}

// TestUpdate_VerifySignatureInterrupt proves a SIGINT-style cancellation during
// signature verification surfaces E_INTERRUPTED (exit 130) with binary_replaced
// false — never an E_INTEGRITY forged-release verdict.
func TestUpdate_VerifySignatureInterrupt(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()

	origAPI := updateGitHubAPI
	origExec := updateExecutable
	origDownload := updateDownloadHook
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateExecutable = origExec
		updateDownloadHook = origDownload
	})
	updateGitHubAPI = srv.URL
	exe := t.TempDir() + "/archery-cli"
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	updateExecutable = func() (string, error) { return exe, nil }
	// Archive + checksums download fine; the signature-bundle download is the one
	// the user cancels (ctx cancelled), so the stage is verify_signature.
	updateDownloadHook = func(ctx context.Context, url, dest string) error {
		if strings.Contains(url, "sigstore.json") {
			return context.Canceled
		}
		return nil
	}

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if ok, _ := env["ok"].(bool); ok {
		t.Fatalf("interrupt must not be ok: %v", env)
	}
	if exit != ExitInterrupted {
		t.Fatalf("exit = %d, want 130", exit)
	}
	e := envError(t, env)
	if e["code"] != "E_INTERRUPTED" {
		t.Fatalf("code = %v, want E_INTERRUPTED", e["code"])
	}
	if e["code"] == "E_INTEGRITY" {
		t.Fatal("an interrupt must never be an integrity failure")
	}
	details, _ := e["details"].(map[string]any)
	if details["stage"] != "verify_signature" {
		t.Fatalf("stage = %v, want verify_signature", details["stage"])
	}
	if br, _ := details["binary_replaced"].(bool); br {
		t.Fatal("binary_replaced must be false when cancelled before the swap")
	}
}

// TestUpdate_DryRunReachableUnderPackageManager proves --dry-run is decided
// BEFORE the npm/pip branch: a package-manager-managed install can still get a
// read-only preview instead of being short-circuited into package_manager_required.
func TestUpdate_DryRunReachableUnderPackageManager(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()
	origAPI := updateGitHubAPI
	origExec := updateExecutable
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateExecutable = origExec
	})
	updateGitHubAPI = srv.URL

	// Lay out a node_modules tree whose package.json names this npm package so
	// detectInstallMethod returns "npm".
	root := t.TempDir()
	pkgDir := root + "/node_modules/@fateforge/archery-cli"
	if err := os.MkdirAll(pkgDir+"/bin", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pkgDir+"/package.json", []byte(`{"name":"`+updateNPMPackage+`"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	exe := pkgDir + "/bin/archery-cli"
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	updateExecutable = func() (string, error) { return exe, nil }

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t, "--dry-run")
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("dry-run ok=false: %v", env)
	}
	if exit != ExitOK {
		t.Fatalf("exit = %d, want 0", exit)
	}
	data := envData(t, env)
	if data["status"] != "dry_run" {
		t.Fatalf("status = %v, want dry_run (preview must win over package_manager_required)", data["status"])
	}
	if data["install_method"] != "npm" {
		t.Fatalf("install_method = %v, want npm", data["install_method"])
	}
	preview, _ := data["preview"].(map[string]any)
	if preview == nil {
		t.Fatalf("dry-run under npm must still carry a preview: %v", data)
	}
}

// TestUpdate_NPMDrivesPackageManager proves that a bare `update` on an npm-managed
// install EXECUTES the package manager (via the updateRunPackageManager seam) rather
// than printing a package_manager_required message. The seam is stubbed so tests
// never shell out to real npm.
func TestUpdate_NPMDrivesPackageManager(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()
	origAPI := updateGitHubAPI
	origExec := updateExecutable
	origPM := updateRunPackageManager
	origSync := updateSkillSync
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateExecutable = origExec
		updateRunPackageManager = origPM
		updateSkillSync = origSync
	})
	updateGitHubAPI = srv.URL

	root := t.TempDir()
	pkgDir := root + "/node_modules/@fateforge/archery-cli"
	if err := os.MkdirAll(pkgDir+"/bin", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pkgDir+"/package.json", []byte(`{"name":"`+updateNPMPackage+`"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	exe := pkgDir + "/bin/archery-cli"
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	updateExecutable = func() (string, error) { return exe, nil }

	var calledMethod, calledVersion string
	updateRunPackageManager = func(_ context.Context, method, ver string) error {
		calledMethod = method
		calledVersion = ver
		return nil
	}
	updateSkillSync = func(context.Context, string) error { return nil }

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("npm drive ok=false: %v", env)
	}
	if exit != ExitOK {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if calledMethod != "npm" {
		t.Fatalf("package manager method = %q, want npm", calledMethod)
	}
	if calledVersion != "9.9.9" {
		t.Fatalf("package manager version = %q, want 9.9.9", calledVersion)
	}
	data := envData(t, env)
	if data["status"] != "updated" {
		t.Fatalf("status = %v, want updated (drove npm)", data["status"])
	}
	if data["install_method"] != "npm" {
		t.Fatalf("install_method = %v, want npm", data["install_method"])
	}
}

// TestUpdate_PackageManagerFailureIsEIO proves a package manager failure is E_IO
// (exit 1, retryable:false, binary_replaced:false) — the PM owns integrity/replace,
// so a failure leaves the installed binary unchanged.
func TestUpdate_PackageManagerFailureIsEIO(t *testing.T) {
	srv := updateMockReleaseServer(t, "9.9.9")
	defer srv.Close()
	origAPI := updateGitHubAPI
	origExec := updateExecutable
	origPM := updateRunPackageManager
	t.Cleanup(func() {
		updateGitHubAPI = origAPI
		updateExecutable = origExec
		updateRunPackageManager = origPM
	})
	updateGitHubAPI = srv.URL

	root := t.TempDir()
	pkgDir := root + "/node_modules/@fateforge/archery-cli"
	if err := os.MkdirAll(pkgDir+"/bin", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pkgDir+"/package.json", []byte(`{"name":"`+updateNPMPackage+`"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	exe := pkgDir + "/bin/archery-cli"
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	updateExecutable = func() (string, error) { return exe, nil }
	updateRunPackageManager = func(context.Context, string, string) error {
		return errBatch("npm: EACCES permission denied")
	}

	t.Setenv("ARCHERY_CLI_NO_UPDATE_CHECK", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	env, exit := runUpdateCapture(t)
	if ok, _ := env["ok"].(bool); ok {
		t.Fatalf("PM failure must not be ok: %v", env)
	}
	if exit != ExitIO {
		t.Fatalf("exit = %d, want 1 (E_IO)", exit)
	}
	e := envError(t, env)
	if e["code"] != "E_IO" {
		t.Fatalf("code = %v, want E_IO", e["code"])
	}
	if retry, _ := e["retryable"].(bool); retry {
		t.Fatal("PM failure must be retryable:false")
	}
	details, _ := e["details"].(map[string]any)
	if br, _ := details["binary_replaced"].(bool); br {
		t.Fatal("binary_replaced must be false when PM fails")
	}
	if details["command"] == nil || details["command"] == "" {
		t.Fatal("PM failure must include the command in details")
	}
}

// TestDetectInstallMethod probes the install-method detection across the binary,
// npm, and pip layouts so an agent gets the right update path per install shape.
func TestDetectInstallMethod(t *testing.T) {
	root := t.TempDir()

	// npm: node_modules tree whose package.json names this package.
	npmDir := root + "/node_modules/@fateforge/archery-cli"
	if err := os.MkdirAll(npmDir+"/bin", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(npmDir+"/package.json", []byte(`{"name":"`+updateNPMPackage+`"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if got := detectInstallMethod(npmDir + "/bin/archery-cli"); got != "npm" {
		t.Errorf("npm layout -> %q, want npm", got)
	}

	// A node_modules path WITHOUT a matching package.json is not our npm install.
	otherDir := root + "/node_modules/other/bin"
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := detectInstallMethod(otherDir + "/archery-cli"); got != "binary" {
		t.Errorf("foreign node_modules -> %q, want binary", got)
	}

	// pip: a site-packages path is detected as pip.
	if got := detectInstallMethod(root + "/.venv/lib/python3.11/site-packages/archery_cli/cli"); got != "pip" {
		t.Errorf("site-packages layout -> %q, want pip", got)
	}

	// plain binary install.
	if got := detectInstallMethod(root + "/usr/local/bin/archery-cli"); got != "binary" {
		t.Errorf("plain layout -> %q, want binary", got)
	}
}

// TestUpdate_ErrorCodeExitMapping proves the new error codes map to the right
// exits in the shared status->code->exit mapping.
func TestUpdate_ErrorCodeExitMapping(t *testing.T) {
	cases := []struct {
		code output.ErrorCode
		exit int
	}{
		{output.E_IO, ExitIO},
		{output.E_INTERRUPTED, ExitInterrupted},
		{output.E_INTEGRITY, ExitError},
	}
	for _, c := range cases {
		if got := exitForErrorCode(c.code); got != c.exit {
			t.Errorf("exitForErrorCode(%s) = %d, want %d", c.code, got, c.exit)
		}
	}
}
