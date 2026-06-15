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

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Execute and manage SQL queries",
}

func init() {
	rootCmd.AddCommand(queryCmd)

	// run
	queryRunCmd.Flags().String("instance", "", "Instance name (compatibility alias of --instances; deprecated)")
	queryRunCmd.Flags().StringSlice("instances", nil, "Instance names to run the SQL against (comma-separated or repeatable; batch)")
	queryRunCmd.Flags().String("db", "", "Database name (required)")
	queryRunCmd.Flags().String("sql", "", "SQL to execute (required)")
	queryRunCmd.Flags().Int("limit", 0, "Row limit (0 = server default)")
	queryRunCmd.Flags().String("table", "", "Table name (for context)")
	queryRunCmd.Flags().String("schema", "", "Schema name")
	queryRunCmd.Flags().Bool("continue-on-error", true, "Keep running after an instance fails (batch; default true)")
	queryRunCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	queryCmd.AddCommand(queryRunCmd)
	markWrite(queryRunCmd)
	markRiskLevel(queryRunCmd, "high")
	markOutputFormats(queryRunCmd, formatJSON, formatText, formatRaw)

	// explain
	queryExplainCmd.Flags().String("instance", "", "Instance name (required)")
	queryExplainCmd.Flags().String("db", "", "Database name (required)")
	queryExplainCmd.Flags().String("sql", "", "SQL to explain (required)")
	queryExplainCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	queryCmd.AddCommand(queryExplainCmd)
	markOutputFormats(queryExplainCmd, formatJSON, formatText)

	// log
	queryLogCmd.Flags().Int("limit", 20, "Max results (1-100)")
	queryLogCmd.Flags().Int("offset", 0, "Offset for pagination")
	queryLogCmd.Flags().String("search", "", "Search query text")
	queryLogCmd.Flags().Bool("star", false, "Show only starred queries")
	queryLogCmd.Flags().String("start", "", "Start date (YYYY-MM-DD)")
	queryLogCmd.Flags().String("end", "", "End date (YYYY-MM-DD)")
	queryLogCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	queryCmd.AddCommand(queryLogCmd)

	// favorite
	queryFavoriteCmd.Flags().Bool("star", true, "Star (true) or unstar (false)")
	queryFavoriteCmd.Flags().String("alias", "", "Alias for the query")
	queryCmd.AddCommand(queryFavoriteCmd)
	markWrite(queryFavoriteCmd)
	markRiskLevel(queryFavoriteCmd, "medium")

	// generate
	queryGenerateCmd.Flags().String("instance", "", "Instance name (required)")
	queryGenerateCmd.Flags().String("db", "", "Database name (required)")
	queryGenerateCmd.Flags().String("table", "", "Table name (required)")
	queryGenerateCmd.Flags().String("desc", "", "Description of desired query (required)")
	queryGenerateCmd.Flags().String("db-type", "", "Database type (e.g. mysql, postgresql)")
	queryGenerateCmd.Flags().String("schema", "", "Schema name")
	queryGenerateCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	queryCmd.AddCommand(queryGenerateCmd)
}

// ─── run ────────────────────────────────────────────────────────────────────

// QueryRunOutput is the structured output for query run results.
type QueryRunOutput struct {
	Columns     []string `json:"columns"`
	Rows        [][]any  `json:"rows"`
	RowCount    int      `json:"row_count"`
	QueryTimeMS int      `json:"query_time_ms"`
	Masked      bool     `json:"masked"`
}

// queryRunResponse is the raw API response from POST /query/. Archery wraps the
// result in {status, msg, data:{...}}: the outer status/msg report whether the
// request was accepted, while the row payload and any per-query DB error live
// under data.
type queryRunResponse struct {
	Status int               `json:"status"`
	Msg    string            `json:"msg"`
	Data   queryRunResultSet `json:"data"`
}

// queryRunResultSet is the nested data object of POST /query/.
type queryRunResultSet struct {
	ColumnList   []string `json:"column_list"`
	Rows         [][]any  `json:"rows"`
	AffectedRows int      `json:"affected_rows"`
	QueryTime    float64  `json:"query_time"`
	IsMasked     bool     `json:"is_masked"`
	Error        string   `json:"error"`
}

var queryRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute a SQL query (single instance, or batch across --instances)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve the target set: --instances is the plural form; --instance is a
		// deprecated single-value compatibility alias. The plural flag, when set,
		// drives the batch envelope even for one target (CLI-SPEC §15.1).
		single, _ := cmd.Flags().GetString("instance")
		plural, _ := cmd.Flags().GetStringSlice("instances")
		pluralSet := cmd.Flags().Changed("instances")

		db, _ := cmd.Flags().GetString("db")
		if db == "" {
			return failArg("--db is required")
		}
		sql, _ := cmd.Flags().GetString("sql")
		if sql == "" {
			return failArg("--sql is required")
		}
		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 0 {
			return failArg("--limit must be >= 0")
		}
		table, _ := cmd.Flags().GetString("table")
		schema, _ := cmd.Flags().GetString("schema")

		if pluralSet {
			return runQueryBatch(cmd, parsePluralTargets(plural), db, sql, limit, table, schema)
		}

		// Legacy single-instance path: same envelope as before for backwards compat.
		if single == "" {
			return failArg("--instance or --instances is required")
		}

		detail := map[string]any{"instance": single, "db": db, "sql": sql}
		if limit > 0 {
			detail["limit"] = limit
		}
		if table != "" {
			detail["table"] = table
		}
		if schema != "" {
			detail["schema"] = schema
		}
		if markDryRunOrConfirm("query run", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		out, errMsg, code := runQueryOnInstance(client, single, db, sql, limit, table, schema)
		if errMsg != "" {
			return failWithCode(errMsg, exitForErrorCode(code), code)
		}

		if jsonMode {
			outMap := queryRunOutputToMap(out)
			if fields := getFieldsFlag(cmd); len(fields) > 0 {
				outMap = output.FilterMap(outMap, fields)
			}
			output.PrintJSON(outMap)
			return nil
		}

		// text/raw format
		if formatMode == formatRaw {
			printRawQueryResult(out.Columns, out.Rows)
			return nil
		}

		printTextQueryResult(out)
		return nil
	},
}

// runQueryOnInstance executes the SQL against one instance and returns either the
// structured result or an error message + code. Shared by the single-instance
// path and the batch loop so both speak the identical contract.
func runQueryOnInstance(client *api.Client, instance, db, sql string, limit int, table, schema string) (QueryRunOutput, string, output.ErrorCode) {
	// Archery's /query/ view reads sql_content/limit_num/tb_name (not sql/limit/
	// table_name); a mismatched name lands the value in None and the view rejects
	// the request with "页面提交参数可能为空". limit_num is in the view's not-None
	// check, so always send it.
	form := url.Values{
		"instance_name": {instance},
		"db_name":       {db},
		"sql_content":   {sql},
		"limit_num":     {strconv.Itoa(limit)},
	}
	if table != "" {
		form.Set("tb_name", table)
	}
	if schema != "" {
		form.Set("schema_name", schema)
	}

	data, err := client.InternalPost(apiCtx(), "/query/", form)
	if err != nil {
		return QueryRunOutput{}, err.Error(), errorCodeForAPIErr(err)
	}

	var resp queryRunResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return QueryRunOutput{}, "failed to parse query response: " + err.Error(), output.E_SERVER
	}
	// Archery signals request-level errors with {"status":1,"msg":"..."} (e.g.
	// RBAC "你所在组未关联该实例" or "页面提交参数可能为空"). Surface msg so
	// per-instance failures aren't swallowed as empty-row successes in a batch.
	if resp.Status != 0 {
		return QueryRunOutput{}, resp.Msg, output.E_VALIDATION
	}
	// A query accepted by Archery can still fail at the database; that error is
	// reported inside data.error, not the outer status.
	if resp.Data.Error != "" {
		return QueryRunOutput{}, resp.Data.Error, output.E_VALIDATION
	}

	return QueryRunOutput{
		Columns:     resp.Data.ColumnList,
		Rows:        resp.Data.Rows,
		RowCount:    len(resp.Data.Rows),
		QueryTimeMS: int(resp.Data.QueryTime * 1000),
		Masked:      resp.Data.IsMasked,
	}, "", ""
}

// queryRunOutputToMap builds the per-result JSON map, tagging rows as untrusted
// (raw database data may carry injection payloads, SEC-SPEC §2).
func queryRunOutputToMap(out QueryRunOutput) map[string]any {
	m := map[string]any{
		"columns":       out.Columns,
		"rows":          out.Rows,
		"row_count":     out.RowCount,
		"query_time_ms": out.QueryTimeMS,
		"masked":        out.Masked,
	}
	api.TagUntrusted(m, "rows")
	return m
}

// runQueryBatch runs one SQL across many instances, grouped per instance, failing
// soft (CLI-SPEC §15). This is a class-B client loop: Archery has no native
// cross-instance read, so results are NOT atomic and per-instance status lives in
// items[]. query run is high risk → the --dangerous gate applies to the batch.
func runQueryBatch(cmd *cobra.Command, instances []string, db, sql string, limit int, table, schema string) error {
	if len(instances) == 0 {
		return failArg("--instances must list at least one instance")
	}

	changes := make([]map[string]any, len(instances))
	for i, inst := range instances {
		changes[i] = map[string]any{"action": "query", "instance": inst, "db": db}
	}
	if batchDryRunOrConfirm("query run", instances, changes) {
		return nil
	}

	client, _, _, err := newClient()
	if err != nil {
		return err
	}

	continueOnError := continueOnErrorFlag(cmd, true)
	fields := getFieldsFlag(cmd)

	items, summary := runBatch(instances, continueOnError, func(target string) (map[string]any, output.ErrorCode, bool, error) {
		out, errMsg, code := runQueryOnInstance(client, target, db, sql, limit, table, schema)
		if errMsg != "" {
			return nil, code, output.RetryableErrorCode(code), errBatch(errMsg)
		}
		m := queryRunOutputToMap(out)
		if len(fields) > 0 {
			m = output.FilterMap(m, fields)
		}
		return m, "", false, nil
	})

	printBatchResult(items, summary)
	return nil
}

func printTextQueryResult(out QueryRunOutput) {
	if len(out.Columns) == 0 && len(out.Rows) == 0 {
		output.Info("Query returned no results.")
		return
	}

	headers := out.Columns
	rows := make([][]string, len(out.Rows))
	for i, row := range out.Rows {
		cells := make([]string, len(row))
		for j, val := range row {
			cells[j] = fmt.Sprintf("%v", val)
		}
		rows[i] = cells
	}
	output.Table(headers, rows)

	fmt.Printf("\n%d rows returned", out.RowCount)
	if out.QueryTimeMS > 0 {
		fmt.Printf(" in %dms", out.QueryTimeMS)
	}
	fmt.Println()
	if out.Masked {
		output.Warn("Results may be masked due to permissions.")
	}
}

func printRawQueryResult(columns []string, rows [][]any) {
	// Print tab-separated output: header row + data rows
	fmt.Println(strings.Join(columns, "\t"))
	for _, row := range rows {
		cells := make([]string, len(row))
		for j, val := range row {
			cells[j] = fmt.Sprintf("%v", val)
		}
		fmt.Println(strings.Join(cells, "\t"))
	}
}

// ─── explain ────────────────────────────────────────────────────────────────

// QueryExplainOutput is the structured output for explain results.
type QueryExplainOutput struct {
	Plan []map[string]any `json:"plan"`
}

// queryExplainResponse is the raw API response from POST /query/explain/. The
// view returns data as a separated dict ({column_list, rows}) via
// ResultSet.to_sep_dict(), not a list of row maps — so the plan is reassembled
// client-side by zipping column_list onto each row.
type queryExplainResponse struct {
	Status int                 `json:"status"`
	Msg    string              `json:"msg"`
	Data   queryExplainDataSet `json:"data"`
}

type queryExplainDataSet struct {
	ColumnList []string `json:"column_list"`
	Rows       [][]any  `json:"rows"`
}

var queryExplainCmd = &cobra.Command{
	Use:   "explain",
	Short: "Get the EXPLAIN plan for a SQL query",
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

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// The explain view reads sql_content (not sql); a mismatched key lands the
		// value in None and the view rejects it with "页面提交参数可能为空".
		form := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
			"sql_content":   {sql},
		}

		data, err := client.InternalPost(apiCtx(), "/query/explain/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp queryExplainResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse explain response: " + err.Error())
		}

		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		// Reassemble the column-separated dict into one map per plan row, keeping
		// the {plan:[{col:val}]} output schema stable across Archery versions.
		out := QueryExplainOutput{Plan: zipExplainRows(resp.Data.ColumnList, resp.Data.Rows)}

		if jsonMode {
			m := map[string]any{"plan": out.Plan}
			api.TagUntrusted(m, "plan")
			if fields := getFieldsFlag(cmd); len(fields) > 0 {
				output.PrintJSON(output.FilterMap(m, fields))
			} else {
				output.PrintJSON(m)
			}
			return nil
		}

		if len(out.Plan) == 0 {
			output.Info("No explain plan returned.")
			return nil
		}

		// Text format: print as table, columns in the engine's declared order.
		headers := resp.Data.ColumnList
		rows := make([][]string, len(out.Plan))
		for i, row := range out.Plan {
			cells := make([]string, len(headers))
			for j, h := range headers {
				cells[j] = fmt.Sprintf("%v", row[h])
			}
			rows[i] = cells
		}
		output.Table(headers, rows)
		return nil
	},
}

// zipExplainRows turns the engine's column-separated result ({column_list, rows})
// into one map per row keyed by column name. Cells beyond the declared columns
// are dropped; missing cells are left unset.
func zipExplainRows(columns []string, rows [][]any) []map[string]any {
	plan := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m := make(map[string]any, len(columns))
		for i, col := range columns {
			if i < len(row) {
				m[col] = row[i]
			}
		}
		plan = append(plan, m)
	}
	return plan
}

// ─── log ────────────────────────────────────────────────────────────────────

// QueryLogEntry represents a single query log entry for output. Fields map to
// the v1.8.5 _querylog projection: username←user_display, db←db_name,
// exec_time←create_time. favorite/alias surface the star state set by
// `query favorite`.
type QueryLogEntry struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	DB        string `json:"db"`
	SQL       string `json:"sql"`
	EffectRow int    `json:"effect_row"`
	CostTime  string `json:"cost_time"`
	Instance  string `json:"instance_name"`
	ExecTime  string `json:"exec_time"`
	Favorite  bool   `json:"favorite"`
	Alias     string `json:"alias"`
}

// queryLogResponse is the raw API response from GET /query/querylog/. The
// _querylog view emits the page payload at the top level ({total, rows}), not
// wrapped in the usual {status, msg, data} envelope — so total/rows are decoded
// directly here, not under a data object.
type queryLogResponse struct {
	Total int                `json:"total"`
	Rows  []queryLogEntryRaw `json:"rows"`
}

// queryLogEntryRaw mirrors the .values() projection in sql/query.py _querylog:
// the row exposes user_display (the engineer's display name) and db_name; there
// is no separate db_user or exec_time column in v1.8.5. create_time carries the
// execution timestamp.
type queryLogEntryRaw struct {
	ID         int    `json:"id"`
	UserDisp   string `json:"user_display"`
	DbName     string `json:"db_name"`
	SQLText    string `json:"sqllog"`
	EffectRow  int    `json:"effect_row"`
	CostTime   string `json:"cost_time"`
	Instance   string `json:"instance_name"`
	CreateTime string `json:"create_time"`
	Favorite   bool   `json:"favorite"`
	Alias      string `json:"alias"`
}

var queryLogCmd = &cobra.Command{
	Use:   "log",
	Short: "View query execution history",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		search, _ := cmd.Flags().GetString("search")
		star, _ := cmd.Flags().GetBool("star")
		start, _ := cmd.Flags().GetString("start")
		end, _ := cmd.Flags().GetString("end")

		if limit < 1 || limit > 100 {
			return failArg("--limit must be between 1 and 100")
		}
		if offset < 0 {
			return failArg("--offset must be >= 0")
		}

		params := url.Values{}
		if limit > 0 {
			params.Set("limit", strconv.Itoa(limit))
		}
		if offset > 0 {
			params.Set("offset", strconv.Itoa(offset))
		}
		if search != "" {
			params.Set("search", search)
		}
		if star {
			params.Set("star", "true")
		}
		if start != "" {
			params.Set("start_date", start)
		}
		if end != "" {
			params.Set("end_date", end)
		}

		path := "/query/querylog/"
		if len(params) > 0 {
			path += "?" + params.Encode()
		}

		data, err := client.InternalGet(apiCtx(), path)
		if err != nil {
			return handleAPIError(err)
		}

		var resp queryLogResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse query log response: " + err.Error())
		}

		entries := make([]QueryLogEntry, len(resp.Rows))
		for i, r := range resp.Rows {
			entries[i] = QueryLogEntry{
				ID:        r.ID,
				Username:  r.UserDisp,
				DB:        r.DbName,
				SQL:       r.SQLText,
				EffectRow: r.EffectRow,
				CostTime:  r.CostTime,
				Instance:  r.Instance,
				ExecTime:  r.CreateTime,
				Favorite:  r.Favorite,
				Alias:     r.Alias,
			}
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			out := make([]map[string]any, len(entries))
			for i, e := range entries {
				m := queryLogEntryToMap(e)
				api.TagUntrusted(m, "sql", "alias")
				out[i] = output.FilterMap(m, fields)
			}
			output.PrintJSON(offsetListEnvelope(out, offset, len(out), resp.Total))
			return nil
		}

		if len(entries) == 0 {
			output.Info("No query log entries found.")
			return nil
		}

		headers := []string{"ID", "USER", "INSTANCE", "SQL", "ROWS", "TIME", "EXEC_TIME"}
		rows := make([][]string, len(entries))
		for i, e := range entries {
			sqlPreview := e.SQL
			if len(sqlPreview) > 60 {
				sqlPreview = sqlPreview[:57] + "..."
			}
			rows[i] = []string{
				strconv.Itoa(e.ID),
				e.Username,
				e.Instance,
				sqlPreview,
				strconv.Itoa(e.EffectRow),
				e.CostTime,
				e.ExecTime,
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

func queryLogEntryToMap(e QueryLogEntry) map[string]any {
	return normalizeAgentMap(map[string]any{
		"id":         strconv.Itoa(e.ID),
		"username":   e.Username,
		"db":         e.DB,
		"sql":        e.SQL,
		"effect_row": e.EffectRow,
		"cost_time":  e.CostTime,
		"instance":   e.Instance,
		"exec_time":  e.ExecTime,
		"favorite":   e.Favorite,
		"alias":      e.Alias,
	})
}

// ─── favorite ───────────────────────────────────────────────────────────────

var queryFavoriteCmd = &cobra.Command{
	Use:   "favorite <log_id>",
	Short: "Star or unstar a query log entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logID, err := strconv.Atoi(args[0])
		if err != nil {
			return failArg("log_id must be a number")
		}

		star, _ := cmd.Flags().GetBool("star")
		alias, _ := cmd.Flags().GetString("alias")

		detail := map[string]any{"logId": strconv.Itoa(logID), "star": star}
		if alias != "" {
			detail["alias"] = alias
		}
		if markDryRunOrConfirm("query favorite", detail) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		// The favorite view reads query_log_id (not id) from POST and treats
		// star as the literal string "true"/"false"; FormatBool matches that.
		// alias must always be sent: QueryLog.alias is NOT NULL, and the view
		// writes request.POST.get('alias') verbatim — omitting it sends None and
		// the save fails with an IntegrityError (500). An empty string clears it.
		form := url.Values{
			"query_log_id": {strconv.Itoa(logID)},
			"star":         {strconv.FormatBool(star)},
			"alias":        {alias},
		}

		_, err = client.InternalPost(apiCtx(), "/query/favorite/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"id":   strconv.Itoa(logID),
				"star": star,
			})
			return nil
		}

		if star {
			output.Success(fmt.Sprintf("Starred query log #%d", logID))
		} else {
			output.Success(fmt.Sprintf("Unstarred query log #%d", logID))
		}
		return nil
	},
}

// ─── generate ───────────────────────────────────────────────────────────────

var queryGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate SQL using AI based on a description (requires a newer Archery)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate the same arg contract first so the gate is the only reason
		// this fails once the server-side route exists, not missing flags.
		if instance, _ := cmd.Flags().GetString("instance"); instance == "" {
			return failArg("--instance is required")
		}
		if db, _ := cmd.Flags().GetString("db"); db == "" {
			return failArg("--db is required")
		}
		if table, _ := cmd.Flags().GetString("table"); table == "" {
			return failArg("--table is required")
		}
		if desc, _ := cmd.Flags().GetString("desc"); desc == "" {
			return failArg("--desc is required")
		}

		// NL→SQL generation has no route in Archery v1.8.5 (the production
		// version): /query/generate_sql/ is absent from sql/urls.py and was added
		// in a later release. Fail fast with the server's missing-route semantics
		// instead of issuing a request that would 404 and read as a transport bug.
		return failWithCode(
			"query generate is not available on this Archery server (no /query/generate_sql/ route in v1.8.5); upgrade Archery to a version that ships NL→SQL generation",
			ExitNotFound, output.E_NOT_FOUND)
	},
}
