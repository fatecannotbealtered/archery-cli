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
	diagnosticProcessCmd.Flags().String("command-type", "", "Filter by command type (e.g. Query, Sleep)")
	diagnosticProcessCmd.Flags().Int("limit", 0, "Max results (0 = no limit)")
	diagnosticProcessCmd.Flags().Int("offset", 0, "Pagination offset")
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
	diagnosticTablespaceCmd.Flags().Int("limit", 0, "Max results (0 = no limit)")
	diagnosticTablespaceCmd.Flags().Int("offset", 0, "Pagination offset")
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
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
		}
		if commandType != "" {
			form.Set("command_type", commandType)
		}
		if limit > 0 {
			form.Set("limit", strconv.Itoa(limit))
		}
		if offset > 0 {
			form.Set("offset", strconv.Itoa(offset))
		}

		data, err := client.InternalPost(apiCtx(), "/db_diagnostic/process/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
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

		// Validate thread IDs are comma-separated integers
		threadIDs := splitCSV(threads)
		for _, id := range threadIDs {
			if _, err := strconv.Atoi(strings.TrimSpace(id)); err != nil {
				return failArg(fmt.Sprintf("invalid thread ID %q: must be an integer", id))
			}
		}

		detail := map[string]any{
			"instance": instance,
			"threads":  threads,
		}
		if markDryRunOrConfirm("kill database threads", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
			"thread_ids":    {threads},
		}

		data, err := client.InternalPost(apiCtx(), "/db_diagnostic/kill_session/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
			}
			output.PrintJSON(map[string]any{
				"instance": instance,
				"threads":  threadIDs,
				"result":   raw,
			})
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
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
		}
		if limit > 0 {
			form.Set("limit", strconv.Itoa(limit))
		}
		if offset > 0 {
			form.Set("offset", strconv.Itoa(offset))
		}

		data, err := client.InternalPost(apiCtx(), "/db_diagnostic/tablespace/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
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

		data, err := client.InternalPost(apiCtx(), "/db_diagnostic/trxandlocks/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
			}
			if len(fields) > 0 {
				if m, ok := raw.(map[string]any); ok {
					output.PrintJSON(output.FilterMap(m, fields))
					return nil
				}
			}
			output.PrintJSON(raw)
			return nil
		}

		// Text mode: try to extract tabular data
		items := extractDiagnosticRows(nil)
		_ = json.Unmarshal(data, &items)
		if len(items) == 0 {
			// Print raw as summary
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err == nil {
				for k, v := range raw {
					fmt.Printf("  %-20s %v\n", output.FormatGray(k), v)
				}
			} else {
				output.Info("No lock information found.")
			}
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

		data, err := client.InternalPost(apiCtx(), "/db_diagnostic/innodb_trx/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitError, output.E_SERVER)
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
				items = append(items, m)
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
