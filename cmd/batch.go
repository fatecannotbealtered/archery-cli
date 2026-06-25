package cmd

import (
	"errors"
	"time"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/config"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

// errBatch wraps a per-item failure message for the batch executor.
func errBatch(msg string) error { return errors.New(msg) }

// errorCodeForAPIErr maps an API/transport error to a machine error code so a
// per-item failure carries the same taxonomy as the top-level envelope (§6).
func errorCodeForAPIErr(err error) output.ErrorCode {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return output.ErrorCodeFromStatus(apiErr.StatusCode)
	}
	return output.E_NETWORK
}

// exitForErrorCode maps an error code to its semantic process exit code. Delegates
// to the canonical contract (internal/contract) so it cannot drift from the fleet's
// E_* -> exit table, and automatically covers E_CONFIRMATION_REQUIRED, E_UNKNOWN,
// and any ext codes without manual case additions.
func exitForErrorCode(code output.ErrorCode) int {
	return output.ExitCodeForErrorCode(code)
}

// This file holds the shared client-side batch contract (CLI-SPEC §15). Every
// archery batch command is class B: Archery's upstream has no native bulk write
// endpoint (the lone exception is diagnostic kill --threads), so we loop over
// single-target calls client-side. The external contract is identical to a
// class-A native batch — plural inputs, one dry-run summary, one confirm token
// over the whole resolved set, per-item aggregation, and --continue-on-error —
// so an agent cannot and need not tell A from B. Atomicity is NOT promised:
// a partial failure leaves already-applied items applied (no rollback).

// batchItemResult is one entry in a batch result's items[] (CLI-SPEC §15.5).
// Target is the natural key of the input (instance name, workflow id, …), never
// an array index, so an agent can zip results back to inputs deterministically.
type batchItemResult struct {
	Target  string         `json:"target"`
	OK      bool           `json:"ok"`
	Data    map[string]any `json:"data,omitempty"`
	Error   *batchItemErr  `json:"error,omitempty"`
	Skipped bool           `json:"skipped,omitempty"`
}

// batchItemErr mirrors the top-level error taxonomy {code, retryable} (§6).
type batchItemErr struct {
	Code      output.ErrorCode `json:"code"`
	Message   string           `json:"message"`
	Retryable bool             `json:"retryable"`
}

// batchSummary is the always-present {total, succeeded, failed} tally (§15.5).
// Skipped counts targets not attempted because --continue-on-error=false
// stopped the run; total == succeeded + failed + skipped.
type batchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped,omitempty"`
}

// parsePluralTargets de-duplicates a plural flag value while preserving input
// order (§15.1). cobra's StringSlice already splits comma-separated and
// repeated forms into one slice; we only trim, drop empties, and dedup here.
func parsePluralTargets(raw []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range raw {
		v = trim(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// batchExecutor runs one target and returns its per-item data or an error code.
// A returned error stops the batch only when continueOnError is false.
type batchExecutor func(target string) (data map[string]any, code output.ErrorCode, retryable bool, err error)

// runBatch executes targets sequentially, aggregating per-item results without
// rolling back on partial failure (§15.5). When continueOnError is false it
// stops at the first failure and reports the unattempted remainder as skipped.
func runBatch(targets []string, continueOnError bool, exec batchExecutor) ([]batchItemResult, batchSummary) {
	items := make([]batchItemResult, 0, len(targets))
	summary := batchSummary{Total: len(targets)}

	stopped := false
	for _, target := range targets {
		if stopped {
			items = append(items, batchItemResult{Target: target, OK: false, Skipped: true})
			summary.Skipped++
			continue
		}
		data, code, retryable, err := exec(target)
		if err != nil {
			items = append(items, batchItemResult{
				Target: target,
				OK:     false,
				Error:  &batchItemErr{Code: code, Message: err.Error(), Retryable: retryable},
			})
			summary.Failed++
			if !continueOnError {
				// Mark the rest skipped so the agent can resume from here.
				stopped = true
			}
			continue
		}
		items = append(items, batchItemResult{Target: target, OK: true, Data: data})
		summary.Succeeded++
	}
	return items, summary
}

// batchItemsToMaps converts items[] to the []map[string]any shape PrintJSON and
// FilterMap expect, propagating any per-item _untrusted tags from data.
func batchItemsToMaps(items []batchItemResult) []map[string]any {
	out := make([]map[string]any, len(items))
	for i, it := range items {
		m := map[string]any{"target": it.Target, "ok": it.OK}
		if it.Skipped {
			m["skipped"] = true
		}
		if it.Data != nil {
			m["data"] = it.Data
		}
		if it.Error != nil {
			m["error"] = map[string]any{
				"code":      string(it.Error.Code),
				"message":   it.Error.Message,
				"retryable": it.Error.Retryable,
			}
		}
		out[i] = m
	}
	return out
}

// printBatchResult emits the aggregated batch envelope: items[] + summary (§15.5).
// Top-level ok stays true whenever the batch ran and produced a result, even with
// per-item failures; the per-item status lives in items[].
func printBatchResult(items []batchItemResult, summary batchSummary) {
	output.PrintJSON(map[string]any{
		"items": batchItemsToMaps(items),
		"summary": map[string]any{
			"total":     summary.Total,
			"succeeded": summary.Succeeded,
			"failed":    summary.Failed,
			"skipped":   summary.Skipped,
		},
	})
}

// batchDryRunOrConfirm handles the whole-batch dry-run preview and confirm token
// (§15.2–15.3). The token binds the full resolved target set plus command path +
// region context, so adding/removing a target invalidates it. It reuses the same
// single-use confirm infrastructure as single-target writes — batch adds no new
// token mechanism. Returns true if the command should return immediately.
//
// action is the human-readable verb (e.g. "audit workflows"); targets is the
// resolved, de-duplicated set; changes are the per-target preview rows.
func batchDryRunOrConfirm(action string, targets []string, changes []map[string]any) bool {
	cmdPath := action
	if activeCmd != nil {
		cmdPath = activeCmd.CommandPath()
	}

	// Read-only gate: the batch write path is a separate chokepoint from the
	// single-target one, so it must refuse writes here too.
	if readOnlyMode {
		emitError(
			"read-only mode: write commands are disabled (unset --read-only / ARCHERY_CLI_READONLY to enable writes)",
			ExitForbidden, output.E_FORBIDDEN)
		return true
	}

	// The confirm payload binds the whole resolved target set: any change to the
	// set produces a different token, rejected with E_CONFLICT on confirm.
	confirmPayload := map[string]any{
		"batch":   true,
		"targets": targets,
	}

	dangerous := requiresDangerousGate(activeCmd)
	if dangerous {
		if !dangerousMode {
			return failDangerousRequired(activeCmd)
		}
		confirmPayload = map[string]any{
			"dangerous": true,
			"operation": confirmPayload,
		}
	}

	confirmCtx := ""
	if cfg, err := config.Load(); err == nil {
		confirmCtx = confirmContext(cfg)
	}

	if dryRun {
		if jsonMode {
			preview := map[string]any{
				"action":  action,
				"total":   len(targets),
				"targets": targets,
				"changes": changes,
			}
			if dangerous {
				preview["dangerous"] = true
			}
			token, expires := newConfirmToken(cmdPath, confirmCtx, confirmPayload)
			output.PrintJSON(map[string]any{
				"preview":       preview,
				"confirm_token": token,
				"expires_at":    expires.Format(time.RFC3339),
			})
		} else {
			output.Info("[dry-run] " + action)
		}
		return true
	}

	if err := requireConfirm(activeCmd, cmdPath, confirmCtx, confirmPayload); err != nil {
		return true
	}
	return false
}

// boolDefault returns the --continue-on-error value, honoring a per-command
// default. Dangerous batches default to false (stop at first failure); declare
// the override in reference.
func continueOnErrorFlag(cmd *cobra.Command, def bool) bool {
	f := cmd.Flags().Lookup("continue-on-error")
	if f == nil || !f.Changed {
		return def
	}
	v, _ := cmd.Flags().GetBool("continue-on-error")
	return v
}
