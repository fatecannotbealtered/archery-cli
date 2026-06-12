package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestWriteCommandsDeclareConfirmAndRisk(t *testing.T) {
	walkCommandTree(rootCmd, func(c *cobra.Command) {
		if c == rootCmd || c.Hidden || c.Annotations["write"] != "true" {
			return
		}
		if c.Annotations["confirm"] != "true" {
			t.Errorf("%s is write but does not require confirmation", c.CommandPath())
		}
		if c.Annotations["riskLevel"] == "" {
			t.Errorf("%s is write but has no riskLevel annotation", c.CommandPath())
		}
	})
}

func TestReferenceReportsWriteConfirmRequirement(t *testing.T) {
	tree := buildReferenceTree(rootCmd)
	walkRefCommands(tree.Commands, func(c refCommand) {
		if c.Write && !c.RequiresConfirmation {
			t.Errorf("%s is write in reference but requiresConfirmation=false", c.Path)
		}
		if c.Write && c.RiskLevel == "" {
			t.Errorf("%s is write in reference but has no riskLevel", c.Path)
		}
		if c.Write && (c.RiskLevel == "high" || c.RiskLevel == "critical") && !c.RequiresDangerous {
			t.Errorf("%s is %s risk but requiresDangerous=false", c.Path, c.RiskLevel)
		}
	})
}

func TestReferenceReportsErrorCodesAndOutputSchema(t *testing.T) {
	tree := buildReferenceTree(rootCmd)
	if tree.SecurityTier != "T2" {
		t.Fatalf("security tier = %q, want T2", tree.SecurityTier)
	}
	if tree.ReleaseReadiness.Level != "unpublishable" {
		t.Fatalf("release level = %q, want unpublishable until FCC reaches 100%%", tree.ReleaseReadiness.Level)
	}
	if tree.ReleaseReadiness.LiveSmokeStatus != "missing" {
		t.Fatalf("live smoke status = %q, want missing", tree.ReleaseReadiness.LiveSmokeStatus)
	}
	if len(tree.ErrorCodesSnake) == 0 {
		t.Fatal("reference error_codes is empty")
	}
	if _, ok := tree.ErrorCodesSnake["E_CONFIRMATION_REQUIRED"]; !ok {
		t.Fatal("reference missing E_CONFIRMATION_REQUIRED")
	}
	if tree.ErrorCodesSnake["E_NETWORK"].Retryable != true {
		t.Fatal("E_NETWORK should be retryable in reference")
	}
	walkRefCommands(tree.Commands, func(c refCommand) {
		if len(c.Commands) == 0 && c.OutputType == "json" && len(c.OutputSchema) == 0 {
			t.Errorf("%s is missing output_schema", c.Path)
		}
	})
}

func TestReleaseReadinessDoctorContract(t *testing.T) {
	readiness := buildReleaseReadiness()
	if readiness.Level != "unpublishable" || readiness.FCCStatus != "missing" {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
	if releaseReadinessCheckStatus() != "fail" {
		t.Fatalf("release readiness doctor status = %q, want fail", releaseReadinessCheckStatus())
	}
	if releaseReadinessCheckFix() == "" {
		t.Fatal("unpublishable release readiness should include a fix")
	}
}

func TestHighRiskWriteRequiresDangerousGate(t *testing.T) {
	origActiveCmd := activeCmd
	origDangerous := dangerousMode
	origDryRun := dryRun
	origJSON := jsonMode
	origExit := lastExit
	t.Cleanup(func() {
		activeCmd = origActiveCmd
		dangerousMode = origDangerous
		dryRun = origDryRun
		jsonMode = origJSON
		lastExit = origExit
	})

	activeCmd = queryRunCmd
	dangerousMode = false
	dryRun = true
	jsonMode = true
	lastExit = 0

	if !markDryRunOrConfirm("query run", map[string]any{"sql": "SELECT 1"}) {
		t.Fatal("expected high-risk write without --dangerous to stop")
	}
	if lastExit != ExitConfirm {
		t.Fatalf("lastExit = %d, want %d", lastExit, ExitConfirm)
	}
}

func walkCommandTree(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, child := range c.Commands() {
		walkCommandTree(child, fn)
	}
}

func walkRefCommands(commands []refCommand, fn func(refCommand)) {
	for _, c := range commands {
		fn(c)
		walkRefCommands(c.Commands, fn)
	}
}
