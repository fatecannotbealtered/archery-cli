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
	queryRunCmd.Flags().String("instance", "", "Instance name (required)")
	queryRunCmd.Flags().String("db", "", "Database name (required)")
	queryRunCmd.Flags().String("sql", "", "SQL to execute (required)")
	queryRunCmd.Flags().Int("limit", 0, "Row limit (0 = server default)")
	queryRunCmd.Flags().String("table", "", "Table name (for context)")
	queryRunCmd.Flags().String("schema", "", "Schema name")
	queryRunCmd.Flags().String("fields", "", "Comma-separated fields for JSON output")
	queryCmd.AddCommand(queryRunCmd)
	markWrite(queryRunCmd)
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

// queryRunResponse is the raw API response from POST /query/.
type queryRunResponse struct {
	Status      int            `json:"status"`
	Msg         string         `json:"msg"`
	ColumnList  []string       `json:"column_list"`
	Rows        [][]any        `json:"rows"`
	RowCount    int            `json:"row_count"`
	AffectedRows int           `json:"affected_rows"`
	QueryTime   float64        `json:"query_time"`
	IsMasked    bool           `json:"is_masked"`
	IsExecute   bool           `json:"is_execute"`
	ErrorMessage string        `json:"error_message"`
}

var queryRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute a SQL query",
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
		limit, _ := cmd.Flags().GetInt("limit")
		table, _ := cmd.Flags().GetString("table")
		schema, _ := cmd.Flags().GetString("schema")

		if dryRunOutput("query run", map[string]any{"instance": instance, "db": db, "sql": sql}) {
			return nil
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
		if limit > 0 {
			form.Set("limit", strconv.Itoa(limit))
		}
		if table != "" {
			form.Set("table_name", table)
		}
		if schema != "" {
			form.Set("schema_name", schema)
		}

		data, err := client.InternalPost(apiCtx(), "/query/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp queryRunResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse query response: " + err.Error())
		}

		if resp.Status != 0 && resp.ErrorMessage != "" {
			return failArg(resp.ErrorMessage)
		}

		out := QueryRunOutput{
			Columns:     resp.ColumnList,
			Rows:        resp.Rows,
			RowCount:    resp.RowCount,
			QueryTimeMS: int(resp.QueryTime * 1000),
			Masked:      resp.IsMasked,
		}

		if jsonMode {
			// Tag rows as untrusted: raw database data may contain injection payloads (SEC-SPEC §2).
			outMap := map[string]any{
				"columns":       out.Columns,
				"rows":          out.Rows,
				"row_count":     out.RowCount,
				"query_time_ms": out.QueryTimeMS,
				"masked":        out.Masked,
			}
			api.TagUntrusted(outMap, "rows")
			if fields := getFieldsFlag(cmd); len(fields) > 0 {
				outMap = output.FilterMap(outMap, fields)
			}
			output.PrintJSON(outMap)
			return nil
		}

		// text/raw format
		if formatMode == formatRaw {
			printRawQueryResult(resp.ColumnList, resp.Rows)
			return nil
		}

		printTextQueryResult(out)
		return nil
	},
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

// queryExplainResponse is the raw API response from POST /query/explain/.
type queryExplainResponse struct {
	Status int              `json:"status"`
	Msg    string           `json:"msg"`
	Data   []map[string]any `json:"data"`
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

		form := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
			"sql":           {sql},
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

		out := QueryExplainOutput{Plan: resp.Data}

		if jsonMode {
			if fields := getFieldsFlag(cmd); len(fields) > 0 {
				m := map[string]any{"plan": out.Plan}
				output.PrintJSON(output.FilterMap(m, fields))
			} else {
				output.PrintJSON(out)
			}
			return nil
		}

		if len(resp.Data) == 0 {
			output.Info("No explain plan returned.")
			return nil
		}

		// Text format: print as table
		var headers []string
		for k := range resp.Data[0] {
			headers = append(headers, k)
		}
		rows := make([][]string, len(resp.Data))
		for i, row := range resp.Data {
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

// ─── log ────────────────────────────────────────────────────────────────────

// QueryLogEntry represents a single query log entry for output.
type QueryLogEntry struct {
	ID         int    `json:"id"`
	Username   string `json:"username"`
	DbUser     string `json:"db_user"`
	SQL        string `json:"sql"`
	EffectRow  int    `json:"effect_row"`
	CostTime   string `json:"cost_time"`
	Instance   string `json:"instance_name"`
	ExecTime   string `json:"exec_time"`
}

// queryLogResponse is the raw API response from GET /query/querylog/.
type queryLogResponse struct {
	Status int               `json:"status"`
	Msg    string            `json:"msg"`
	Data   queryLogPageData  `json:"data"`
}

type queryLogPageData struct {
	Total int                `json:"total"`
	Rows  []queryLogEntryRaw `json:"rows"`
}

type queryLogEntryRaw struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	DbUser    string `json:"db_user"`
	SQLText   string `json:"sqllog"`
	EffectRow int    `json:"effect_row"`
	CostTime  string `json:"cost_time"`
	Instance  string `json:"instance_name"`
	ExecTime  string `json:"exec_time"`
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

		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		entries := make([]QueryLogEntry, len(resp.Data.Rows))
		for i, r := range resp.Data.Rows {
			entries[i] = QueryLogEntry{
				ID:        r.ID,
				Username:  r.Username,
				DbUser:    r.DbUser,
				SQL:       r.SQLText,
				EffectRow: r.EffectRow,
				CostTime:  r.CostTime,
				Instance:  r.Instance,
				ExecTime:  r.ExecTime,
			}
		}

		if jsonMode {
			fields := getFieldsFlag(cmd)
			out := make([]map[string]any, len(entries))
			for i, e := range entries {
				m := queryLogEntryToMap(e)
				api.TagUntrusted(m, "sql")
				out[i] = output.FilterMap(m, fields)
			}
			output.PrintJSON(map[string]any{
				"items":    out,
				"total":    resp.Data.Total,
				"count":    len(out),
				"has_more": offset+len(out) < resp.Data.Total,
			})
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
	return map[string]any{
		"id":          e.ID,
		"username":    e.Username,
		"db_user":     e.DbUser,
		"sql":         e.SQL,
		"effect_row":  e.EffectRow,
		"cost_time":   e.CostTime,
		"instance":    e.Instance,
		"exec_time":   e.ExecTime,
	}
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

		if dryRunOutput("query favorite", map[string]any{"log_id": logID, "star": star}) {
			return nil
		}

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"id":    {strconv.Itoa(logID)},
			"star":  {strconv.FormatBool(star)},
		}
		if alias != "" {
			form.Set("alias", alias)
		}

		_, err = client.InternalPost(apiCtx(), "/query/favorite/", form)
		if err != nil {
			return handleAPIError(err)
		}

		if jsonMode {
			output.PrintJSON(map[string]any{
				"id":   logID,
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

// QueryGenerateOutput is the structured output for AI SQL generation.
type QueryGenerateOutput struct {
	SQL string `json:"sql"`
}

// queryGenerateResponse is the raw API response from POST /query/generate_sql/.
type queryGenerateResponse struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
	Data   string `json:"data"`
}

var queryGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate SQL using AI based on a description",
	RunE: func(cmd *cobra.Command, args []string) error {
		instance, _ := cmd.Flags().GetString("instance")
		if instance == "" {
			return failArg("--instance is required")
		}
		db, _ := cmd.Flags().GetString("db")
		if db == "" {
			return failArg("--db is required")
		}
		table, _ := cmd.Flags().GetString("table")
		if table == "" {
			return failArg("--table is required")
		}
		desc, _ := cmd.Flags().GetString("desc")
		if desc == "" {
			return failArg("--desc is required")
		}
		dbType, _ := cmd.Flags().GetString("db-type")
		schema, _ := cmd.Flags().GetString("schema")

		client, _, _, err := newClient()
		if err != nil {
			return err
		}

		form := url.Values{
			"instance_name": {instance},
			"db_name":       {db},
			"table_name":    {table},
			"desc":          {desc},
		}
		if dbType != "" {
			form.Set("db_type", dbType)
		}
		if schema != "" {
			form.Set("schema_name", schema)
		}

		data, err := client.InternalPost(apiCtx(), "/query/generate_sql/", form)
		if err != nil {
			return handleAPIError(err)
		}

		var resp queryGenerateResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return failArg("failed to parse generate response: " + err.Error())
		}

		if resp.Status != 0 && resp.Msg != "" {
			return failArg(resp.Msg)
		}

		out := QueryGenerateOutput{SQL: resp.Data}

		if jsonMode {
			if fields := getFieldsFlag(cmd); len(fields) > 0 {
				m := map[string]any{"sql": out.SQL}
				output.PrintJSON(output.FilterMap(m, fields))
			} else {
				output.PrintJSON(out)
			}
			return nil
		}

		fmt.Println(resp.Data)
		return nil
	},
}
