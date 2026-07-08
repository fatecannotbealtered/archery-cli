package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	updateDefaultRepo = "fatecannotbealtered/archery-cli"
	updateBinaryName  = "archery-cli"
	updateAPIBaseURL  = "https://api.github.com"
	updateNPMPackage  = "@fateforge/archery-cli"
	updatePipPackage  = "archery-cli"
	updateSkillRepo   = updateDefaultRepo
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update archery-cli to the latest release",
	Long: `Update archery-cli to the latest release in a single command.

A bare 'archery-cli update' performs the whole self-update in one call: resolve
the latest (or --target-version) release, download the platform archive and
checksums.txt, verify the Sigstore signature on checksums.txt in-process against
this repo's tagged release workflow identity, verify the archive SHA256, replace
the current binary, then sync the bundled Agent Skill directory. An unsigned or
unverifiable release is refused; there is no skip path. There is no confirm
token — self-update is exempt from the dry-run/confirm write gate.

Use --check for a read-only availability probe and --dry-run for a read-only
preview of the plan; neither changes anything and neither issues a token.`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

// updateStage names the staged-work phases of a self-update so every failure or
// interruption envelope can report exactly where the tool stopped (CLI-SPEC §14).
const (
	stageDiscover        = "discover"
	stageDownload        = "download"
	stageVerifySignature = "verify_signature"
	stageVerifyChecksum  = "verify_checksum"
	stageReplace         = "replace"
	stageSkillSync       = "skill_sync"
)

// updateFailDetails builds the failure envelope details shared by every update
// failure: stage, the version the tool is actually running NOW, whether the
// binary was already swapped, and the Skill sync state.
func updateFailDetails(stage, currentVersion string, binaryReplaced bool, skillSyncStatus string) map[string]any {
	return map[string]any{
		"stage":             stage,
		"current_version":   currentVersion,
		"binary_replaced":   binaryReplaced,
		"skill_sync_status": skillSyncStatus,
	}
}

// updateHTTPError carries the upstream HTTP status of a failed update request so
// the staged-work failure can be classified onto the taxonomy (CLI-SPEC §6/§14)
// instead of collapsing every transport failure into E_NETWORK. A zero StatusCode
// means the request never produced a response (connection refused / DNS / reset).
type updateHTTPError struct {
	StatusCode int
	err        error
}

func (e *updateHTTPError) Error() string { return e.err.Error() }
func (e *updateHTTPError) Unwrap() error { return e.err }

// classifyUpdateNetworkError maps an update HTTP failure onto the error taxonomy.
// A context cancellation/timeout is handled by the caller's interrupt path; here
// 403/404/408/429/5xx are split out so an agent can tell a missing release (don't
// retry) from a rate-limit (back off) rather than seeing a blanket E_NETWORK.
func classifyUpdateNetworkError(err error) output.ErrorCode {
	var httpErr *updateHTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode > 0 {
		return output.ErrorCodeFromStatus(httpErr.StatusCode)
	}
	return output.E_NETWORK
}

type updateReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type updateRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []updateReleaseAsset `json:"assets"`
}

type updatePlan struct {
	CurrentVersion     string
	TargetVersion      string
	ReleaseURL         string
	AssetName          string
	AssetURL           string
	ChecksumURL        string
	SignatureBundleURL string
	UpdateAvailable    bool
	Downgrade          bool
	InstallMethod      string
	SkillSyncCommand   string
}

type updateApplyResult struct {
	Status string
	Path   string
}

var (
	updateHTTPClient = &http.Client{Timeout: 2 * time.Minute}
	updateGitHubAPI  = updateAPIBaseURL
	updatePlatform   = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	updateExecutable = os.Executable
	updateApply      = applyUpdateBinary
	updateSkillSync  = runUpdateSkillSync
	// updateRunPackageManager is the testable seam for the npm/pip install step.
	// Tests stub this to avoid shelling out to real package managers.
	updateRunPackageManager = runPackageManagerInstall
	// Stage seams, overridable in tests so the staged-work failure contract can
	// be exercised without building real signed archives.
	updateDownloadHook = downloadUpdateFile
	updateChecksumHook = verifyUpdateChecksum
	updateExtractHook  = extractUpdateArchive
)

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().Bool("check", false, "Check for an available update without installing")
	updateCmd.Flags().String("target-version", "", "Install a specific version (for example 1.2.3 or v1.2.3)")
	updateCmd.Flags().Bool("reinstall", false, "Install even when the target version matches the current version")
	markRiskLevel(updateCmd, "medium")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")
	targetVersion, _ := cmd.Flags().GetString("target-version")
	reinstall, _ := cmd.Flags().GetBool("reinstall")

	ctx := cmd.Context()
	exePath, _ := updateExecutable()
	installMethod := detectInstallMethod(exePath)

	// discover: resolve the target release. Before the swap the installed binary
	// is untouched, so any failure here reports current_version unchanged.
	release, err := fetchUpdateRelease(ctx, targetVersion)
	if err != nil {
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			return interrupted(stageDiscover, version, false, "not_run", "cancelled before any change; still on "+version)
		}
		return failWithDetails("checking release: "+err.Error(), classifyUpdateNetworkError(err),
			updateFailDetails(stageDiscover, version, false, "not_run"))
	}

	plan, err := buildUpdatePlan(release, version)
	if err != nil {
		return failArg(err.Error())
	}
	plan.InstallMethod = installMethod
	plan.SkillSyncCommand = updateSkillSyncCommand()

	result := updateResultMap(plan, updateStatus(plan, targetVersion))
	if plan.Downgrade {
		result["downgrade"] = true
	}

	if checkOnly {
		notices := updateNoticesFromPlan(plan, "update_check")
		if len(notices) > 0 {
			result["notices"] = notices
		}
		writeUpdateNoticeCache(notices)
		printUpdateResult(result)
		return nil
	}

	// Idempotent: already on the latest (or requested) version is a no-op success.
	installNeeded := reinstall || plan.UpdateAvailable || targetVersionDiffers(plan, targetVersion)
	if !installNeeded {
		writeUpdateNoticeCache(nil)
		printUpdateResult(result)
		return nil
	}

	// --dry-run is an OPTIONAL read-only preview: it issues NO confirm_token and
	// NO expires_at — self-update is not a dry-run/confirm write gate (CLI-SPEC §14).
	// It is decided BEFORE the package-manager branch so a read-only preview is
	// always reachable, even when the binary is managed by npm/pip.
	if dryRun {
		result["status"] = "dry_run"
		changes := []map[string]any{}
		if installMethod == "npm" || installMethod == "pip" {
			changes = append(changes, map[string]any{
				"action":         "run package manager update",
				"currentVersion": plan.CurrentVersion,
				"targetVersion":  plan.TargetVersion,
				"command":        updateInstallCommand(installMethod, plan.TargetVersion),
			})
		} else {
			changes = append(changes, map[string]any{
				"action":         "replace binary",
				"currentVersion": plan.CurrentVersion,
				"targetVersion":  plan.TargetVersion,
				"asset":          plan.AssetName,
			})
		}
		changes = append(changes, map[string]any{"action": "sync skill directory", "command": plan.SkillSyncCommand})
		preview := map[string]any{
			"action":  "update archery-cli",
			"changes": changes,
		}
		if installMethod != "npm" && installMethod != "pip" {
			preview["verification"] = []string{"verify_signature", "verify_checksum"}
		}
		result["preview"] = preview
		printUpdateResult(result)
		return nil
	}

	if installMethod == "npm" || installMethod == "pip" {
		return runPackageManagerUpdate(ctx, plan, result, installMethod)
	}

	if err := ensureExecutable(exePath); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "archery-cli-update-*")
	if err != nil {
		// Local filesystem failure creating the staging dir, not a network blip.
		return failWithDetails("creating temp dir: "+err.Error(), output.E_IO,
			updateFailDetails(stageReplace, plan.CurrentVersion, false, "not_run"))
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// download: still touches only the temp dir; failure leaves the binary intact.
	archivePath := filepath.Join(tmpDir, plan.AssetName)
	if err := updateDownloadHook(ctx, plan.AssetURL, archivePath); err != nil {
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			return interrupted(stageDownload, plan.CurrentVersion, false, "not_run", "cancelled during download; no change, still on "+plan.CurrentVersion)
		}
		return failWithDetails("downloading archive: "+err.Error(), classifyUpdateNetworkError(err),
			updateFailDetails(stageDownload, plan.CurrentVersion, false, "not_run"))
	}

	checksumPath := filepath.Join(tmpDir, "checksums.txt")
	if err := updateDownloadHook(ctx, plan.ChecksumURL, checksumPath); err != nil {
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			return interrupted(stageDownload, plan.CurrentVersion, false, "not_run", "cancelled during download; no change, still on "+plan.CurrentVersion)
		}
		return failWithDetails("downloading checksums: "+err.Error(), classifyUpdateNetworkError(err),
			updateFailDetails(stageDownload, plan.CurrentVersion, false, "not_run"))
	}

	// verify_signature -> verify_checksum: signature is verified FIRST, then the
	// archive checksum. Only a real signature/checksum verdict is E_INTEGRITY
	// (non-retryable); fetching the trust root or the signature bundle is a network
	// step, so its failure is a retryable network class, and a SIGINT here is an
	// interrupt — none of those are a forged-release verdict to stop and report.
	signatureStatus, code, err := verifyUpdateChecksumSignature(ctx, checksumPath, plan.SignatureBundleURL, tmpDir)
	if err != nil {
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			return interrupted(stageVerifySignature, plan.CurrentVersion, false, "not_run", "cancelled during signature verification; no change, still on "+plan.CurrentVersion)
		}
		return failWithDetails("verifying release signature: "+err.Error(), code,
			updateFailDetails(stageVerifySignature, plan.CurrentVersion, false, "not_run"))
	}
	if err := ctx.Err(); err != nil {
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			return interrupted(stageVerifyChecksum, plan.CurrentVersion, false, "not_run", "cancelled before checksum verification; no change, still on "+plan.CurrentVersion)
		}
	}
	if err := updateChecksumHook(archivePath, checksumPath, plan.AssetName); err != nil {
		return failWithDetails("verifying archive: "+err.Error(), output.E_INTEGRITY,
			updateFailDetails(stageVerifyChecksum, plan.CurrentVersion, false, "not_run"))
	}

	// replace: local extract + atomic swap. Failures here are filesystem/permission
	// problems (MISCLASSIFIED as network before this change), not transient.
	binPath, err := updateExtractHook(archivePath, plan.AssetName, tmpDir)
	if err != nil {
		return failWithDetails("extracting archive: "+err.Error(), output.E_IO,
			updateFailDetails(stageReplace, plan.CurrentVersion, false, "not_run"))
	}

	applied, err := updateApply(binPath, exePath)
	if err != nil {
		return failWithDetails("installing update: "+err.Error(), updateReplaceFailureClass(err),
			updateFailDetails(stageReplace, plan.CurrentVersion, false, "not_run"))
	}
	writeUpdateNoticeCache(nil)

	// skill_sync runs AFTER the atomic swap. A failure here is PARTIAL SUCCESS:
	// the binary is on the new version, only the Skill is stale. Report that
	// truthfully (ok:false, binary_replaced:true) with the replay command, not a
	// hard network error that hides the successful swap.
	if err := updateSkillSync(ctx, updateSkillRepo); err != nil {
		details := updateFailDetails(stageSkillSync, plan.TargetVersion, true, "failed")
		details["binary_replaced"] = true
		details["skill_sync_command"] = plan.SkillSyncCommand
		details["previous_version"] = plan.CurrentVersion
		details["target_version"] = plan.TargetVersion
		details["update_available"] = false
		details["signature_status"] = signatureStatus
		details["status"] = applied.Status
		details["hint"] = fmt.Sprintf("binary now at %s; run %q to sync the Skill, then \"archery-cli changelog --since %s\"", plan.TargetVersion, plan.SkillSyncCommand, plan.CurrentVersion)
		// Post-swap, so still partial success (binary NEW). A SIGINT here switches
		// the code to E_INTERRUPTED, and the exit follows the code (130) rather than
		// staying hardcoded at the network exit.
		code := output.E_NETWORK
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			code = output.E_INTERRUPTED
		}
		return failWithDetails("syncing skill directory: "+err.Error(), code, details)
	}

	result = updateResultMap(plan, applied.Status)
	result["path"] = applied.Path
	result["previous_version"] = plan.CurrentVersion
	result["current_version"] = plan.TargetVersion
	result["update_available"] = false
	result["binary_replaced"] = true
	result["checksum_verified"] = true
	result["signature_status"] = signatureStatus
	if signatureStatus == "verified" {
		result["signature_verified"] = true
	}
	result["skill_sync_status"] = "synced"
	result["hint"] = fmt.Sprintf("run \"archery-cli changelog --since %s\" to see what changed", plan.CurrentVersion)
	printUpdateResult(result)
	return nil
}

// updateReplaceFailureClass classifies a binary-replace failure by the agent's
// next action: a permission error needs an environment fix (E_FORBIDDEN, exit 4),
// any other filesystem/disk failure is E_IO (exit 1). Neither is retryable, and
// neither is the old misclassified E_NETWORK. The exit code is derived from the
// returned error code through exitForErrorCode.
func updateReplaceFailureClass(err error) output.ErrorCode {
	if errors.Is(err, os.ErrPermission) {
		return output.E_FORBIDDEN
	}
	return output.E_IO
}

// updateInterrupted returns a terminal-envelope emitter when ctx was cancelled by
// a trapped signal (SIGINT/SIGTERM). The returned closure emits E_INTERRUPTED
// (exit 130) with the stage invariant message, so an interrupted agent still
// receives a parseable terminal state instead of a bare killed process.
func updateInterrupted(ctx context.Context, err error) func(stage, currentVersion string, binaryReplaced bool, skillSyncStatus, message string) error {
	if ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		// A timeout is a transient network condition, not a user/signal interrupt.
		return nil
	}
	return func(stage, currentVersion string, binaryReplaced bool, skillSyncStatus, message string) error {
		details := updateFailDetails(stage, currentVersion, binaryReplaced, skillSyncStatus)
		return failWithDetails("update cancelled: "+message, output.E_INTERRUPTED, details)
	}
}

func updateSkillSyncCommand() string {
	return "npx skills add " + updateSkillRepo + " -y -g"
}

func runUpdateSkillSync(ctx context.Context, repo string) error {
	command := exec.CommandContext(ctx, "npx", "skills", "add", repo, "-y", "-g")
	outputBytes, err := command.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(outputBytes))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, truncateForError(msg, 300))
		}
		return err
	}
	return nil
}

// runPackageManagerUpdate handles `update` for a package-manager-managed install
// (npm or pip). The binary is owned by the package manager, so instead of replacing
// it in place, the tool DRIVES the package manager (runs the install command), then
// syncs the Skill. Integrity on this path is the package manager's own; signature_status
// stays "not_checked". The new version takes effect on the next invocation.
func runPackageManagerUpdate(ctx context.Context, plan updatePlan, result map[string]any, method string) error {
	command := updateInstallCommand(method, plan.TargetVersion)
	result["command"] = command
	if err := updateRunPackageManager(ctx, method, plan.TargetVersion); err != nil {
		return failPackageManagerStage(result, command, err)
	}
	writeUpdateNoticeCache(nil)
	// The package manager replaced the on-disk binary; this process is still the
	// old image, so the new version is effective on the next invocation.
	result["status"] = "updated"
	result["previous_version"] = plan.CurrentVersion
	result["current_version"] = plan.TargetVersion
	result["update_available"] = false
	result["signature_status"] = "not_checked"
	result["binary_replaced"] = true
	if err := updateSkillSync(ctx, updateSkillRepo); err != nil {
		details := updateFailDetails(stageSkillSync, plan.TargetVersion, true, "failed")
		details["binary_replaced"] = true
		details["skill_sync_command"] = plan.SkillSyncCommand
		details["previous_version"] = plan.CurrentVersion
		details["target_version"] = plan.TargetVersion
		details["update_available"] = false
		details["install_method"] = method
		details["hint"] = fmt.Sprintf("binary now at %s; run %q to sync the Skill", plan.TargetVersion, plan.SkillSyncCommand)
		code := output.E_NETWORK
		if interrupted := updateInterrupted(ctx, err); interrupted != nil {
			code = output.E_INTERRUPTED
		}
		return failWithDetails("syncing skill directory: "+err.Error(), code, details)
	}
	result["skill_sync_status"] = "synced"
	result["hint"] = fmt.Sprintf("run \"archery-cli changelog --since %s\" to see what changed", plan.CurrentVersion)
	printUpdateResult(result)
	return nil
}

// runPackageManagerInstall drives the package manager to install the target version.
// argv is built directly (no shell) so the version string cannot be reinterpreted
// by a shell.
func runPackageManagerInstall(ctx context.Context, method, targetVersion string) error {
	var name string
	var args []string
	v := normalizeVersion(targetVersion)
	switch method {
	case "npm":
		name = "npm"
		args = []string{"install", "-g", updateNPMPackage + "@" + v}
	case "pip":
		name = "pip"
		args = []string{"install", "--upgrade", updatePipPackage + "==" + v}
	default:
		return fmt.Errorf("unsupported package manager: %s", method)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(outputBytes))
		if out != "" {
			return fmt.Errorf("%w: %s", err, truncateForError(out, 300))
		}
		return err
	}
	return nil
}

// failPackageManagerStage reports a failed package-manager-driven update. The
// package manager owns download/integrity/replace, so a failure leaves the
// installed binary unchanged (binary_replaced:false). The exact command is
// surfaced so the agent can run it manually.
func failPackageManagerStage(result map[string]any, command string, err error) error {
	msg := fmt.Sprintf("package-manager update failed: %s — run %q manually", strings.TrimSpace(err.Error()), command)
	method, _ := result["install_method"].(string)
	currentVersion, _ := result["current_version"].(string)
	details := updateFailDetails(stageReplace, currentVersion, false, "not_run")
	details["install_method"] = method
	details["command"] = command
	return failWithDetails(msg, output.E_IO, details)
}

func targetVersionDiffers(plan updatePlan, requested string) bool {
	return strings.TrimSpace(requested) != "" && normalizeVersion(plan.TargetVersion) != normalizeVersion(plan.CurrentVersion)
}

func updateStatus(plan updatePlan, requested string) string {
	if plan.UpdateAvailable {
		return "available"
	}
	if plan.Downgrade {
		if strings.TrimSpace(requested) != "" {
			return "downgrade"
		}
		return "ahead"
	}
	return "up_to_date"
}

func fetchUpdateRelease(ctx context.Context, targetVersion string) (*updateRelease, error) {
	url := updateReleaseURL(updateDefaultRepo, targetVersion)
	data, err := updateHTTPGet(ctx, url)
	if err != nil {
		return nil, err
	}
	var rel updateRelease
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("parsing release JSON: %w", err)
	}
	return &rel, nil
}

func updateReleaseURL(repo, targetVersion string) string {
	base := strings.TrimRight(updateGitHubAPI, "/")
	if strings.TrimSpace(targetVersion) != "" {
		return base + "/repos/" + repo + "/releases/tags/" + canonicalVersionTag(targetVersion)
	}
	return base + "/repos/" + repo + "/releases/latest"
}

func buildUpdatePlan(rel *updateRelease, currentVersion string) (updatePlan, error) {
	if rel == nil {
		return updatePlan{}, errors.New("empty release response")
	}
	target := normalizeVersion(rel.TagName)
	if target == "" {
		return updatePlan{}, errors.New("release is missing tag_name")
	}
	assetName, err := updateArchiveName(target)
	if err != nil {
		return updatePlan{}, err
	}
	assetURL := findUpdateAssetURL(rel.Assets, assetName)
	if assetURL == "" {
		return updatePlan{}, fmt.Errorf("release %s does not include asset %s", rel.TagName, assetName)
	}
	checksumURL := findUpdateAssetURL(rel.Assets, "checksums.txt")
	if checksumURL == "" {
		return updatePlan{}, fmt.Errorf("release %s does not include checksums.txt", rel.TagName)
	}
	signatureBundleURL := findUpdateAssetURL(rel.Assets, "checksums.txt.sigstore.json")

	current := normalizeVersion(currentVersion)
	cmp := compareVersions(current, target)
	return updatePlan{
		CurrentVersion:     currentVersion,
		TargetVersion:      target,
		ReleaseURL:         rel.HTMLURL,
		AssetName:          assetName,
		AssetURL:           assetURL,
		ChecksumURL:        checksumURL,
		SignatureBundleURL: signatureBundleURL,
		UpdateAvailable:    cmp < 0,
		Downgrade:          cmp > 0,
	}, nil
}

func updateArchiveName(ver string) (string, error) {
	goos, goarch := updatePlatform()
	platform, ok := map[string]string{
		"darwin":  "darwin",
		"linux":   "linux",
		"windows": "windows",
	}[goos]
	if !ok {
		return "", fmt.Errorf("unsupported update platform: %s-%s", goos, goarch)
	}
	arch, ok := map[string]string{
		"amd64": "amd64",
		"arm64": "arm64",
	}[goarch]
	if goos == "windows" && goarch == "arm64" {
		arch, ok = "amd64", true
	}
	if !ok {
		return "", fmt.Errorf("unsupported update platform: %s-%s", goos, goarch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s-%s-%s-%s%s", updateBinaryName, normalizeVersion(ver), platform, arch, ext), nil
}

func findUpdateAssetURL(assets []updateReleaseAsset, name string) string {
	for _, asset := range assets {
		if asset.Name == name {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func updateHTTPGet(ctx context.Context, url string) ([]byte, error) {
	req, err := newUpdateRequest(ctx, url, "application/json")
	if err != nil {
		return nil, err
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("reading response: %w", readErr)
	}
	if resp.StatusCode >= 400 {
		return nil, &updateHTTPError{
			StatusCode: resp.StatusCode,
			err:        fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, truncateForError(string(data), 200)),
		}
	}
	return data, nil
}

func downloadUpdateFile(ctx context.Context, url, dest string) error {
	req, err := newUpdateRequest(ctx, url, "application/octet-stream")
	if err != nil {
		return err
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return &updateHTTPError{
			StatusCode: resp.StatusCode,
			err:        fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, truncateForError(string(data), 200)),
		}
	}
	tmp := dest + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func newUpdateRequest(ctx context.Context, url, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "archery-cli")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

func verifyUpdateChecksum(archivePath, checksumPath, assetName string) error {
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("reading checksums: %w", err)
	}
	expected := ""
	for _, line := range strings.Split(string(checksumData), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) == assetName {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", assetName)
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("reading archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return fmt.Errorf("hashing archive: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

// verifyUpdateChecksumSignature enforces a mandatory, in-process Sigstore
// signature check on checksums.txt before the release is trusted. There is no
// skip path: a release without a signature bundle, or one whose signature does
// not verify against this repo's release-workflow identity, is refused.
//
// It returns the signature status, the error code to surface, and the error.
// The code matters because only a real verification verdict is E_INTEGRITY
// (non-retryable): a missing bundle (unsigned release refused) and a signature
// that does not verify are integrity failures, but DOWNLOADING the bundle is a
// network step whose failure is retryable (E_NETWORK / classified by status),
// and a SIGINT mid-download is an interrupt the caller maps to E_INTERRUPTED.
// On the nil-error path the status is always "verified".
func verifyUpdateChecksumSignature(ctx context.Context, checksumPath, bundleURL, tmpDir string) (string, output.ErrorCode, error) {
	if strings.TrimSpace(bundleURL) == "" {
		return "missing", output.E_INTEGRITY, errors.New("release does not include checksums.txt.sigstore.json; refusing to install an unsigned release")
	}
	bundlePath := filepath.Join(tmpDir, "checksums.txt.sigstore.json")
	if err := updateDownloadHook(ctx, bundleURL, bundlePath); err != nil {
		// Fetching the bundle is a network step, not a forged-release verdict:
		// classify by status (or interrupt, which the caller detects on ctx.Err).
		return "download_failed", classifyUpdateNetworkError(err), fmt.Errorf("downloading checksum signature bundle: %w", err)
	}
	if err := updateVerifySignature(ctx, checksumPath, bundlePath, updateSignerIdentityRegexp()); err != nil {
		// Obtaining the trust root is a network step, not a signature verdict:
		// a transient trust-root fetch failure stays retryable network, only a
		// real signature mismatch is the non-retryable E_INTEGRITY verdict.
		if errors.Is(err, errTrustRootUnavailable) {
			return "trust_root_unavailable", output.E_NETWORK, err
		}
		return "failed", output.E_INTEGRITY, err
	}
	return "verified", output.E_UNKNOWN, nil
}

func extractUpdateArchive(archivePath, assetName, tmpDir string) (string, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractUpdateZip(archivePath, tmpDir)
	}
	if strings.HasSuffix(assetName, ".tar.gz") {
		return extractUpdateTarGz(archivePath, tmpDir)
	}
	return "", fmt.Errorf("unsupported archive type: %s", assetName)
}

func extractUpdateZip(archivePath, tmpDir string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()
	want := updateArchiveBinaryName()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer func() { _ = rc.Close() }()
		return writeExtractedUpdateBinary(tmpDir, want, rc)
	}
	return "", fmt.Errorf("%s not found in archive", want)
}

func extractUpdateTarGz(archivePath, tmpDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	want := updateArchiveBinaryName()
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != want {
			continue
		}
		return writeExtractedUpdateBinary(tmpDir, want, tr)
	}
	return "", fmt.Errorf("%s not found in archive", want)
}

func updateArchiveBinaryName() string {
	goos, _ := updatePlatform()
	if goos == "windows" {
		return updateBinaryName + ".exe"
	}
	return updateBinaryName
}

func writeExtractedUpdateBinary(tmpDir, name string, r io.Reader) (string, error) {
	outDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", err
	}
	outPath := filepath.Join(outDir, name)
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return outPath, nil
}

// applyUpdateBinary performs an in-place atomic replacement of the running
// executable using the rename trick, identically on Windows and Unix:
// write .<base>.new -> rename target to .<base>.old (moves the in-use binary
// aside, which Windows allows) -> rename .new into place -> on failure restore
// from .old. On success remove .old (ignored if Windows still has it locked).
func applyUpdateBinary(src, dst string) (updateApplyResult, error) {
	target := dst
	if resolved, err := filepath.EvalSymlinks(dst); err == nil {
		target = resolved
	}
	mode := os.FileMode(0o755)
	if st, err := os.Stat(target); err == nil {
		mode = st.Mode().Perm()
		if mode&0o111 == 0 {
			mode |= 0o755
		}
	}
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	newPath := filepath.Join(dir, "."+base+".new")
	backupPath := filepath.Join(dir, "."+base+".old")

	_ = os.Remove(newPath)
	if err := copyFile(src, newPath, mode); err != nil {
		return updateApplyResult{}, err
	}

	_ = os.Remove(backupPath)
	if err := os.Rename(target, backupPath); err != nil {
		_ = os.Remove(newPath)
		return updateApplyResult{}, fmt.Errorf("preparing to replace %s: %w", target, err)
	}
	if err := os.Rename(newPath, target); err != nil {
		_ = os.Rename(backupPath, target)
		_ = os.Remove(newPath)
		return updateApplyResult{}, fmt.Errorf("replacing %s: %w; original restored", target, err)
	}
	_ = os.Remove(backupPath)
	return updateApplyResult{Status: "installed", Path: target}, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func updateResultMap(plan updatePlan, status string) map[string]any {
	result := map[string]any{
		"status":             status,
		"asset":              plan.AssetName,
		"current_version":    plan.CurrentVersion,
		"target_version":     plan.TargetVersion,
		"update_available":   plan.UpdateAvailable,
		"release_url":        plan.ReleaseURL,
		"install_method":     plan.InstallMethod,
		"signature_status":   "not_checked",
		"skill_sync_command": plan.SkillSyncCommand,
		"skill_sync_status":  "not_run",
	}
	return result
}

func printUpdateResult(result map[string]any) {
	if jsonMode {
		output.PrintJSON(result)
		return
	}
	status, _ := result["status"].(string)
	current, _ := result["current_version"].(string)
	target, _ := result["target_version"].(string)
	command, _ := result["command"].(string)
	switch status {
	case "up_to_date":
		output.Success(fmt.Sprintf("archery-cli is up to date (%s)", current))
	case "available":
		output.Info(fmt.Sprintf("Update available: %s -> %s", current, target))
	case "downgrade":
		output.Info(fmt.Sprintf("Target version is older: %s -> %s", current, target))
	case "ahead":
		output.Info(fmt.Sprintf("Current version %s is newer than latest release %s", current, target))
	case "dry_run":
		output.Info(fmt.Sprintf("[dry-run] update archery-cli to %s", target))
	case "installed":
		output.Success(fmt.Sprintf("Updated archery-cli to %s", target))
	case "package_manager_required":
		output.Warn(fmt.Sprintf("archery-cli is managed by a package manager; run the suggested command to update to %s", target))
		if command != "" {
			output.Gray("  " + command)
		}
	default:
		output.Info(fmt.Sprintf("Update status: %s", status))
	}
}

func ensureExecutable(path string) error {
	if path == "" {
		return failWithDetails("could not determine current executable path", output.E_IO,
			updateFailDetails(stageReplace, version, false, "not_run"))
	}
	return nil
}

func detectInstallMethod(exe string) string {
	exe = filepath.Clean(exe)
	if exe != "" && pathHasSegment(exe, "node_modules") && npmPackageRoot(exe) != "" {
		return "npm"
	}
	if isPipOrVenvPath(exe) {
		return "pip"
	}
	return "binary"
}

func pathHasSegment(path, segment string) bool {
	for _, part := range strings.FieldsFunc(filepath.Clean(path), func(r rune) bool {
		return r == os.PathSeparator || r == '/' || r == '\\'
	}) {
		if part == segment {
			return true
		}
	}
	return false
}

func npmPackageRoot(exe string) string {
	for dir := filepath.Dir(exe); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(data, &pkg) == nil && pkg.Name == updateNPMPackage {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return ""
}

// isPipOrVenvPath reports whether the executable lives in a genuine pip/venv
// install layout. It matches only real path SEGMENTS (site-packages, venv/.venv,
// lib + python) — never a loose substring like "pip"/"conda" anywhere in the
// path, which would misclassify a standalone binary under any directory that
// merely contains those letters (e.g. a "pipeline" folder) as pip-managed and
// then drive a bogus `pip install`.
func isPipOrVenvPath(exe string) bool {
	exe = filepath.Clean(exe)
	if pathHasSegment(exe, "site-packages") {
		return true
	}
	if pathHasSegment(exe, "venv") || pathHasSegment(exe, ".venv") {
		return true
	}
	if pathHasSegment(exe, "lib") && pathHasSegment(exe, "python") {
		return true
	}
	return false
}

func updateInstallCommand(method, targetVersion string) string {
	v := normalizeVersion(targetVersion)
	switch method {
	case "npm":
		return "npm install -g " + updateNPMPackage + "@" + v
	case "pip":
		return "pip install --upgrade " + updatePipPackage + "==" + v
	default:
		return ""
	}
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "refs/tags/")
	v = strings.TrimPrefix(v, "v")
	return v
}

func canonicalVersionTag(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func compareVersions(current, target string) int {
	if current == target {
		return 0
	}
	c, cOK := parseSemver(current)
	t, tOK := parseSemver(target)
	if !cOK && tOK {
		return -1
	}
	if cOK && !tOK {
		return 1
	}
	if !cOK && !tOK {
		return strings.Compare(current, target)
	}
	for i := 0; i < 3; i++ {
		if c.nums[i] < t.nums[i] {
			return -1
		}
		if c.nums[i] > t.nums[i] {
			return 1
		}
	}
	if c.pre == t.pre {
		return 0
	}
	if c.pre == "" {
		return 1
	}
	if t.pre == "" {
		return -1
	}
	return comparePrerelease(c.pre, t.pre)
}

type semverParts struct {
	nums [3]int
	pre  string
}

func parseSemver(v string) (semverParts, bool) {
	var out semverParts
	v = normalizeVersion(v)
	if v == "" {
		return out, false
	}
	if plus := strings.Index(v, "+"); plus >= 0 {
		v = v[:plus]
	}
	if dash := strings.Index(v, "-"); dash >= 0 {
		out.pre = v[dash+1:]
		v = v[:dash]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, part := range parts {
		if part == "" {
			return out, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out.nums[i] = n
	}
	return out, true
}

func comparePrerelease(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		if i >= len(aParts) {
			return -1
		}
		if i >= len(bParts) {
			return 1
		}
		cmp := comparePrereleaseIdentifier(aParts[i], bParts[i])
		if cmp != 0 {
			return cmp
		}
	}
	return 0
}

func comparePrereleaseIdentifier(a, b string) int {
	aNum, aOK := parseNumericIdentifier(a)
	bNum, bOK := parseNumericIdentifier(b)
	switch {
	case aOK && bOK:
		if aNum < bNum {
			return -1
		}
		if aNum > bNum {
			return 1
		}
		return 0
	case aOK:
		return -1
	case bOK:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func parseNumericIdentifier(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func truncateForError(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
