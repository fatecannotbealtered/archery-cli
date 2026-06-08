package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var dictCmd = &cobra.Command{
	Use:   "dict",
	Short: "Browse data dictionary (tables, views, triggers, procedures)",
}

func init() {
	rootCmd.AddCommand(dictCmd)

	// dict tables
	dictTablesCmd.Flags().String("instance", "", "Instance name (required)")
	dictTablesCmd.Flags().String("db", "", "Database name (required)")
	dictTablesCmd.Flags().String("db-type", "", "Database type (e.g. mysql, postgresql)")
	dictTablesCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	dictCmd.AddCommand(dictTablesCmd)

	// dict table-info
	dictTableInfoCmd.Flags().String("instance", "", "Instance name (required)")
	dictTableInfoCmd.Flags().String("db", "", "Database name (required)")
	dictTableInfoCmd.Flags().String("table", "", "Table name (required)")
	dictTableInfoCmd.Flags().String("db-type", "", "Database type (e.g. mysql, postgresql)")
	dictTableInfoCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	dictCmd.AddCommand(dictTableInfoCmd)

	// dict views
	dictViewsCmd.Flags().String("instance", "", "Instance name (required)")
	dictViewsCmd.Flags().String("db", "", "Database name (required)")
	dictViewsCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	dictCmd.AddCommand(dictViewsCmd)

	// dict triggers
	dictTriggersCmd.Flags().String("instance", "", "Instance name (required)")
	dictTriggersCmd.Flags().String("db", "", "Database name (required)")
	dictTriggersCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	dictCmd.AddCommand(dictTriggersCmd)

	// dict procedures
	dictProceduresCmd.Flags().String("instance", "", "Instance name (required)")
	dictProceduresCmd.Flags().String("db", "", "Database name (required)")
	dictProceduresCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	dictCmd.AddCommand(dictProceduresCmd)

	// dict export
	dictExportCmd.Flags().String("instance", "", "Instance name (required)")
	dictExportCmd.Flags().String("db", "", "Database name (required)")
	dictCmd.AddCommand(dictExportCmd)
	markOutputFormats(dictExportCmd, formatJSON, formatText, formatRaw)
}

// ─── dict tables ───────────────────────────────────────────────────────────

var dictTablesCmd = &cobra.Command{
	Use:   "tables",
	Short: "List tables in a database",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, err := requireFlagString(cmd, "instance", "--instance")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		dbType, _ := cmd.Flags().GetString("db-type")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
		}
		if dbType != "" {
			params.Set("db_type", dbType)
		}

		data, err := client.InternalGet(apiCtx(), "/data_dictionary/table_list/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			items := extractDictRows(raw)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(map[string]any{
				"instance": instance,
				"db":       db,
				"tables":   items,
				"count":    len(items),
			})
			return nil
		}

		items := extractDictRowsFromBytes(data)
		if len(items) == 0 {
			output.Info("No tables found.")
			return nil
		}
		printDictTable(items, "TABLE")
		return nil
	},
}

// ─── dict table-info ───────────────────────────────────────────────────────

var dictTableInfoCmd = &cobra.Command{
	Use:   "table-info",
	Short: "Show table metadata, description, and indexes",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, err := requireFlagString(cmd, "instance", "--instance")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}
		table, err := requireFlagString(cmd, "table", "--table")
		if err != nil {
			return err
		}
		dbType, _ := cmd.Flags().GetString("db-type")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
			"table_name":    {table},
		}
		if dbType != "" {
			params.Set("db_type", dbType)
		}

		data, err := client.InternalGet(apiCtx(), "/data_dictionary/table_info/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			result := extractDictData(raw)
			if result != nil && len(fields) > 0 {
				if m, ok := result.(map[string]any); ok {
					result = output.FilterMap(m, fields)
				}
			}
			out := map[string]any{
				"instance": instance,
				"db":       db,
				"table":    table,
				"info":     result,
			}
			tagCommonUntrusted(out)
			output.PrintJSON(out)
			return nil
		}

		// Text mode: print structured info
		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
			Data   struct {
				Meta    map[string]any   `json:"meta"`
				Desc    string           `json:"desc"`
				Indexes []map[string]any `json:"indexes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		fmt.Println()
		output.Bold(fmt.Sprintf("  %s.%s.%s", instance, db, table))
		output.Gray("  ────────────────────────────────────────")

		if resp.Data.Desc != "" {
			printDictField("Description", resp.Data.Desc)
		}
		if len(resp.Data.Meta) > 0 {
			for k, v := range resp.Data.Meta {
				printDictField(k, fmt.Sprintf("%v", v))
			}
		}
		if len(resp.Data.Indexes) > 0 {
			fmt.Println()
			output.Gray("  Indexes")
			output.Gray("  ────────────────────────────────────────")
			for _, idx := range resp.Data.Indexes {
				name := fmt.Sprintf("%v", idx["name"])
				cols := fmt.Sprintf("%v", idx["columns"])
				unique := ""
				if u, ok := idx["unique"]; ok && u == true {
					unique = " (unique)"
				}
				fmt.Printf("  %s  %s%s\n", output.FormatCyan(name), cols, unique)
			}
		}
		fmt.Println()
		return nil
	},
}

// ─── dict views ────────────────────────────────────────────────────────────

var dictViewsCmd = &cobra.Command{
	Use:   "views",
	Short: "List views in a database",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, err := requireFlagString(cmd, "instance", "--instance")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
		}

		data, err := client.InternalGet(apiCtx(), "/data_dictionary/view_list/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			items := extractDictRows(raw)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(map[string]any{
				"instance": instance,
				"db":       db,
				"views":    items,
				"count":    len(items),
			})
			return nil
		}

		items := extractDictRowsFromBytes(data)
		if len(items) == 0 {
			output.Info("No views found.")
			return nil
		}
		printDictTable(items, "VIEW")
		return nil
	},
}

// ─── dict triggers ─────────────────────────────────────────────────────────

var dictTriggersCmd = &cobra.Command{
	Use:   "triggers",
	Short: "List triggers in a database",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, err := requireFlagString(cmd, "instance", "--instance")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
		}

		data, err := client.InternalGet(apiCtx(), "/data_dictionary/trigger_list/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			items := extractDictRows(raw)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(map[string]any{
				"instance": instance,
				"db":       db,
				"triggers": items,
				"count":    len(items),
			})
			return nil
		}

		items := extractDictRowsFromBytes(data)
		if len(items) == 0 {
			output.Info("No triggers found.")
			return nil
		}
		printDictTable(items, "TRIGGER")
		return nil
	},
}

// ─── dict procedures ───────────────────────────────────────────────────────

var dictProceduresCmd = &cobra.Command{
	Use:   "procedures",
	Short: "List stored procedures in a database",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, err := requireFlagString(cmd, "instance", "--instance")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
		}

		data, err := client.InternalGet(apiCtx(), "/data_dictionary/procedure_list/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			items := extractDictRows(raw)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			output.PrintJSON(map[string]any{
				"instance":   instance,
				"db":         db,
				"procedures": items,
				"count":      len(items),
			})
			return nil
		}

		items := extractDictRowsFromBytes(data)
		if len(items) == 0 {
			output.Info("No stored procedures found.")
			return nil
		}
		printDictTable(items, "PROCEDURE")
		return nil
	},
}

// ─── dict export ───────────────────────────────────────────────────────────

var dictExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export data dictionary as HTML",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, err := requireFlagString(cmd, "instance", "--instance")
		if err != nil {
			return err
		}
		db, err := requireFlagString(cmd, "db", "--db")
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
		}

		data, err := client.InternalGet(apiCtx(), "/data_dictionary/export/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		// Export returns raw HTML
		if formatMode == formatRaw || formatMode == formatText {
			fmt.Print(string(data))
			return nil
		}

		// JSON mode: wrap HTML in envelope
		result := map[string]any{
			"instance": instance,
			"db":       db,
			"format":   "html",
			"content":  string(data),
		}
		tagCommonUntrusted(result)
		output.PrintJSON(result)
		return nil
	},
}

// ─── helpers ───────────────────────────────────────────────────────────────

// extractDictRows extracts row items from internal API response formats.
func extractDictRows(raw any) []map[string]any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		// Try internal: {data: {rows: [...]}}
		if data, ok := m["data"]; ok {
			if dm, ok := data.(map[string]any); ok {
				if rows, ok := dm["rows"]; ok {
					return extractRows(rows)
				}
			}
		}
		// Try flat: {rows: [...]}
		if rows, ok := m["rows"]; ok {
			return extractRows(rows)
		}
		// Try: {data: [...]}
		if data, ok := m["data"]; ok {
			if arr, ok := data.([]any); ok {
				items := make([]map[string]any, 0, len(arr))
				for _, v := range arr {
					if item, ok := v.(map[string]any); ok {
						items = append(items, normalizeAgentMap(item))
					}
				}
				return items
			}
		}
	}
	// Try direct array
	if arr, ok := raw.([]any); ok {
		items := make([]map[string]any, 0, len(arr))
		for _, v := range arr {
			if item, ok := v.(map[string]any); ok {
				items = append(items, normalizeAgentMap(item))
			}
		}
		return items
	}
	return nil
}

// extractDictRowsFromBytes parses raw JSON bytes into row items.
func extractDictRowsFromBytes(data []byte) []map[string]any {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return extractDictRows(raw)
}

// extractDictData extracts the data payload from internal API response.
func extractDictData(raw any) any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		if data, ok := m["data"]; ok {
			return normalizeAgentValue(data)
		}
	}
	return normalizeAgentValue(raw)
}

// printDictField prints a labeled field for text mode detail output.
func printDictField(label, value string) {
	if value != "" {
		fmt.Printf("  %-14s %s\n", output.FormatGray(label), value)
	}
}

// printDictTable prints a generic list of dictionary items as a table.
func printDictTable(items []map[string]any, entityLabel string) {
	if len(items) == 0 {
		output.Info("No " + strings.ToLower(entityLabel) + "s found.")
		return
	}

	// Collect keys from the first item as headers
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
