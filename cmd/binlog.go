package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
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

		form := url.Values{
			"instance_name": {instance},
		}

		data, err := client.InternalPost(apiCtx(), "/binlog/list/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			output.PrintJSON(normalizeAgentValue(raw))
			return nil
		}

		// Text mode
		var resp struct {
			Status int              `json:"status"`
			Msg    string           `json:"msg"`
			Data   []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse binlog list response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}
		if len(resp.Data) == 0 {
			output.Info("No binlog files found.")
			return nil
		}
		var headers []string
		for k := range resp.Data[0] {
			headers = append(headers, strings.ToUpper(k))
		}
		rows := make([][]string, len(resp.Data))
		for i, row := range resp.Data {
			cells := make([]string, len(headers))
			for j, h := range headers {
				key := strings.ToLower(h)
				if v, ok := row[key]; ok {
					cells[j] = fmt.Sprintf("%v", v)
				}
			}
			rows[i] = cells
		}
		output.Table(headers, rows)
		return nil
	},
}

// ─── binlog parse ──────────────────────────────────────────────────────────

// BinlogParseOutput is the structured output for binlog parse results.
type BinlogParseOutput struct {
	SQLs     []string `json:"sqls,omitempty"`
	Rollback bool     `json:"rollback"`
	FullSQLs []string `json:"full_sqls,omitempty"`
}

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
		endTime, _ := cmd.Flags().GetString("end-time")
		schemas, _ := cmd.Flags().GetString("schemas")
		tables, _ := cmd.Flags().GetStringSlice("tables")
		sqlTypes, _ := cmd.Flags().GetStringSlice("sql-types")
		rollback, _ := cmd.Flags().GetBool("rollback")
		saveSQL, _ := cmd.Flags().GetBool("save-sql")
		threads, _ := cmd.Flags().GetInt("threads")

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
		if endTime != "" {
			detail["endTime"] = endTime
		}
		if schemas != "" {
			detail["schemas"] = schemas
		}
		if len(tables) > 0 {
			detail["tables"] = tables
		}
		if len(sqlTypes) > 0 {
			detail["sqlTypes"] = sqlTypes
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

		form := url.Values{
			"instance_name": {instance},
		}
		if startFile != "" {
			form.Set("start_file", startFile)
		}
		if endFile != "" {
			form.Set("end_file", endFile)
		}
		if startPos > 0 {
			form.Set("start_pos", fmt.Sprintf("%d", startPos))
		}
		if endPos > 0 {
			form.Set("end_pos", fmt.Sprintf("%d", endPos))
		}
		if startTime != "" {
			form.Set("start_time", startTime)
		}
		if endTime != "" {
			form.Set("end_time", endTime)
		}
		if schemas != "" {
			form.Set("schemas", schemas)
		}
		if len(tables) > 0 {
			form.Set("tables", strings.Join(tables, ","))
		}
		if len(sqlTypes) > 0 {
			form.Set("sql_type", strings.Join(sqlTypes, ","))
		}
		if rollback {
			form.Set("rollback", "true")
		}
		if saveSQL {
			form.Set("save_sql", "true")
		}
		if threads > 0 {
			form.Set("threads", fmt.Sprintf("%d", threads))
		}

		data, err := client.InternalPost(apiCtx(), "/binlog/my2sql/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
			if m, ok := raw.(map[string]any); ok {
				if dataMap, ok := m["data"].(map[string]any); ok {
					api.TagUntrusted(dataMap, "sqls", "full_sqls")
				}
			}
			output.PrintJSON(normalizeAgentValue(raw))
			return nil
		}

		// Text mode
		var resp struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
			Data   struct {
				SQLs     []string `json:"sqls"`
				FullSQLs []string `json:"full_sqls"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse binlog response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		sqls := resp.Data.SQLs
		if len(sqls) == 0 {
			sqls = resp.Data.FullSQLs
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

		form := url.Values{
			"instance_id": {instance},
			"binlog":      {binlogFile},
		}

		data, err := client.InternalPost(apiCtx(), "/binlog/del_log/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			var raw any
			if err := json.Unmarshal(data, &raw); err != nil {
				return failWithCode("parsing response: "+err.Error(), ExitNetwork, output.E_SERVER)
			}
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
