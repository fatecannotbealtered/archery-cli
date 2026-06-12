package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFCC_EveryLeafCommandHasTest enforces the FCC gate behind
// release_readiness.fcc_status: every leaf command declared by `reference`
// must be invoked by at least one command-level test. A zero leaf count fails
// the guard itself, so a reference schema change cannot silently disarm it.
func TestFCC_EveryLeafCommandHasTest(t *testing.T) {
	leaves, declared := fccReferenceFacts(t)
	if declared != "verified" {
		t.Skipf("fcc_status is declared %q; this guard enforces enumeration only for a"+
			" \"verified\" claim - flipping the claim without command-level tests fails the guard", declared)
	}
	if len(leaves) == 0 {
		t.Fatal("reference enumerated zero leaf commands; the FCC guard would be a no-op")
	}
	sources := fccReadTestSources(t)
	missing := 0
	for _, leaf := range leaves {
		if !fccLeafCoveredInTests(leaf, sources) {
			missing++
			t.Errorf("no command-level test invokes %q", leaf)
		}
	}
	t.Logf("FCC guard: %d leaf commands, %d uncovered", len(leaves), missing)
}

// fccReferenceFacts black-boxes everything from one `reference` run: the leaf
// command list and the declared fcc_status, so the guard needs no internal
// symbols and stays identical across tools.
func fccReferenceFacts(t *testing.T) ([]string, string) {
	t.Helper()
	cmd := exec.Command("go", "run", "./cmd/archery-cli", "reference", "--format", "json")
	cmd.Dir = fccModuleRoot()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running reference: %v", err)
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("parsing reference envelope: %v", err)
	}
	if !envelope.OK {
		t.Fatal("reference returned ok=false")
	}
	declared := ""
	if rr, ok := envelope.Data["release_readiness"].(map[string]any); ok {
		declared, _ = rr["fcc_status"].(string)
	}
	items, _ := envelope.Data["commands"].([]any)
	var leaves []string
	var walk func(map[string]any)
	walk = func(item map[string]any) {
		var children []any
		for _, key := range []string{"commands", "subcommands", "children"} {
			if v, ok := item[key].([]any); ok && len(v) > 0 {
				children = v
				break
			}
		}
		if len(children) == 0 {
			id, _ := item["path"].(string)
			if id == "" {
				id, _ = item["name"].(string)
			}
			id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "archery-cli"))
			if id != "" {
				leaves = append(leaves, id)
			}
			return
		}
		for _, c := range children {
			if m, ok := c.(map[string]any); ok {
				walk(m)
			}
		}
	}
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			walk(m)
		}
	}
	return leaves, declared
}

func fccReadTestSources(t *testing.T) string {
	t.Helper()
	root := fccModuleRoot()
	var b strings.Builder
	for _, pattern := range []string{
		filepath.Join(root, "cmd", "*_test.go"),
		filepath.Join(root, "test", "*", "*_test.go"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		for _, p := range matches {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read %s: %v", p, err)
			}
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	// Collapse whitespace so wrapped argument lists still match the needle.
	return strings.Join(strings.Fields(b.String()), " ")
}

// fccLeafCoveredInTests requires the full command-word sequence as an
// adjacent quoted argument list ("issue", "get"), the form produced by both
// rootCmd.SetArgs and runRoot-style helpers. Positional placeholders in the
// reference path (e.g. <iid>) are stripped before matching.
func fccLeafCoveredInTests(leaf, sources string) bool {
	var parts []string
	for _, p := range strings.Fields(leaf) {
		if strings.HasPrefix(p, "<") || strings.HasPrefix(p, "[") {
			break
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return false
	}
	needle := `"` + strings.Join(parts, `", "`) + `"`
	return strings.Contains(sources, needle)
}

func fccModuleRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}
