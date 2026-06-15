package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// BinlogAPI provides methods for the MySQL binlog endpoints.
//
// These views are session-only: Archery exposes no REST/JWT equivalent under
// /api/v1/, so every method uses the session (AJAX/Django) transport regardless
// of the client's mode. Form fields and response shapes are taken from
// sql/binlog.py in the v1.8.5 source.
type BinlogAPI struct {
	client *Client
}

// BinlogListResult is the decoded response of POST /binlog/list/.
//
// The view runs `show binary logs;` and returns each row as a map keyed by the
// result columns (Log_name, File_size, and Encrypted on 8.0). Rows are kept as
// generic maps because the column set varies across MySQL versions.
type BinlogListResult struct {
	Status int              `json:"status"`
	Msg    string           `json:"msg"`
	Data   []map[string]any `json:"data"`
}

// BinlogParseRow is a single parsed statement. The my2sql engine returns only
// the SQL text; the binlog2sql engine also fills BinlogInfo (position metadata).
type BinlogParseRow struct {
	SQL        string `json:"sql"`
	BinlogInfo string `json:"binlog_info,omitempty"`
}

// BinlogParseResult is the decoded response of POST /binlog/my2sql/.
//
// data is kept raw because its shape depends on status: the success path sets
// it to a list of parsed rows, while the arg-check failure path returns
// {"status":1,"data":{}} (an object). Rows() decodes the list only when the
// call succeeded, so an error envelope never causes an unmarshal failure.
type BinlogParseResult struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

// Rows decodes the parsed statements from a successful response. It returns an
// empty slice for any non-list data payload (e.g. the {} returned on error).
func (r *BinlogParseResult) Rows() ([]BinlogParseRow, error) {
	if len(r.Data) == 0 {
		return nil, nil
	}
	var rows []BinlogParseRow
	if err := json.Unmarshal(r.Data, &rows); err != nil {
		// Error envelopes carry data:{}; treat as no rows rather than failing.
		return nil, nil
	}
	return rows, nil
}

// BinlogParseParams collects the my2sql request fields. Zero-valued numeric
// fields are sent as empty strings (see emptyIfZero) so the view applies its
// own defaults (num=30, threads=4) without 500ing on int(None); an empty
// sql_type list lets the view default to all DML.
type BinlogParseParams struct {
	InstanceName string
	StartFile    string
	EndFile      string
	StartPos     int
	EndPos       int
	StartTime    string
	StopTime     string
	Schemas      []string // -> only_schemas (repeated)
	Tables       []string // -> only_tables[] (repeated)
	SQLTypes     []string // -> sql_type[] (repeated)
	Num          int
	Threads      int
	Rollback     bool
	SaveSQL      bool
}

// BinlogPurgeResult is the decoded response of POST /binlog/del_log/.
//
// The view returns status 0 on success, 1 when no binlog was given, and 2 when
// the `purge master logs` statement failed.
type BinlogPurgeResult struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

// emptyIfZero renders an int as a decimal string, or "" when zero. The my2sql
// view reads num/threads/start_pos/end_pos as int(value) unless value is the
// empty string, so "" signals "unset" and lets the view apply its own default.
func emptyIfZero(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// List returns the binary logs of an instance.
//
//	session: POST /binlog/list/ (instance_name)
func (b *BinlogAPI) List(ctx context.Context, instanceName string) (*BinlogListResult, error) {
	form := url.Values{"instance_name": {instanceName}}
	data, err := b.client.SessionPost(ctx, "/binlog/list/", form)
	if err != nil {
		return nil, err
	}
	var res BinlogListResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Parse runs the my2sql engine over a binlog range and returns the first `num`
// statements (rollback statements when Rollback is set).
//
//	session: POST /binlog/my2sql/
//
// List fields use the suffixed keys the Django view reads with getlist:
// only_schemas, only_tables[], sql_type[]. Time fields are start_time and
// stop_time (the view has no end_time). Booleans are the literal string "true".
func (b *BinlogAPI) Parse(ctx context.Context, p BinlogParseParams) (*BinlogParseResult, error) {
	// The v1.8.5 my2sql view parses num/threads/start_pos/end_pos with the
	// pattern `30 if get(...) == '' else int(get(...))`. An absent field yields
	// None, so `int(None)` raises and the view 500s. The web UI always submits
	// these fields (empty string when blank); mirror that by sending every
	// optional field, using "" for unset values so the view applies its own
	// defaults (num=30, threads=4) and skips the int() conversion.
	form := url.Values{
		"instance_name": {p.InstanceName},
		"start_file":    {p.StartFile},
		"end_file":      {p.EndFile},
		"start_time":    {p.StartTime},
		"stop_time":     {p.StopTime},
		"start_pos":     {emptyIfZero(p.StartPos)},
		"end_pos":       {emptyIfZero(p.EndPos)},
		"num":           {emptyIfZero(p.Num)},
		"threads":       {emptyIfZero(p.Threads)},
	}
	for _, s := range p.Schemas {
		form.Add("only_schemas", s)
	}
	for _, t := range p.Tables {
		form.Add("only_tables[]", t)
	}
	// my2sql's -sql flag only accepts lowercase insert/update/delete and rejects
	// any other casing with "invalid sqltypes"; the view forwards these values
	// verbatim, so normalize here regardless of how the user typed them.
	for _, t := range p.SQLTypes {
		form.Add("sql_type[]", strings.ToLower(t))
	}
	if p.Rollback {
		form.Set("rollback", "true")
	}
	if p.SaveSQL {
		form.Set("save_sql", "true")
	}

	data, err := b.client.SessionPost(ctx, "/binlog/my2sql/", form)
	if err != nil {
		return nil, err
	}
	var res BinlogParseResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Purge runs `purge master logs to '<binlog>'` on an instance, deleting that
// file and all binlogs before it.
//
//	session: POST /binlog/del_log/ (instance_id, binlog)
//
// Note the view keys the instance by numeric id, not name.
func (b *BinlogAPI) Purge(ctx context.Context, instanceID, binlogFile string) (*BinlogPurgeResult, error) {
	form := url.Values{
		"instance_id": {instanceID},
		"binlog":      {binlogFile},
	}
	data, err := b.client.SessionPost(ctx, "/binlog/del_log/", form)
	if err != nil {
		return nil, err
	}
	var res BinlogPurgeResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
