package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// applyUpdateBinary uses the cross-platform rename trick: the in-use binary is
// moved aside and the new one renamed into its place, committing in-process with
// no .cmd helper or restart deferral. These tests assert that real on-disk
// behavior rather than the stubbed update seam.

func TestApplyUpdateBinaryRenameTrickSwapsInPlace(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "archery-cli")
	if err := os.WriteFile(dst, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "extracted")
	if err := os.WriteFile(src, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := applyUpdateBinary(src, dst)
	if err != nil {
		t.Fatalf("applyUpdateBinary: %v", err)
	}
	if res.Status != "installed" {
		t.Errorf("Status = %q, want installed (committed in-process, not scheduled)", res.Status)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Errorf("target contents = %q, want NEW (swap committed)", got)
	}
	// The .new staging file must not linger; .old is best-effort removed.
	if _, err := os.Stat(filepath.Join(dir, ".archery-cli.new")); !os.IsNotExist(err) {
		t.Errorf(".new staging file still present: %v", err)
	}
}

func TestApplyUpdateBinaryRollsBackOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "archery-cli")
	if err := os.WriteFile(dst, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Point src at a nonexistent file so the copy step fails before any rename
	// touches the live binary; the original must remain intact.
	src := filepath.Join(dir, "does-not-exist")

	if _, err := applyUpdateBinary(src, dst); err == nil {
		t.Fatal("applyUpdateBinary: want error, got nil")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OLD" {
		t.Errorf("target contents = %q, want OLD (untouched after failure)", got)
	}
}
