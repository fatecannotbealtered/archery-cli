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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	updateDefaultRepo   = "fatecannotbealtered/archery-cli"
	updateBinaryName    = "archery-cli"
	updateAPIBaseURL    = "https://api.github.com"
	updateNPMPackage    = "@fatecannotbealtered-/archery-cli"
	updatePipPackage    = "archery-cli"
	updateChannelStable = "stable"
	updateChannelCanary = "canary"
	updateSkillRepo     = updateDefaultRepo
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update archery-cli to the latest release",
	Long: `Check GitHub Releases for a newer archery-cli binary and install it.

The update flow downloads the platform archive and checksums.txt, verifies the
signed checksum when possible, verifies the archive SHA256, extracts the
archery-cli binary, and replaces the current binary.

Use --check to inspect availability without installing. Writes require
--dry-run first, then --confirm <confirm_token> in non-interactive agent runs.`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
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
	Channel            string
	SkillSyncCommand   string
}

type updateApplyResult struct {
	Status      string
	Path        string
	PendingPath string
}

var (
	updateHTTPClient = &http.Client{Timeout: 2 * time.Minute}
	updateGitHubAPI  = updateAPIBaseURL
	updateNow        = time.Now
	updatePlatform   = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	updateExecutable = os.Executable
	updateApply      = applyUpdateBinary
	updateSkillSync  = runUpdateSkillSync
)

func init() {
	rootCmd.AddCommand(updateCmd)
	updateCmd.Flags().Bool("check", false, "Check for an available update without installing")
	updateCmd.Flags().String("target-version", "", "Install a specific version (for example 1.2.3 or v1.2.3)")
	updateCmd.Flags().Bool("reinstall", false, "Install even when the target version matches the current version")
	updateCmd.Flags().String("channel", updateChannelStable, "Release channel: stable or canary")
	markWrite(updateCmd)
	markConfirm(updateCmd)
	markRiskLevel(updateCmd, "medium")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")
	targetVersion, _ := cmd.Flags().GetString("target-version")
	reinstall, _ := cmd.Flags().GetBool("reinstall")
	channel, _ := cmd.Flags().GetString("channel")
	if channel != updateChannelStable && channel != updateChannelCanary {
		return failArg("invalid channel: must be 'stable' or 'canary'")
	}

	exePath, _ := updateExecutable()
	installMethod := detectInstallMethod(exePath)

	release, err := fetchUpdateRelease(cmd.Context(), targetVersion)
	if err != nil {
		return failWithCode("checking release: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	plan, err := buildUpdatePlan(release, version)
	if err != nil {
		return failArg(err.Error())
	}
	plan.InstallMethod = installMethod
	plan.Channel = channel
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

	installNeeded := reinstall || plan.UpdateAvailable || targetVersionDiffers(plan, targetVersion)
	if !installNeeded {
		printUpdateResult(result)
		return nil
	}

	if installMethod == "npm" || installMethod == "pip" {
		cmd := updateInstallCommand(installMethod, plan.TargetVersion)
		result["status"] = "package_manager_required"
		result["command"] = cmd
		printUpdateResult(result)
		return nil
	}

	confirmPayload := map[string]any{
		"currentVersion":     plan.CurrentVersion,
		"targetVersion":      plan.TargetVersion,
		"assetName":          plan.AssetName,
		"assetURL":           plan.AssetURL,
		"checksumURL":        plan.ChecksumURL,
		"signatureBundleURL": plan.SignatureBundleURL,
		"reinstall":          reinstall,
		"channel":            channel,
		"skillSyncCommand":   plan.SkillSyncCommand,
	}
	if dryRun {
		confirmToken, expires := newConfirmToken(cmd.CommandPath(), "", confirmPayload)
		result["status"] = "dry_run"
		result["preview"] = map[string]any{
			"action": "update archery-cli",
			"changes": []map[string]any{
				confirmPayload,
				{"action": "sync skill directory", "command": plan.SkillSyncCommand},
			},
		}
		result["confirm_token"] = confirmToken
		result["expires_at"] = expires.Format(time.RFC3339)
		printUpdateResult(result)
		return nil
	}

	if err := requireConfirm(cmd, cmd.CommandPath(), "", confirmPayload); err != nil {
		return err
	}

	if err := ensureExecutable(exePath); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "archery-cli-update-*")
	if err != nil {
		return failWithCode("creating temp dir: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, plan.AssetName)
	if err := downloadUpdateFile(cmd.Context(), plan.AssetURL, archivePath); err != nil {
		return failWithCode("downloading archive: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	checksumPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadUpdateFile(cmd.Context(), plan.ChecksumURL, checksumPath); err != nil {
		return failWithCode("downloading checksums: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}
	signatureStatus, err := verifyUpdateChecksumSignature(cmd.Context(), checksumPath, plan.SignatureBundleURL, tmpDir)
	if err != nil {
		return failWithCode(err.Error(), ExitNetwork, output.E_NETWORK)
	}
	if err := verifyUpdateChecksum(archivePath, checksumPath, plan.AssetName); err != nil {
		return failWithCode("verifying archive: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	binPath, err := extractUpdateArchive(archivePath, plan.AssetName, tmpDir)
	if err != nil {
		return failWithCode("extracting archive: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	applied, err := updateApply(binPath, exePath)
	if err != nil {
		return failWithCode("installing update: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}
	if err := updateSkillSync(cmd.Context(), updateSkillRepo); err != nil {
		return failWithCode("syncing skill directory: "+err.Error(), ExitNetwork, output.E_NETWORK)
	}

	result = updateResultMap(plan, applied.Status)
	result["path"] = applied.Path
	result["previous_version"] = plan.CurrentVersion
	result["current_version"] = plan.TargetVersion
	result["checksum_verified"] = true
	result["signature_status"] = signatureStatus
	if signatureStatus == "verified" {
		result["signature_verified"] = true
	}
	result["skill_sync_status"] = "synced"
	result["hint"] = fmt.Sprintf("run \"archery-cli changelog --since %s\" to see what changed", plan.CurrentVersion)
	if applied.PendingPath != "" {
		result["pending_path"] = applied.PendingPath
	}
	printUpdateResult(result)
	return nil
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
		return nil, fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, truncateForError(string(data), 200))
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
		return fmt.Errorf("GET %s returned %d: %s", url, resp.StatusCode, truncateForError(string(data), 200))
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

func verifyUpdateChecksumSignature(ctx context.Context, checksumPath, bundleURL, tmpDir string) (string, error) {
	if strings.TrimSpace(bundleURL) == "" {
		if updateRequireSignature() {
			return "missing_bundle", fmt.Errorf("release does not include checksums.txt.sigstore.json")
		}
		return "missing_bundle", nil
	}
	if _, err := exec.LookPath("cosign"); err != nil {
		if updateRequireSignature() {
			return "cosign_missing", fmt.Errorf("cosign is not installed; checksum signature verification cannot run")
		}
		return "skipped_cosign_missing", nil
	}

	bundlePath := filepath.Join(tmpDir, "checksums.txt.sigstore.json")
	if err := downloadUpdateFile(ctx, bundleURL, bundlePath); err != nil {
		return "download_failed", fmt.Errorf("downloading checksum signature bundle: %w", err)
	}
	identity := "^https://github\\.com/" + regexp.QuoteMeta(updateDefaultRepo) + "/\\.github/workflows/release\\.yml@refs/tags/v.*$"
	command := exec.CommandContext(ctx, "cosign",
		"verify-blob",
		"--bundle", bundlePath,
		"--certificate-identity-regexp", identity,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		checksumPath,
	)
	outputBytes, err := command.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(outputBytes))
		if msg != "" {
			return "failed", fmt.Errorf("verifying checksum signature: %w: %s", err, truncateForError(msg, 300))
		}
		return "failed", fmt.Errorf("verifying checksum signature: %w", err)
	}
	return "verified", nil
}

func updateRequireSignature() bool {
	return os.Getenv("ARCHERY_CLI_REQUIRE_SIGNATURE") == "1"
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

func applyUpdateBinary(src, dst string) (updateApplyResult, error) {
	if runtime.GOOS == "windows" {
		return scheduleWindowsBinaryReplace(src, dst)
	}
	mode := os.FileMode(0o755)
	if st, err := os.Stat(dst); err == nil {
		mode = st.Mode().Perm()
		if mode&0o111 == 0 {
			mode |= 0o755
		}
	}
	tmpName := fmt.Sprintf(".%s.update-%d", filepath.Base(dst), updateNow().UnixNano())
	tmpPath := filepath.Join(filepath.Dir(dst), tmpName)
	if err := copyFile(src, tmpPath, mode); err != nil {
		return updateApplyResult{}, err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return updateApplyResult{}, err
	}
	return updateApplyResult{Status: "installed", Path: dst}, nil
}

func scheduleWindowsBinaryReplace(src, dst string) (updateApplyResult, error) {
	pending := dst + ".new"
	if err := copyFile(src, pending, 0o755); err != nil {
		return updateApplyResult{}, err
	}
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("archery-cli-update-%d.cmd", updateNow().UnixNano()))
	script := strings.Join([]string{
		"@echo off",
		"setlocal",
		"set \"PENDING=" + batchEscape(pending) + "\"",
		"set \"TARGET=" + batchEscape(dst) + "\"",
		"for /L %%I in (1,1,30) do (",
		"  move /Y \"%PENDING%\" \"%TARGET%\" > nul 2>&1",
		"  if not exist \"%PENDING%\" goto done",
		"  ping 127.0.0.1 -n 2 > nul",
		")",
		":done",
		"del \"%~f0\" > nul 2>&1",
		"",
	}, "\r\n")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return updateApplyResult{}, err
	}
	if err := exec.Command("cmd", "/C", "start", "", "/B", scriptPath).Start(); err != nil {
		return updateApplyResult{}, err
	}
	return updateApplyResult{Status: "scheduled", Path: dst, PendingPath: pending}, nil
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
		"channel":            plan.Channel,
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
	case "scheduled":
		output.Success(fmt.Sprintf("Update to %s scheduled; restart the command after this process exits", target))
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
		return failWithCode("could not determine current executable path", ExitNetwork, output.E_NETWORK)
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

func isPipOrVenvPath(exe string) bool {
	exe = filepath.Clean(exe)
	lower := strings.ToLower(exe)
	if pathHasSegment(exe, "site-packages") {
		return true
	}
	if pathHasSegment(exe, "venv") || pathHasSegment(exe, ".venv") {
		return true
	}
	if strings.Contains(lower, "pip") || strings.Contains(lower, "conda") {
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

func batchEscape(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}
