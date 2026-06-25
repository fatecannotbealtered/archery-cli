package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var dictCmd = &cobra.Command{
	Use:   "dict",
	Short: "Browse data dictionary (tables, table metadata, export)",
}

func init() {
	rootCmd.AddCommand(dictCmd)

	// dict tables
	dictTablesCmd.Flags().String("instance", "", "Instance name (required)")
	dictTablesCmd.Flags().String("db", "", "Database name (required)")
	dictTablesCmd.Flags().String("db-type", "", "Database type, e.g. mysql, mssql, oracle (required: part of the v1.8.5 instance lookup key)")
	dictTablesCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	dictCmd.AddCommand(dictTablesCmd)

	// dict table-info
	dictTableInfoCmd.Flags().String("instance", "", "Instance name (required)")
	dictTableInfoCmd.Flags().String("db", "", "Database name (required)")
	dictTableInfoCmd.Flags().String("table", "", "Table name (required)")
	dictTableInfoCmd.Flags().String("db-type", "", "Database type, e.g. mysql, mssql, oracle (required: part of the v1.8.5 instance lookup key)")
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
		// v1.8.5 looks up the instance via Instance.objects.get(instance_name,
		// db_type): db_type is part of the lookup key, so omitting it returns
		// "Instance.DoesNotExist". The flag is therefore required here.
		dbType, err := requireFlagString(cmd, "db-type", "--db-type")
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
			"db_type":       {dbType},
		}

		data, err := client.SessionGet(apiCtx(), "/data_dictionary/table_list/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		items, err := parseDictTableList(data)
		if err != nil {
			return err
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			if len(fields) > 0 {
				filtered := make([]map[string]any, len(items))
				for i, m := range items {
					filtered[i] = output.FilterMap(m, fields)
				}
				items = filtered
			}
			out := map[string]any{
				"instance": instance,
				"db":       db,
				"tables":   items,
				"count":    len(items),
			}
			tagCommonUntrusted(out)
			output.PrintJSON(out)
			return nil
		}

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
		// Like table_list, the view keys the instance lookup on db_type, so it is
		// required; omitting it yields "Instance.DoesNotExist".
		dbType, err := requireFlagString(cmd, "db-type", "--db-type")
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// The v1.8.5 view reads request.GET['tb_name'] (not table_name); sending
		// the wrong key returns "非法调用！".
		params := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
			"tb_name":       {table},
			"db_type":       {dbType},
		}

		data, err := client.SessionGet(apiCtx(), "/data_dictionary/table_info/?"+params.Encode())
		if err != nil {
			return handleAPIError(err)
		}

		info, err := parseDictTableInfo(data)
		if err != nil {
			return err
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			if len(fields) > 0 {
				info = output.FilterMap(info, fields)
			}
			out := map[string]any{
				"instance": instance,
				"db":       db,
				"table":    table,
				"info":     info,
			}
			tagCommonUntrusted(out)
			output.PrintJSON(out)
			return nil
		}

		// Text mode: print structured info as labeled column/row tables.
		fmt.Println()
		output.Bold(fmt.Sprintf("  %s.%s.%s", instance, db, table))
		output.Gray("  ────────────────────────────────────────")

		printDictColumnTable("Metadata", info["meta_data"])
		printDictColumnTable("Columns", info["desc"])
		printDictColumnTable("Indexes", info["index"])
		fmt.Println()
		return nil
	},
}

// ─── dict views ────────────────────────────────────────────────────────────

var dictViewsCmd = &cobra.Command{
	Use:   "views",
	Short: "List views in a database (requires a newer Archery)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return dictUnavailable(cmd, "views", "view_list")
	},
}

// ─── dict triggers ─────────────────────────────────────────────────────────

var dictTriggersCmd = &cobra.Command{
	Use:   "triggers",
	Short: "List triggers in a database (requires a newer Archery)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return dictUnavailable(cmd, "triggers", "trigger_list")
	},
}

// ─── dict procedures ───────────────────────────────────────────────────────

var dictProceduresCmd = &cobra.Command{
	Use:   "procedures",
	Short: "List stored procedures in a database (requires a newer Archery)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return dictUnavailable(cmd, "procedures", "procedure_list")
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

		// With db_name set the view streams an HTML FileResponse attachment; the
		// CLI requires --db, so this always returns the table-structure HTML
		// (never the superuser "exported to downloads/" JSON status branch).
		data, err := client.SessionGet(apiCtx(), "/data_dictionary/export/?"+params.Encode())
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

// dictEnvelope is the {status,msg,data} shape returned by every
// data_dictionary view. data is decoded lazily because its shape differs per
// endpoint (letter-keyed map for table_list, nested column/row tables for
// table_info).
type dictEnvelope struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

// decodeDictEnvelope parses the common envelope and maps a non-zero status to
// the server's message (e.g. "Instance.DoesNotExist", "非法调用！").
func decodeDictEnvelope(data []byte) (dictEnvelope, error) {
	var env dictEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return env, failWithCode("parsing response: "+err.Error(), output.E_SERVER)
	}
	if env.Status != 0 {
		msg := env.Msg
		if msg == "" {
			msg = "data dictionary request failed"
		}
		return env, failArg(msg)
	}
	return env, nil
}

// parseDictTableList flattens the table_list response. v1.8.5 returns
// data as a map keyed by the table name's first letter, each value a list of
// [table_name, table_comment] pairs (from get_group_tables_by_db). The result
// is a flat, name-sorted list of {name, comment} rows.
func parseDictTableList(data []byte) ([]map[string]any, error) {
	env, err := decodeDictEnvelope(data)
	if err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return nil, nil
	}

	var grouped map[string][][]any
	if err := json.Unmarshal(env.Data, &grouped); err != nil {
		return nil, failWithCode("parsing table list: "+err.Error(), output.E_SERVER)
	}

	items := make([]map[string]any, 0)
	for _, pairs := range grouped {
		for _, pair := range pairs {
			row := map[string]any{"name": "", "comment": ""}
			if len(pair) > 0 {
				row["name"] = pair[0]
			}
			if len(pair) > 1 {
				row["comment"] = pair[1]
			}
			items = append(items, row)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprintf("%v", items[i]["name"]) < fmt.Sprintf("%v", items[j]["name"])
	})
	return items, nil
}

// parseDictTableInfo decodes the table_info data payload. Each of meta_data,
// desc, and index is a {column_list, rows} table; create_sql (mysql only) is a
// list of [table_name, ddl] rows. The payload is returned as-is (keys
// preserved) for the JSON envelope, and consumed by printDictColumnTable for
// text mode.
func parseDictTableInfo(data []byte) (map[string]any, error) {
	env, err := decodeDictEnvelope(data)
	if err != nil {
		return nil, err
	}
	info := map[string]any{}
	if len(env.Data) == 0 {
		return info, nil
	}
	if err := json.Unmarshal(env.Data, &info); err != nil {
		return nil, failWithCode("parsing table info: "+err.Error(), output.E_SERVER)
	}
	return info, nil
}

// dictUnavailable fails fast for endpoints absent from Archery v1.8.5
// (view_list, trigger_list, procedure_list). Issuing the request would 404 and
// read as a transport bug, so the gate reports missing-route semantics instead.
func dictUnavailable(cmd *cobra.Command, entity, route string) error {
	// Validate the shared arg contract first so the gate is the only failure
	// reason once a newer server ships the route.
	if _, err := requireFlagString(cmd, "instance", "--instance"); err != nil {
		return err
	}
	if _, err := requireFlagString(cmd, "db", "--db"); err != nil {
		return err
	}
	return failWithCode(
		fmt.Sprintf("dict %s is not available on this Archery server (no /data_dictionary/%s/ route in v1.8.5); upgrade Archery to a version that ships it", entity, route),
		output.E_NOT_FOUND)
}

// printDictColumnTable renders a {column_list, rows} table from table_info under
// a heading. rows may be a flat list (single row) or a list of lists.
func printDictColumnTable(heading string, v any) {
	tbl, ok := v.(map[string]any)
	if !ok {
		return
	}
	cols := toStringSlice(tbl["column_list"])
	rawRows, _ := tbl["rows"].([]any)
	if len(cols) == 0 || len(rawRows) == 0 {
		return
	}

	// A single-row payload (e.g. meta_data) comes back as a flat []any rather
	// than [][]any; wrap it so both shapes render uniformly.
	var rows [][]string
	if _, nested := rawRows[0].([]any); nested {
		for _, r := range rawRows {
			rows = append(rows, toStringSlice(r))
		}
	} else {
		rows = append(rows, toStringSlice(rawRows))
	}

	fmt.Println()
	output.Gray("  " + heading)
	output.Gray("  ────────────────────────────────────────")
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = strings.ToUpper(c)
	}
	output.Table(headers, rows)
}

// toStringSlice coerces a JSON array (or scalar) into a slice of display
// strings, rendering nil as the empty string.
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		if e == nil {
			out[i] = ""
		} else {
			out[i] = fmt.Sprintf("%v", e)
		}
	}
	return out
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
