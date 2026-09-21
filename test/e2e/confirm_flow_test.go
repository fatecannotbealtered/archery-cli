package e2e

import (
	"strings"
	"testing"
)

// TestDryRun_TextModeEmitsConfirmToken guards the gap that made every write
// command unusable in --format text: the dry-run printed the action and
// nothing else, so the two-step flow it instructs the caller to follow had no
// token to follow it with. The token, and the preview of what is being
// approved, must both be there.
func TestDryRun_TextModeEmitsConfirmToken(t *testing.T) {
	r := runCLI(t.TempDir(), nil, "--format", "text", "--dry-run",
		"workflow", "submit", "--name", "n", "--instance-name", "inst1",
		"--group-name", "ops", "--db", "db1", "--sql", "alter table t add c int")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "--confirm ct_") {
		t.Fatalf("text dry-run carries no confirm token:\n%s", r.Stdout)
	}
	// The preview has to name what is being approved, not just the verb.
	for _, want := range []string{"instanceName", "groupName", "db1"} {
		if !strings.Contains(r.Stdout, want) {
			t.Fatalf("text dry-run preview omits %q:\n%s", want, r.Stdout)
		}
	}
}

// TestAuthLogin_TextDryRunDoesNotPromptForPassword covers the second half of
// the same dead end: auth login collected the password before reaching the
// gate, so a dry-run took a secret and threw it away — which reads to the
// operator like the password was rejected. A dry-run issues no request, so it
// must not ask.
func TestAuthLogin_TextDryRunDoesNotPromptForPassword(t *testing.T) {
	r := runCLI(t.TempDir(), map[string]string{"ARCHERY_CLI_PASSWORD": ""},
		"--format", "text", "--dry-run",
		"auth", "login", "--url", mockURL, "--username", "tester", "--region", "probe")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	if strings.Contains(r.Stdout, "Password:") {
		t.Fatalf("dry-run prompted for a password it cannot use:\n%s", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "--confirm ct_") {
		t.Fatalf("auth login dry-run carries no confirm token:\n%s", r.Stdout)
	}
}

// TestAuthLogin_JSONDryRunNeedsNoPassword is the json-mode counterpart: the
// dry-run used to demand --password, which pushed the secret into argv (where
// other processes and the shell history can read it) purely to obtain a token
// for a step that sends nothing.
func TestAuthLogin_JSONDryRunNeedsNoPassword(t *testing.T) {
	r := runCLI(t.TempDir(), map[string]string{"ARCHERY_CLI_PASSWORD": ""},
		"--format", "json", "--dry-run",
		"auth", "login", "--url", mockURL, "--username", "tester", "--region", "probe")
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0\nstdout: %s", r.ExitCode, r.Stdout)
	}
	data := decodeOK(t, r)
	if tok, _ := data["confirm_token"].(string); !strings.HasPrefix(tok, "ct_") {
		t.Fatalf("no confirm_token in json dry-run: %s", r.Stdout)
	}
}

// TestAuthLogin_ConfirmStillNeedsAPassword is the other side of the same
// change: skipping the password is a property of the dry-run only.
func TestAuthLogin_ConfirmStillNeedsAPassword(t *testing.T) {
	home := t.TempDir()
	dry := runCLI(home, map[string]string{"ARCHERY_CLI_PASSWORD": ""},
		"--format", "json", "--dry-run",
		"auth", "login", "--url", mockURL, "--username", "tester", "--region", "probe")
	token, _ := decodeOK(t, dry)["confirm_token"].(string)
	if token == "" {
		t.Fatalf("no token from dry-run: %s", dry.Stdout)
	}

	r := runCLI(home, map[string]string{"ARCHERY_CLI_PASSWORD": ""},
		"--format", "json", "--confirm", token,
		"auth", "login", "--url", mockURL, "--username", "tester", "--region", "probe")
	if r.ExitCode == 0 {
		t.Fatalf("confirm without a password should fail, got exit 0: %s", r.Stdout)
	}
	env := decodeEnvelope(t, r.Stdout)
	if env.Error == nil || !strings.Contains(env.Error.Message, "password") {
		t.Fatalf("error should name the missing password, got: %s", r.Stdout)
	}
}
