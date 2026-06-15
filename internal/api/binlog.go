package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
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
type BinlogParseResult struct {
	Status int              `json:"status"`
	Msg    string           `json:"msg"`
	Data   []BinlogParseRow `json:"data"`
}

// BinlogParseParams collects the my2sql request fields. Empty/zero values are
// omitted from the form so the view applies its own defaults (num=30,
// threads=4, sql_type=all DML).
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
	form := url.Values{"instance_name": {p.InstanceName}}
	if p.StartFile != "" {
		form.Set("start_file", p.StartFile)
	}
	if p.EndFile != "" {
		form.Set("end_file", p.EndFile)
	}
	if p.StartPos > 0 {
		form.Set("start_pos", strconv.Itoa(p.StartPos))
	}
	if p.EndPos > 0 {
		form.Set("end_pos", strconv.Itoa(p.EndPos))
	}
	if p.StartTime != "" {
		form.Set("start_time", p.StartTime)
	}
	if p.StopTime != "" {
		form.Set("stop_time", p.StopTime)
	}
	for _, s := range p.Schemas {
		form.Add("only_schemas", s)
	}
	for _, t := range p.Tables {
		form.Add("only_tables[]", t)
	}
	for _, t := range p.SQLTypes {
		form.Add("sql_type[]", t)
	}
	if p.Num > 0 {
		form.Set("num", strconv.Itoa(p.Num))
	}
	if p.Threads > 0 {
		form.Set("threads", strconv.Itoa(p.Threads))
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
