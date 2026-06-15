package cmd

import (
	"fmt"
	"strings"

	"github.com/fatecannotbealtered/archery-cli/internal/api"
	"github.com/fatecannotbealtered/archery-cli/internal/output"
	"github.com/spf13/cobra"
)

var binlogCmd = &cobra.Command{
	Use:   "binlog",
	Short: "Manage and parse MySQL binlog files",
}

func init() {
	rootCmd.AddCommand(binlogCmd)

	// binlog list
	binlogListCmd.Flags().String("instance", "", "Instance name (required)")
	binlogCmd.AddCommand(binlogListCmd)

	// binlog parse
	binlogParseCmd.Flags().String("instance", "", "Instance name (required)")
	binlogParseCmd.Flags().String("start-file", "", "Start binlog file name")
	binlogParseCmd.Flags().String("end-file", "", "End binlog file name")
	binlogParseCmd.Flags().Int("start-pos", 0, "Start position")
	binlogParseCmd.Flags().Int("end-pos", 0, "End position")
	binlogParseCmd.Flags().String("start-time", "", "Start time (YYYY-MM-DD HH:MM:SS)")
	binlogParseCmd.Flags().String("end-time", "", "End time (YYYY-MM-DD HH:MM:SS)")
	binlogParseCmd.Flags().String("schemas", "", "Filter by schema names (comma-separated)")
	binlogParseCmd.Flags().StringSlice("tables", nil, "Filter by table names")
	binlogParseCmd.Flags().StringSlice("sql-types", nil, "Filter by SQL types (e.g. INSERT,UPDATE,DELETE)")
	binlogParseCmd.Flags().Int("num", 0, "Max number of statements to return (server default 30)")
	binlogParseCmd.Flags().Bool("rollback", false, "Generate rollback SQL")
	binlogParseCmd.Flags().Bool("save-sql", false, "Save SQL to file on server")
	binlogParseCmd.Flags().Int("threads", 0, "Number of parsing threads")
	binlogCmd.AddCommand(binlogParseCmd)
	markWrite(binlogParseCmd)
	markRiskLevel(binlogParseCmd, "medium")

	// binlog purge
	binlogPurgeCmd.Flags().String("instance", "", "Instance ID (required)")
	binlogPurgeCmd.Flags().String("binlog", "", "Binlog file name to purge (required)")
	binlogCmd.AddCommand(binlogPurgeCmd)
	markWrite(binlogPurgeCmd)
	markRiskLevel(binlogPurgeCmd, "high")
}

// ─── binlog list ───────────────────────────────────────────────────────────

var binlogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List binlog files for an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		res, err := client.Binlog.List(apiCtx(), instance)
		if err != nil {
			return handleAPIError(err)
		}
		if res.Status != 0 {
			return failArg(binlogMsg(res.Msg, "listing binlogs failed"))
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"instance": instance,
				"data":     normalizeAgentValue(res.Data),
			})
			return nil
		}

		if len(res.Data) == 0 {
			output.Info("No binlog files found.")
			return nil
		}
		// Column set varies by MySQL version; derive headers from the first row.
		var headers []string
		for k := range res.Data[0] {
			headers = append(headers, k)
		}
		rows := make([][]string, len(res.Data))
		for i, row := range res.Data {
			cells := make([]string, len(headers))
			for j, h := range headers {
				if v, ok := row[h]; ok {
					cells[j] = fmt.Sprintf("%v", v)
				}
			}
			rows[i] = cells
		}
		upper := make([]string, len(headers))
		for i, h := range headers {
			upper[i] = strings.ToUpper(h)
		}
		output.Table(upper, rows)
		return nil
	},
}

// ─── binlog parse ──────────────────────────────────────────────────────────

var binlogParseCmd = &cobra.Command{
	Use:   "parse",
	Short: "Parse binlog to SQL (supports rollback SQL generation)",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}

		startFile, _ := cmd.Flags().GetString("start-file")
		endFile, _ := cmd.Flags().GetString("end-file")
		startPos, _ := cmd.Flags().GetInt("start-pos")
		endPos, _ := cmd.Flags().GetInt("end-pos")
		startTime, _ := cmd.Flags().GetString("start-time")
		stopTime, _ := cmd.Flags().GetString("end-time")
		schemas, _ := cmd.Flags().GetString("schemas")
		tables, _ := cmd.Flags().GetStringSlice("tables")
		sqlTypes, _ := cmd.Flags().GetStringSlice("sql-types")
		num, _ := cmd.Flags().GetInt("num")
		rollback, _ := cmd.Flags().GetBool("rollback")
		saveSQL, _ := cmd.Flags().GetBool("save-sql")
		threads, _ := cmd.Flags().GetInt("threads")

		schemaList := nonEmptyCSV(schemas)

		detail := map[string]any{
			"instance": instance,
			"rollback": rollback,
		}
		if startFile != "" {
			detail["startFile"] = startFile
		}
		if endFile != "" {
			detail["endFile"] = endFile
		}
		if startPos > 0 {
			detail["startPos"] = startPos
		}
		if endPos > 0 {
			detail["endPos"] = endPos
		}
		if startTime != "" {
			detail["startTime"] = startTime
		}
		if stopTime != "" {
			detail["stopTime"] = stopTime
		}
		if len(schemaList) > 0 {
			detail["schemas"] = schemaList
		}
		if len(tables) > 0 {
			detail["tables"] = tables
		}
		if len(sqlTypes) > 0 {
			detail["sqlTypes"] = sqlTypes
		}
		if num > 0 {
			detail["num"] = num
		}
		if saveSQL {
			detail["saveSql"] = true
		}
		if threads > 0 {
			detail["threads"] = threads
		}
		if markDryRunOrConfirm("parse binlog", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		res, err := client.Binlog.Parse(apiCtx(), api.BinlogParseParams{
			InstanceName: instance,
			StartFile:    startFile,
			EndFile:      endFile,
			StartPos:     startPos,
			EndPos:       endPos,
			StartTime:    startTime,
			StopTime:     stopTime,
			Schemas:      schemaList,
			Tables:       tables,
			SQLTypes:     sqlTypes,
			Num:          num,
			Threads:      threads,
			Rollback:     rollback,
			SaveSQL:      saveSQL,
		})
		if err != nil {
			return handleAPIError(err)
		}
		if res.Status != 0 {
			return failArg(binlogMsg(res.Msg, "parsing binlog failed"))
		}

		rows, err := res.Rows()
		if err != nil {
			return handleAPIError(err)
		}
		sqls := make([]string, len(rows))
		for i, row := range rows {
			sqls[i] = row.SQL
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"instance":   instance,
				"rollback":   rollback,
				"count":      len(sqls),
				"sqls":       sqls,
				"_untrusted": []string{"sqls"},
			})
			return nil
		}

		if len(sqls) == 0 {
			output.Info("No SQL statements found in binlog.")
			return nil
		}
		for _, s := range sqls {
			fmt.Println(s)
		}
		fmt.Printf("\n%d SQL statements\n", len(sqls))
		return nil
	},
}

// ─── binlog purge ──────────────────────────────────────────────────────────

var binlogPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge a binlog file from an instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		binlogFile, _ := cmd.Flags().GetString("binlog")
		if binlogFile == "" {
			return failArg("--binlog is required")
		}

		detail := map[string]any{
			"instance": instance,
			"binlog":   binlogFile,
		}
		if markDryRunOrConfirm("purge binlog", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		res, err := client.Binlog.Purge(apiCtx(), instance, binlogFile)
		if err != nil {
			return handleAPIError(err)
		}
		// The view returns status 1 (no binlog given) or 2 (purge SQL failed)
		// on failure; only 0 is success.
		if res.Status != 0 {
			return failArg(binlogMsg(res.Msg, "purging binlog failed"))
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"purged":   true,
				"instance": instance,
				"binlog":   binlogFile,
			})
			return nil
		}

		output.Success(fmt.Sprintf("Binlog %s purged from instance %s", binlogFile, instance))
		return nil
	},
}

// binlogMsg returns the server-provided message, or a fallback when the server
// left it empty.
func binlogMsg(msg, fallback string) string {
	if strings.TrimSpace(msg) == "" {
		return fallback
	}
	return msg
}

// nonEmptyCSV splits a comma-separated list and drops empty segments so the
// my2sql view's getlist('only_schemas') never receives a stray "".
func nonEmptyCSV(s string) []string {
	var out []string
	for _, part := range splitCSV(s) {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}
