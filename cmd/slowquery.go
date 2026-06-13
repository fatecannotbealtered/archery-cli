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

var slowqueryCmd = &cobra.Command{
	Use:   "slowquery",
	Short: "Manage slow query analysis",
}

func init() {
	rootCmd.AddCommand(slowqueryCmd)

	// review
	slowqueryReviewCmd.Flags().String("instance", "", "Instance name (required)")
	slowqueryReviewCmd.Flags().String("start", "", "Start time (required, e.g. 2024-01-01 00:00:00)")
	slowqueryReviewCmd.Flags().String("end", "", "End time (required, e.g. 2024-01-31 23:59:59)")
	slowqueryReviewCmd.Flags().String("db", "", "Database name filter")
	slowqueryReviewCmd.Flags().Int("limit", 20, "Max results (1-500)")
	slowqueryReviewCmd.Flags().Int("offset", 0, "Pagination offset")
	slowqueryReviewCmd.Flags().String("search", "", "Search text in SQL")
	slowqueryReviewCmd.Flags().String("sort", "", "Sort field name")
	slowqueryReviewCmd.Flags().String("order", "desc", "Sort order: asc|desc")
	slowqueryReviewCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	slowqueryCmd.AddCommand(slowqueryReviewCmd)

	// history
	slowqueryHistoryCmd.Flags().String("instance", "", "Instance name (required)")
	slowqueryHistoryCmd.Flags().String("start", "", "Start time (required)")
	slowqueryHistoryCmd.Flags().String("end", "", "End time (required)")
	slowqueryHistoryCmd.Flags().String("sql-id", "", "Slow query SQL ID (required)")
	slowqueryHistoryCmd.Flags().Int("limit", 20, "Max results (1-500)")
	slowqueryHistoryCmd.Flags().Int("offset", 0, "Pagination offset")
	slowqueryHistoryCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	slowqueryCmd.AddCommand(slowqueryHistoryCmd)

	// optimize
	slowqueryOptimizeCmd.Flags().String("instance", "", "Instance name (required)")
	slowqueryOptimizeCmd.Flags().String("db", "", "Database name (required)")
	slowqueryOptimizeCmd.Flags().String("sql", "", "SQL to optimize (required)")
	slowqueryOptimizeCmd.Flags().String("tool", "soar", "Optimization tool: sqladvisor|soar|tuning")
	slowqueryOptimizeCmd.Flags().Bool("verbose", false, "Show detailed output")
	slowqueryOptimizeCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	slowqueryCmd.AddCommand(slowqueryOptimizeCmd)
}

// ─── review ─────────────────────────────────────────────────────────────────

// SlowqueryReviewEntry represents a single slow query review entry for output.
type SlowqueryReviewEntry struct {
	ID           int    `json:"id"`
	SQLID        string `json:"sql_id"`
	SQLText      string `json:"sql_text"`
	DBName       string `json:"db_name"`
	MySQLTotal   int    `json:"mysql_total"`
	QueryTimeAvg string `json:"query_time_avg"`
	QueryTimeMax string `json:"query_time_max"`
	LockTimeAvg  string `json:"lock_time_avg"`
	RowsExamined int    `json:"rows_examined"`
	RowsSentAvg  int    `json:"rows_sent_avg"`
	FirstSeen    string `json:"first_seen"`
	LastSeen     string `json:"last_seen"`
}

var slowqueryReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review slow query statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		start, _ := cmd.Flags().GetString("start")
		if start == "" {
			return failArg("--start is required")
		}
		end, _ := cmd.Flags().GetString("end")
		if end == "" {
			return failArg("--end is required")
		}
		db, _ := cmd.Flags().GetString("db")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")
		sort, _ := cmd.Flags().GetString("sort")
		order, _ := cmd.Flags().GetString("order")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}
		order = strings.ToLower(order)
		if order != "" && order != "asc" && order != "desc" {
			return failArg("--order must be asc or desc")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
			"start_time":    {start},
			"end_time":      {end},
		}
		if db != "" {
			form.Set("db_name", db)
		}
		form.Set("limit", strconv.Itoa(limit))
		form.Set("offset", strconv.Itoa(offset))
		if search != "" {
			form.Set("search", search)
		}
		if sort != "" {
			form.Set("sort", sort)
		}
		if order != "" {
			form.Set("order", order)
		}

		data, err := client.InternalPost(apiCtx(), "/slowquery/review/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp slowqueryInternalResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse slowquery review response: " + err.Error())
		}

		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(resp.Data.Rows))
			for i, r := range resp.Data.Rows {
				m := slowqueryRowToMap(r)
				api.TagUntrusted(m, "sql_text")
				if len(fields) > 0 {
					m = output.FilterMap(m, fields)
				}
				items[i] = m
			}
			output.PrintJSON(offsetListEnvelope(items, offset, len(items), resp.Data.Total))
			return nil
		}

		if len(resp.Data.Rows) == 0 {
			output.Info("No slow queries found.")
			return nil
		}

		headers := []string{"SQL_ID", "DB", "TOTAL", "AVG_TIME", "MAX_TIME", "ROWS_EXAMINED", "LAST_SEEN"}
		rows := make([][]string, len(resp.Data.Rows))
		for i, r := range resp.Data.Rows {
			rows[i] = []string{
				r.SQLID,
				r.DBName,
				strconv.Itoa(r.MySQLTotal),
				r.QueryTimeAvg,
				r.QueryTimeMax,
				strconv.Itoa(r.RowsExamined),
				r.LastSeen,
			}
		}
		output.Table(headers, rows)
		fmt.Printf("\n%d slow queries, total %d\n", len(resp.Data.Rows), resp.Data.Total)
		return nil
	},
}

// ─── history ────────────────────────────────────────────────────────────────

var slowqueryHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show execution history for a specific slow query",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		start, _ := cmd.Flags().GetString("start")
		if start == "" {
			return failArg("--start is required")
		}
		end, _ := cmd.Flags().GetString("end")
		if end == "" {
			return failArg("--end is required")
		}
		sqlID, _ := cmd.Flags().GetString("sql-id")
		if sqlID == "" {
			return failArg("--sql-id is required")
		}
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")

		if limit < 1 || limit > 500 {
			return failArg("--limit must be between 1 and 500")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
			"start_time":    {start},
			"end_time":      {end},
			"sql_id":        {sqlID},
		}
		form.Set("limit", strconv.Itoa(limit))
		form.Set("offset", strconv.Itoa(offset))

		data, err := client.InternalPost(apiCtx(), "/slowquery/review_history/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp slowqueryInternalResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse slowquery history response: " + err.Error())
		}

		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(resp.Data.Rows))
			for i, r := range resp.Data.Rows {
				m := slowqueryRowToMap(r)
				api.TagUntrusted(m, "sql_text")
				if len(fields) > 0 {
					m = output.FilterMap(m, fields)
				}
				items[i] = m
			}
			env := offsetListEnvelope(items, offset, len(items), resp.Data.Total)
			env["sql_id"] = sqlID
			output.PrintJSON(env)
			return nil
		}

		if len(resp.Data.Rows) == 0 {
			output.Info("No execution history found.")
			return nil
		}

		headers := []string{"ID", "START_TIME", "QUERY_TIME", "LOCK_TIME", "ROWS_EXAMINED", "ROWS_SENT"}
		rows := make([][]string, len(resp.Data.Rows))
		for i, r := range resp.Data.Rows {
			rows[i] = []string{
				strconv.Itoa(r.ID),
				r.FirstSeen,
				r.QueryTimeAvg,
				r.LockTimeAvg,
				strconv.Itoa(r.RowsExamined),
				strconv.Itoa(r.RowsSentAvg),
			}
		}
		output.Table(headers, rows)
		fmt.Printf("\n%d history entries\n", len(resp.Data.Rows))
		return nil
	},
}

// ─── optimize ───────────────────────────────────────────────────────────────

// SlowqueryOptimizeOutput is the structured output for optimization results.
type SlowqueryOptimizeOutput struct {
	Tool   string `json:"tool"`
	Result string `json:"result"`
}

// slowqueryOptimizeResponse is the raw API response from optimize endpoints.
type slowqueryOptimizeResponse struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
	Data   string `json:"data"`
}

var slowqueryOptimizeCmd = &cobra.Command{
	Use:   "optimize",
	Short: "Get SQL optimization suggestions",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		db, _ := cmd.Flags().GetString("db")
		if db == "" {
			return failArg("--db is required")
		}
		sql, _ := cmd.Flags().GetString("sql")
		if sql == "" {
			return failArg("--sql is required")
		}
		tool, _ := cmd.Flags().GetString("tool")
		verbose, _ := cmd.Flags().GetBool("verbose")

		tool = strings.ToLower(tool)
		var apiPath string
		switch tool {
		case "sqladvisor":
			apiPath = "/slowquery/optimize_sqladvisor/"
		case "soar":
			apiPath = "/slowquery/optimize_soar/"
		case "tuning":
			apiPath = "/slowquery/optimize_sqltuning/"
		default:
			return failArg("--tool must be one of: sqladvisor, soar, tuning")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
			"sql":           {sql},
		}

		data, err := client.InternalPost(apiCtx(), apiPath, form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp slowqueryOptimizeResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse optimize response: " + err.Error())
		}

		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		out := SlowqueryOptimizeOutput{
			Tool:   tool,
			Result: resp.Data,
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			m := map[string]any{
				"tool":   out.Tool,
				"result": out.Result,
			}
			tagCommonUntrusted(m)
			if len(fields) > 0 {
				output.PrintJSON(output.FilterMap(m, fields))
			} else {
				output.PrintJSON(m)
			}
			return nil
		}

		if verbose {
			output.Bold(fmt.Sprintf("  Tool: %s", tool))
			fmt.Println()
		}
		fmt.Println(resp.Data)
		return nil
	},
}

// ─── shared types ───────────────────────────────────────────────────────────

// slowqueryInternalResponse is the Archery internal API response shape for
// slowquery review and history endpoints.
type slowqueryInternalResponse struct {
	Status int               `json:"status"`
	Msg    string            `json:"msg"`
	Data   slowqueryPageData `json:"data"`
}

type slowqueryPageData struct {
	Total int               `json:"total"`
	Rows  []slowqueryRowRaw `json:"rows"`
}

type slowqueryRowRaw struct {
	ID           int    `json:"id"`
	SQLID        string `json:"sql_id"`
	SQLText      string `json:"sql_text"`
	DBName       string `json:"db_name"`
	MySQLTotal   int    `json:"mysql_total"`
	QueryTimeAvg string `json:"query_time_avg"`
	QueryTimeMax string `json:"query_time_max"`
	LockTimeAvg  string `json:"lock_time_avg"`
	RowsExamined int    `json:"rows_examined"`
	RowsSentAvg  int    `json:"rows_sent_avg"`
	FirstSeen    string `json:"first_seen"`
	LastSeen     string `json:"last_seen"`
}

func slowqueryRowToMap(r slowqueryRowRaw) map[string]any {
	return normalizeAgentMap(map[string]any{
		"id":             strconv.Itoa(r.ID),
		"sql_id":         r.SQLID,
		"sql_text":       r.SQLText,
		"db_name":        r.DBName,
		"mysql_total":    r.MySQLTotal,
		"query_time_avg": r.QueryTimeAvg,
		"query_time_max": r.QueryTimeMax,
		"lock_time_avg":  r.LockTimeAvg,
		"rows_examined":  r.RowsExamined,
		"rows_sent_avg":  r.RowsSentAvg,
		"first_seen":     r.FirstSeen,
		"last_seen":      r.LastSeen,
	})
}
