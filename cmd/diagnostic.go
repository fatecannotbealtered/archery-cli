package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var diagnosticCmd = &cobra.Command{
	Use:   "diagnostic",
	Short: "Database diagnostic tools",
}

func init() {
	rootCmd.AddCommand(diagnosticCmd)

	// process
	diagnosticProcessCmd.Flags().String("instance", "", "Instance name (required)")
	diagnosticProcessCmd.Flags().String("command-type", "All", "Command filter: 'All', 'Not Sleep', or an exact command (e.g. Query, Sleep)")
	diagnosticProcessCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	diagnosticCmd.AddCommand(diagnosticProcessCmd)

	// kill
	diagnosticKillCmd.Flags().String("instance", "", "Instance name (required)")
	diagnosticKillCmd.Flags().String("threads", "", "Comma-separated thread IDs to kill (required)")
	diagnosticCmd.AddCommand(diagnosticKillCmd)
	markWrite(diagnosticKillCmd)
	markRiskLevel(diagnosticKillCmd, "critical")

	// tablespace
	diagnosticTablespaceCmd.Flags().String("instance", "", "Instance name (required)")
	diagnosticTablespaceCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	diagnosticCmd.AddCommand(diagnosticTablespaceCmd)

	// locks
	diagnosticLocksCmd.Flags().String("instance", "", "Instance name (required)")
	diagnosticLocksCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	diagnosticCmd.AddCommand(diagnosticLocksCmd)

	// transactions
	diagnosticTransactionsCmd.Flags().String("instance", "", "Instance name (required)")
	diagnosticTransactionsCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	diagnosticCmd.AddCommand(diagnosticTransactionsCmd)
}

// ─── process ────────────────────────────────────────────────────────────────

var diagnosticProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "List running processes on a database instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		commandType, _ := cmd.Flags().GetString("command-type")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// command_type is required server-side (it is escaped then string-formatted
		// into the WHERE clause); "All" selects every command. Default to it so an
		// omitted flag lists all processes instead of matching the empty string.
		if commandType == "" {
			commandType = "All"
		}
		form := url.Values{
			"instance_name": {instance},
			"command_type":  {commandType},
		}

		data, err := client.SessionPost(apiCtx(), "/db_diagnostic/process/", form)
		if err != nil {
			return handleAPIError(err)
		}
		if err := checkDiagnosticStatus(data); err != nil {
			return err
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
			}
			items := extractDiagnosticRows(raw)
			for _, m := range items {
				api.TagUntrusted(m, "Info", "info")
			}
			if len(fields) > 0 && len(items) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(items)
			return nil
		}

		// Text mode
		items := extractDiagnosticRows(nil)
		_ = json.Unmarshal(data, &items)
		if len(items) == 0 {
			output.Info("No running processes found.")
			return nil
		}
		printDynamicTable(items)
		return nil
	},
}

// ─── kill ───────────────────────────────────────────────────────────────────

var diagnosticKillCmd = &cobra.Command{
	Use:   "kill",
	Short: "Kill database threads by ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		threads, _ := cmd.Flags().GetString("threads")
		if threads == "" {
			return failArg("--threads is required")
		}

		// Validate thread IDs are comma-separated integers, then collect them as a
		// JSON array: the kill_session/create_kill_session views read the
		// "ThreadIDs" form field and json.loads() it, so a comma string is rejected.
		idStrings := splitCSV(threads)
		threadIDs := make([]int, 0, len(idStrings))
		for _, id := range idStrings {
			n, err := strconv.Atoi(strings.TrimSpace(id))
			if err != nil {
				return failArg(fmt.Sprintf("invalid thread ID %q: must be an integer", id))
			}
			threadIDs = append(threadIDs, n)
		}
		threadIDsJSON, _ := json.Marshal(threadIDs)

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
			"ThreadIDs":     {string(threadIDsJSON)},
		}

		// Preview the exact KILL statements via create_kill_session so the dry-run /
		// confirmation shows what will actually run. This is a read (it only builds
		// the SQL); kill_session below performs the destructive execution.
		var killPreview string
		if preview, err := client.SessionPost(apiCtx(), "/db_diagnostic/create_kill_session/", form); err == nil {
			if err := checkDiagnosticStatus(preview); err == nil {
				var pv struct {
					Data string `json:"data"`
				}
				_ = json.Unmarshal(preview, &pv)
				killPreview = pv.Data
			}
		}

		detail := map[string]any{
			"instance": instance,
			"threads":  threadIDs,
		}
		if killPreview != "" {
			detail["kill_sql"] = killPreview
		}
		if markDryRunOrConfirm("kill database threads", detail) {
			return nil
		}

		data, err := client.SessionPost(apiCtx(), "/db_diagnostic/kill_session/", form)
		if err != nil {
			return handleAPIError(err)
		}
		if err := checkDiagnosticStatus(data); err != nil {
			return err
		}

		if jsonMode {
			result := map[string]any{
				"instance": instance,
				"threads":  threadIDs,
				"killed":   len(threadIDs),
			}
			if killPreview != "" {
				result["kill_sql"] = killPreview
			}
			tagCommonUntrusted(result)
			output.PrintJSON(result)
			return nil
		}

		output.Success(fmt.Sprintf("Kill request sent for %d thread(s) on %s", len(threadIDs), instance))
		return nil
	},
}

// ─── tablespace ─────────────────────────────────────────────────────────────

var diagnosticTablespaceCmd = &cobra.Command{
	Use:   "tablespace",
	Short: "Show tablespace usage for a database instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
		}

		// NOTE: the v1.8.5 route is the misspelled "tablesapce" (sql/urls.py +
		// db_diagnostic.tablesapce). Matching the typo is required; do not "fix" it.
		data, err := client.SessionPost(apiCtx(), "/db_diagnostic/tablesapce/", form)
		if err != nil {
			return handleAPIError(err)
		}
		if err := checkDiagnosticStatus(data); err != nil {
			return err
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
			}
			items := extractDiagnosticRows(raw)
			if len(fields) > 0 && len(items) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(items)
			return nil
		}

		// Text mode
		items := extractDiagnosticRows(nil)
		_ = json.Unmarshal(data, &items)
		if len(items) == 0 {
			output.Info("No tablespace data found.")
			return nil
		}
		printDynamicTable(items)
		return nil
	},
}

// ─── locks ──────────────────────────────────────────────────────────────────

var diagnosticLocksCmd = &cobra.Command{
	Use:   "locks",
	Short: "Show lock wait information",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
		}

		data, err := client.SessionPost(apiCtx(), "/db_diagnostic/trxandlocks/", form)
		if err != nil {
			return handleAPIError(err)
		}
		if err := checkDiagnosticStatus(data); err != nil {
			return err
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
			}
			items := extractDiagnosticRows(raw)
			if len(fields) > 0 && len(items) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(items)
			return nil
		}

		// Text mode
		items := extractDiagnosticRows(nil)
		_ = json.Unmarshal(data, &items)
		if len(items) == 0 {
			output.Info("No lock waits found.")
			return nil
		}
		printDynamicTable(items)
		return nil
	},
}

// ─── transactions ───────────────────────────────────────────────────────────

var diagnosticTransactionsCmd = &cobra.Command{
	Use:   "transactions",
	Short: "Show long running InnoDB transactions",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
		}

		data, err := client.SessionPost(apiCtx(), "/db_diagnostic/innodb_trx/", form)
		if err != nil {
			return handleAPIError(err)
		}
		if err := checkDiagnosticStatus(data); err != nil {
			return err
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
			}
			items := extractDiagnosticRows(raw)
			if len(fields) > 0 && len(items) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(items)
			return nil
		}

		// Text mode
		items := extractDiagnosticRows(nil)
		_ = json.Unmarshal(data, &items)
		if len(items) == 0 {
			output.Info("No long running transactions found.")
			return nil
		}
		printDynamicTable(items)
		return nil
	},
}

// ─── helpers ────────────────────────────────────────────────────────────────

// diagnosticEnvelope is the common shape returned by every db_diagnostic view:
// {"status":0,"msg":"ok","rows":[...]} on success, {"status":1,"msg":"..."} on
// failure (e.g. RBAC "你所在组未关联该实例" or a database error). These views
// return HTTP 200 even for status==1, so the status field — not the HTTP code —
// is the source of truth.
type diagnosticEnvelope struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Rows   json.RawMessage `json:"rows"`
}

// checkDiagnosticStatus decodes the envelope and returns a CLI error when the
// view reported status!=0. The raw body is returned to callers for row parsing.
func checkDiagnosticStatus(data []byte) error {
	var env diagnosticEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return failWithCode("parsing response: "+err.Error(), output.E_SERVER)
	}
	if env.Status != 0 {
		msg := strings.TrimSpace(env.Msg)
		if msg == "" {
			msg = "diagnostic request failed"
		}
		return failWithCode(msg, output.E_VALIDATION)
	}
	return nil
}

// extractDiagnosticRows extracts a list of row maps from Archery internal
// API responses. Handles both the {"status":0,"data":{"rows":[...]}} shape
// and direct array/object responses.
func extractDiagnosticRows(raw any) []map[string]any {
	if raw == nil {
		return nil
	}

	// Direct array
	if arr, ok := raw.([]any); ok {
		items := make([]map[string]any, 0, len(arr))
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok {
				items = append(items, normalizeAgentMap(m))
			}
		}
		return items
	}

	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	// {"status":0,"data":{"rows":[...]}}
	if data, ok := m["data"]; ok {
		if dm, ok := data.(map[string]any); ok {
			if rows, ok := dm["rows"]; ok {
				return extractDiagnosticRows(rows)
			}
		}
		// data is directly an array
		if arr, ok := data.([]any); ok {
			return extractDiagnosticRows(arr)
		}
	}

	// {"rows":[...]}
	if rows, ok := m["rows"]; ok {
		return extractDiagnosticRows(rows)
	}

	// DRF paginated {"results":[...]}
	if results, ok := m["results"]; ok {
		return extractDiagnosticRows(results)
	}

	return nil
}

// printDynamicTable prints a list of maps as a table, using the keys from
// the first row as headers.
func printDynamicTable(items []map[string]any) {
	if len(items) == 0 {
		output.Info("No data found.")
		return
	}

	// Collect headers from first item in stable order
	var headers []string
	for k := range items[0] {
		headers = append(headers, strings.ToUpper(k))
	}

	rows := make([][]string, len(items))
	for i, m := range items {
		cells := make([]string, len(headers))
		for j, h := range headers {
			key := strings.ToLower(h)
			for mk, mv := range m {
				if strings.ToLower(mk) == key {
					cells[j] = fmt.Sprintf("%v", mv)
					break
				}
			}
		}
		rows[i] = cells
	}
	output.Table(headers, rows)
}
