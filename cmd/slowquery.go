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
	slowqueryReviewCmd.Flags().String("start", "", "Start date YYYY-MM-DD (required; a datetime is truncated to the date)")
	slowqueryReviewCmd.Flags().String("end", "", "End date YYYY-MM-DD, inclusive (required; a datetime is truncated to the date)")
	slowqueryReviewCmd.Flags().String("db", "", "Database name filter")
	slowqueryReviewCmd.Flags().Int("limit", 20, "Max results (1-500)")
	slowqueryReviewCmd.Flags().Int("offset", 0, "Pagination offset")
	slowqueryReviewCmd.Flags().String("search", "", "Search text in SQL fingerprint")
	slowqueryReviewCmd.Flags().String("sort", "MySQLTotalExecutionCounts", "Sort field (upstream column name)")
	slowqueryReviewCmd.Flags().String("order", "desc", "Sort order: asc|desc")
	slowqueryReviewCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	slowqueryCmd.AddCommand(slowqueryReviewCmd)

	// history
	slowqueryHistoryCmd.Flags().String("instance", "", "Instance name (required)")
	slowqueryHistoryCmd.Flags().String("start", "", "Start date YYYY-MM-DD (required; a datetime is truncated to the date)")
	slowqueryHistoryCmd.Flags().String("end", "", "End date YYYY-MM-DD, inclusive (required; a datetime is truncated to the date)")
	slowqueryHistoryCmd.Flags().String("sql-id", "", "Slow query SQL id (checksum) filter")
	slowqueryHistoryCmd.Flags().String("db", "", "Database name filter")
	slowqueryHistoryCmd.Flags().Int("limit", 20, "Max results (1-500)")
	slowqueryHistoryCmd.Flags().Int("offset", 0, "Pagination offset")
	slowqueryHistoryCmd.Flags().String("search", "", "Search text in SQL sample")
	slowqueryHistoryCmd.Flags().String("sort", "ExecutionStartTime", "Sort field (upstream column name)")
	slowqueryHistoryCmd.Flags().String("order", "desc", "Sort order: asc|desc")
	slowqueryHistoryCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	slowqueryCmd.AddCommand(slowqueryHistoryCmd)

	// optimize
	slowqueryOptimizeCmd.Flags().String("instance", "", "Instance name (required)")
	slowqueryOptimizeCmd.Flags().String("db", "", "Database name (required)")
	slowqueryOptimizeCmd.Flags().String("sql", "", "SQL to optimize (required)")
	slowqueryOptimizeCmd.Flags().String("tool", "soar", "Optimization tool: sqladvisor|soar|tuning")
	slowqueryOptimizeCmd.Flags().Bool("verbose", false, "sqladvisor verbose output")
	slowqueryOptimizeCmd.Flags().StringSlice("option", []string{"sys_parm", "sql_plan", "obj_stat"}, "tuning options: sys_parm,sql_plan,obj_stat,sql_profile")
	slowqueryOptimizeCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	slowqueryCmd.AddCommand(slowqueryOptimizeCmd)
}

// validateSlowqueryPaging guards --limit/--offset on the list commands.
func validateSlowqueryPaging(limit, offset int) error {
	if limit < 1 || limit > 500 {
		return failArg("--limit must be between 1 and 500")
	}
	if offset < 0 {
		return failArg("--offset must be >= 0")
	}
	return nil
}

// normalizeSlowqueryOrder lowercases and validates the sort direction flag.
func normalizeSlowqueryOrder(order string) (string, error) {
	order = strings.ToLower(strings.TrimSpace(order))
	if order != "" && order != "asc" && order != "desc" {
		return "", failArg("--order must be asc or desc")
	}
	return order, nil
}

// slowqueryDate truncates a time argument to its YYYY-MM-DD prefix. The v1.8.5
// slowquery views parse EndTime with strptime(..., '%Y-%m-%d') and feed StartTime
// into a date range filter, so a full datetime ("... 23:59:59") makes the server
// 500 with "unconverted data remains". Accepting the friendlier datetime form and
// sending only the date keeps the CLI ergonomic without tripping the view.
func slowqueryDate(arg string) string {
	arg = strings.TrimSpace(arg)
	if i := strings.IndexByte(arg, ' '); i >= 0 {
		return arg[:i]
	}
	if i := strings.IndexByte(arg, 'T'); i >= 0 {
		return arg[:i]
	}
	return arg
}

// ─── review ─────────────────────────────────────────────────────────────────

var slowqueryReviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review aggregated slow query statistics",
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

		if err := validateSlowqueryPaging(limit, offset); err != nil {
			return err
		}
		order, err := normalizeSlowqueryOrder(order)
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// v1.8.5 slowquery_review reads PascalCase time params (StartTime/EndTime)
		// plus sortName/sortOrder; db_name and search default to empty matches.
		form := url.Values{
			"instance_name": {instance},
			"StartTime":     {slowqueryDate(start)},
			"EndTime":       {slowqueryDate(end)},
			"db_name":       {db},
			"search":        {search},
			"limit":         {strconv.Itoa(limit)},
			"offset":        {strconv.Itoa(offset)},
			"sortName":      {sort},
			"sortOrder":     {order},
		}

		data, err := client.SessionPost(apiCtx(), "/slowquery/review/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp slowqueryReviewResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse slowquery review response: " + err.Error())
		}
		// 服务端权限/校验失败走 {status:1,msg} 而非 {total,rows}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(resp.Rows))
			for i, r := range resp.Rows {
				m := slowqueryReviewRowToMap(r)
				api.TagUntrusted(m, "sql_text")
				if len(fields) > 0 {
					m = output.FilterMap(m, fields)
				}
				items[i] = m
			}
			output.PrintJSON(offsetListEnvelope(items, offset, len(items), resp.Total))
			return nil
		}

		if len(resp.Rows) == 0 {
			output.Info("No slow queries found.")
			return nil
		}

		headers := []string{"SQL_ID", "DB", "TOTAL", "AVG_TIME", "ROWS_EXAMINED_AVG", "ROWS_SENT_AVG", "LAST_SEEN"}
		rows := make([][]string, len(resp.Rows))
		for i, r := range resp.Rows {
			rows[i] = []string{
				r.SQLId,
				r.DBName,
				formatSlowCount(r.MySQLTotalExecutionCounts),
				formatSlowFloat(r.QueryTimeAvg),
				strconv.Itoa(r.ParseRowAvg),
				strconv.Itoa(r.ReturnRowAvg),
				r.CreateTime,
			}
		}
		output.Table(headers, rows)
		fmt.Printf("\n%d slow queries, total %d\n", len(resp.Rows), resp.Total)
		return nil
	},
}

// ─── history ────────────────────────────────────────────────────────────────

var slowqueryHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show execution detail history of slow queries",
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
		db, _ := cmd.Flags().GetString("db")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")
		sort, _ := cmd.Flags().GetString("sort")
		order, _ := cmd.Flags().GetString("order")

		if err := validateSlowqueryPaging(limit, offset); err != nil {
			return err
		}
		order, err := normalizeSlowqueryOrder(order)
		if err != nil {
			return err
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// v1.8.5 slowquery_review_history reads StartTime/EndTime and an optional
		// SQLId (checksum) + db_name filter; both are non-required.
		form := url.Values{
			"instance_name": {instance},
			"StartTime":     {slowqueryDate(start)},
			"EndTime":       {slowqueryDate(end)},
			"db_name":       {db},
			"SQLId":         {sqlID},
			"search":        {search},
			"limit":         {strconv.Itoa(limit)},
			"offset":        {strconv.Itoa(offset)},
			"sortName":      {sort},
			"sortOrder":     {order},
		}

		data, err := client.SessionPost(apiCtx(), "/slowquery/review_history/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp slowqueryHistoryResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse slowquery history response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			items := make([]map[string]any, len(resp.Rows))
			for i, r := range resp.Rows {
				m := slowqueryHistoryRowToMap(r)
				api.TagUntrusted(m, "sql_text")
				if len(fields) > 0 {
					m = output.FilterMap(m, fields)
				}
				items[i] = m
			}
			env := offsetListEnvelope(items, offset, len(items), resp.Total)
			if sqlID != "" {
				env["sql_id"] = sqlID
			}
			output.PrintJSON(env)
			return nil
		}

		if len(resp.Rows) == 0 {
			output.Info("No execution history found.")
			return nil
		}

		headers := []string{"START_TIME", "DB", "COUNTS", "QUERY_TIME_95", "QUERY_TIMES", "LOCK_TIMES", "ROWS_EXAMINED", "ROWS_SENT"}
		rows := make([][]string, len(resp.Rows))
		for i, r := range resp.Rows {
			rows[i] = []string{
				r.ExecutionStartTime,
				r.DBName,
				formatSlowCount(r.TotalExecutionCounts),
				formatSlowFloat(r.QueryTimePct95),
				formatSlowFloat(r.QueryTimes),
				formatSlowFloat(r.LockTimes),
				formatSlowCount(r.ParseRowCounts),
				formatSlowCount(r.ReturnRowCounts),
			}
		}
		output.Table(headers, rows)
		fmt.Printf("\n%d history entries, total %d\n", len(resp.Rows), resp.Total)
		return nil
	},
}

// ─── optimize ───────────────────────────────────────────────────────────────

// slowqueryOptimizeResponse is the raw API response from optimize endpoints.
// sqladvisor/soar return data as a string report; sqltuning returns a nested
// object, so data stays json.RawMessage and is decoded per-tool.
type slowqueryOptimizeResponse struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
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
		options, _ := cmd.Flags().GetStringSlice("option")

		tool = strings.ToLower(strings.TrimSpace(tool))

		// 每个工具的端点和表单字段名都不同（v1.8.5 view 实测）：
		// sqladvisor 用 sql_content + verbose；soar 用 sql；tuning 用 sql_content + option[]
		var apiPath string
		form := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
		}
		switch tool {
		case "sqladvisor":
			apiPath = "/slowquery/optimize_sqladvisor/"
			form.Set("sql_content", sql)
			if verbose {
				form.Set("verbose", "1")
			} else {
				form.Set("verbose", "0")
			}
		case "soar":
			apiPath = "/slowquery/optimize_soar/"
			form.Set("sql", sql)
		case "tuning":
			apiPath = "/slowquery/optimize_sqltuning/"
			form.Set("sql_content", sql)
			for _, opt := range options {
				form.Add("option[]", opt)
			}
		default:
			return failArg("--tool must be one of: sqladvisor, soar, tuning")
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		data, err := client.SessionPost(apiCtx(), apiPath, form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp slowqueryOptimizeResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse optimize response: " + err.Error())
		}
		if resp.Status != 0 && resp.Msg != "" && resp.Msg != "ok" {
			return failArg(resp.Msg)
		}

		result := decodeOptimizeData(resp.Data)

		if jsonMode {
			fields := getFieldsFlag(cmd)
			m := map[string]any{
				"tool":   tool,
				"result": result,
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
		// sqladvisor/soar 返回纯文本报告，直接打印；tuning 返回结构体则回退 JSON
		if s, ok := result.(string); ok {
			fmt.Println(s)
		} else {
			output.PrintJSON(map[string]any{"tool": tool, "result": result})
		}
		return nil
	},
}

// decodeOptimizeData unwraps the optimize endpoints' polymorphic `data` field:
// sqladvisor/soar emit a string report, sqltuning emits a nested object. A null
// or empty payload degrades to an empty string so callers always get a value.
func decodeOptimizeData(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	return string(raw)
}

// formatSlowFloat renders a slow-log duration/avg without trailing zero noise.
func formatSlowFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// formatSlowCount renders a count that arrives as a float (Sum over FloatField,
// e.g. 42.0) as a plain integer when whole, falling back to the trimmed float
// form for the rare fractional aggregate.
func formatSlowCount(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ─── response shapes (v1.8.5 wire) ──────────────────────────────────────────

// slowqueryReviewResponse is the slowquery_review wire shape: a bare
// {total, rows} on success, or {status, msg} on a permission/validation failure.
type slowqueryReviewResponse struct {
	Status int                  `json:"status"`
	Msg    string               `json:"msg"`
	Total  int                  `json:"total"`
	Rows   []slowqueryReviewRow `json:"rows"`
}

// slowqueryReviewRow mirrors the annotated columns from slowquery_review: the
// aggregated per-fingerprint stats use PascalCase keys. The Sum() aggregates
// over FloatField columns (ts_cnt/rows_*) come back as floats on the wire
// (e.g. 42.0), so the count totals are decoded as float64; only the *Avg pair
// is int()-cast server-side. SQLId is the checksum bigint serialized as a
// string (bigint_as_string=True).
type slowqueryReviewRow struct {
	SQLId                     string  `json:"SQLId"`
	SQLText                   string  `json:"SQLText"`
	DBName                    string  `json:"DBName"`
	CreateTime                string  `json:"CreateTime"`
	QueryTimeAvg              float64 `json:"QueryTimeAvg"`
	MySQLTotalExecutionCounts float64 `json:"MySQLTotalExecutionCounts"`
	MySQLTotalExecutionTimes  float64 `json:"MySQLTotalExecutionTimes"`
	ParseTotalRowCounts       float64 `json:"ParseTotalRowCounts"`
	ReturnTotalRowCounts      float64 `json:"ReturnTotalRowCounts"`
	ParseRowAvg               int     `json:"ParseRowAvg"`
	ReturnRowAvg              int     `json:"ReturnRowAvg"`
}

func slowqueryReviewRowToMap(r slowqueryReviewRow) map[string]any {
	return normalizeAgentMap(map[string]any{
		"sql_id":              r.SQLId,
		"sql_text":            r.SQLText,
		"db_name":             r.DBName,
		"last_seen":           r.CreateTime,
		"query_time_avg":      r.QueryTimeAvg,
		"mysql_total":         r.MySQLTotalExecutionCounts,
		"total_query_time":    r.MySQLTotalExecutionTimes,
		"rows_examined_total": r.ParseTotalRowCounts,
		"rows_sent_total":     r.ReturnTotalRowCounts,
		"rows_examined_avg":   r.ParseRowAvg,
		"rows_sent_avg":       r.ReturnRowAvg,
	})
}

// slowqueryHistoryResponse is the slowquery_review_history wire shape.
type slowqueryHistoryResponse struct {
	Status int                   `json:"status"`
	Msg    string                `json:"msg"`
	Total  int                   `json:"total"`
	Rows   []slowqueryHistoryRow `json:"rows"`
}

// slowqueryHistoryRow mirrors the annotated columns from
// slowquery_review_history: per-occurrence execution detail. ts_cnt/rows_*
// map directly from FloatField columns, so the counts arrive as floats
// (e.g. 42.0) and are decoded as float64.
type slowqueryHistoryRow struct {
	ExecutionStartTime   string  `json:"ExecutionStartTime"`
	DBName               string  `json:"DBName"`
	HostAddress          string  `json:"HostAddress"`
	SQLText              string  `json:"SQLText"`
	TotalExecutionCounts float64 `json:"TotalExecutionCounts"`
	QueryTimePct95       float64 `json:"QueryTimePct95"`
	QueryTimes           float64 `json:"QueryTimes"`
	LockTimes            float64 `json:"LockTimes"`
	ParseRowCounts       float64 `json:"ParseRowCounts"`
	ReturnRowCounts      float64 `json:"ReturnRowCounts"`
}

func slowqueryHistoryRowToMap(r slowqueryHistoryRow) map[string]any {
	return normalizeAgentMap(map[string]any{
		"execution_start_time":   r.ExecutionStartTime,
		"db_name":                r.DBName,
		"host_address":           r.HostAddress,
		"sql_text":               r.SQLText,
		"total_execution_counts": r.TotalExecutionCounts,
		"query_time_pct95":       r.QueryTimePct95,
		"query_times":            r.QueryTimes,
		"lock_times":             r.LockTimes,
		"rows_examined":          r.ParseRowCounts,
		"rows_sent":              r.ReturnRowCounts,
	})
}
